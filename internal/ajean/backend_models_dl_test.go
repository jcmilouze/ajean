package ajean

import (
	"bytes"
	"context"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// serveBlob serves data with (or without) byte-range support.
func serveBlob(data []byte, ranges bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ranges {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			_, _ = w.Write(data)
			return
		}
		http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
	}))
}

// Un téléchargement annulé ne doit laisser NI le .gguf final NI le .part.
func TestRunDownloadCancelLeavesNothing(t *testing.T) {
	t.Setenv("AJEAN_DL_CONNS", "4")
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" { // sonde : réponse immédiate
			w.Header().Set("Content-Range", "bytes 0-0/"+strconv.Itoa(64<<20))
			w.WriteHeader(206)
			_, _ = w.Write([]byte{0})
			return
		}
		// Corps qui traîne : l'annulation doit l'interrompre.
		w.WriteHeader(206)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	dir := t.TempDir()
	dest := filepath.Join(dir, "m.gguf")
	ctx, cancel := context.WithCancel(context.Background())
	st := &dlState{Filename: "m.gguf", cancel: cancel}
	done := make(chan struct{})
	go func() { runDownloadSet(ctx, st, []string{srv.URL + "/m.gguf"}, []string{dest}); close(done) }()

	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runDownloadSet n a pas rendu la main apres annulation")
	}

	if !st.Canceled || !st.Finished {
		t.Fatalf("état attendu annulé+terminé, got canceled=%v finished=%v err=%q", st.Canceled, st.Finished, st.Err)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part laissé sur le disque après annulation")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("fichier final créé alors que le téléchargement a été annulé")
	}
}

// cleanStalePartFiles doit balayer les .part orphelins sans toucher aux .gguf.
func TestCleanStalePartFiles(t *testing.T) {
	testHome(t)
	dir := modelsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "bon.gguf")
	stale := filepath.Join(dir, "coupe.gguf.part")
	for _, p := range []string{keep, stale} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cleanStalePartFiles()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal(".part orphelin non supprimé")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(".gguf valide supprimé par erreur")
	}
}

func TestRunDownloadParallelAndFallback(t *testing.T) {
	data := make([]byte, 48<<20) // > 3×dlMinChunk so the split actually kicks in
	rand.New(rand.NewSource(1)).Read(data)
	t.Setenv("AJEAN_DL_CONNS", "4")

	for _, ranges := range []bool{true, false} {
		srv := serveBlob(data, ranges)
		dir := t.TempDir()
		dest := filepath.Join(dir, "m.gguf")
		st := &dlState{Filename: "m.gguf"}
		runDownloadSet(context.Background(), st, []string{srv.URL + "/m.gguf"}, []string{dest})
		srv.Close()

		if st.Err != "" {
			t.Fatalf("ranges=%v: erreur %s", ranges, st.Err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("ranges=%v: %v", ranges, err)
		}
		if len(got) != len(data) {
			t.Fatalf("ranges=%v: taille %d != %d", ranges, len(got), len(data))
		}
		for i := range got {
			if got[i] != data[i] {
				t.Fatalf("ranges=%v: octet %d différent", ranges, i)
			}
		}
		if st.Done != int64(len(data)) {
			t.Fatalf("ranges=%v: done=%d", ranges, st.Done)
		}
		if ranges && st.Conns < 2 {
			t.Fatalf("attendu du parallélisme, conns=%d", st.Conns)
		}
		if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
			t.Fatalf("ranges=%v: .part laissé derrière", ranges)
		}
	}
}

type slowWriter struct{ http.ResponseWriter }

func (s slowWriter) Write(b []byte) (int, error) {
	time.Sleep(2 * time.Millisecond)
	return s.ResponseWriter.Write(b)
}

// Une pause coupe le flux, garde ce qui est écrit, et la reprise termine un
// fichier identique à la source.
func TestRunDownloadPauseResume(t *testing.T) {
	data := make([]byte, 48<<20)
	rand.New(rand.NewSource(2)).Read(data)
	t.Setenv("AJEAN_DL_CONNS", "4")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(slowWriter{w}, r, "m.gguf", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "m.gguf")
	st := &dlState{Filename: "m.gguf", gate: newDlGate()}
	done := make(chan struct{})
	go func() {
		runDownloadSet(context.Background(), st, []string{srv.URL + "/m.gguf"}, []string{dest})
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)
	st.gate.setPaused(true)
	time.Sleep(1500 * time.Millisecond) // le compteur publié se fige
	dlMu.Lock()
	a := st.Done
	dlMu.Unlock()
	time.Sleep(1500 * time.Millisecond)
	dlMu.Lock()
	b, fin := st.Done, st.Finished
	dlMu.Unlock()
	if fin || a != b || a == 0 {
		t.Fatalf("pause inopérante : %d puis %d octets, terminé=%v", a, b, fin)
	}
	st.gate.setPaused(false)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("pas terminé après la reprise")
	}
	if st.Err != "" {
		t.Fatalf("erreur après reprise : %s", st.Err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("fichier différent de la source après reprise (err=%v)", err)
	}
}
