package ajean

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// streamBodyWatch doit repérer un saut de ligne du corps SANS redécoder tout le
// corps à chaque chunk, et surtout ne JAMAIS en manquer — un saut manqué figerait
// l'aperçu en direct de la bulle d'écriture jusqu'à la ligne suivante.
func TestStreamBodyWatch(t *testing.T) {
	cases := []struct {
		name  string
		parts []string // morceaux successifs des arguments (ils ne font que s'allonger)
		want  []bool   // grew() attendu après chaque morceau
	}{
		{
			name:  "saut échappé arrivé d'un bloc",
			parts: []string{`{"content":"li`, `gne1`, `\n`, `ligne2`, `"}`},
			want:  []bool{false, false, true, false, false},
		},
		{
			name:  "le \\ et le n coupés par la frontière de chunk",
			parts: []string{`{"content":"a`, `\`, `n`, `b`},
			want:  []bool{false, false, true, false},
		},
		{
			name:  "un antislash échappé (double) n'est PAS un saut de ligne",
			parts: []string{`x`, `\`, `\`, `n`},
			want:  []bool{false, false, false, false},
		},
		{
			name:  "vrai octet de saut de ligne (JSON imprimé ou malformé)",
			parts: []string{"{\n", `"content"`, ":\n"},
			want:  []bool{true, false, true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w streamBodyWatch
			args := ""
			for i, part := range tc.parts {
				args += part
				if got := w.grew(args); got != tc.want[i] {
					t.Fatalf("morceau %d (%q) : grew=%v, attendu %v", i, part, got, tc.want[i])
				}
			}
		})
	}
}

// benchStreamArgs découpe des arguments JSON réalistes (une écriture) en petits
// chunks, comme le fait llama.cpp : ~1 token par chunk (≈ 4 octets ici).
func benchStreamArgs(body string, chunk int) []string {
	esc := strings.ReplaceAll(body, "\n", `\n`)
	raw := `{"file":"out.txt","content":"` + esc + `"}`
	var out []string
	for i := 0; i < len(raw); i += chunk {
		e := i + chunk
		if e > len(raw) {
			e = len(raw)
		}
		out = append(out, raw[i:e])
	}
	return out
}

// L'ancien chemin : le corps était redécodé ET recompté à CHAQUE chunk, soit
// O(n²) sur la taille de l'écriture.
func BenchmarkToolBodyPreviewNaive(b *testing.B) {
	chunks := benchStreamArgs(strings.Repeat("une ligne de contenu assez longue pour compter\n", 1250), 4)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		args := ""
		lastBodyLines := -1
		for _, c := range chunks {
			args += c
			body := previewArg(args, "content")
			if body != "" && strings.Count(body, "\n") > lastBodyLines {
				lastBodyLines = strings.Count(body, "\n")
			}
		}
	}
}

// Le nouveau : le corps n'est décodé qu'au premier passage puis quand un saut de
// ligne a réellement été repéré — le coût par chunk se limite au segment nouveau.
func BenchmarkToolBodyPreviewWatched(b *testing.B) {
	chunks := benchStreamArgs(strings.Repeat("une ligne de contenu assez longue pour compter\n", 1250), 4)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		args := ""
		lastBodyLines := -1
		var w streamBodyWatch
		for _, c := range chunks {
			args += c
			body := ""
			if lastBodyLines < 0 || w.grew(args) {
				body = previewArg(args, "content")
			}
			if body != "" && strings.Count(body, "\n") > lastBodyLines {
				lastBodyLines = strings.Count(body, "\n")
			}
		}
	}
}

// sseWriteServer sert une écriture d'outil (write) dont le corps contient
// plusieurs lignes, puis une réponse texte simple : le tour se termine au lieu de
// boucler sur un appel d'outil. Renvoie le port à mettre dans PORT.
func sseWriteServer(t *testing.T, content string) string {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if calls.Add(1) > 1 {
			_, _ = w.Write([]byte(sseChunk("termine") + "data: [DONE]\n\n"))
			w.(http.Flusher).Flush()
			return
		}
		esc := strings.ReplaceAll(content, "\n", `\n`)
		args := `{"file":"note.md","content":"` + esc + `"}`
		// arguments diffusés en petits chunks (≈ 1 token), comme en vrai
		const step = 4
		for i := 0; i < len(args); i += step {
			e := i + step
			if e > len(args) {
				e = len(args)
			}
			piece := args[i:e]
			line := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write","arguments":"` +
				strings.ReplaceAll(strings.ReplaceAll(piece, `\`, `\\`), `"`, `\"`) + `"}}]}}]}` + "\n\n"
			_, _ = w.Write([]byte(line))
		}
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

// Régression : le corps en cours d'écriture doit continuer à s'afficher LIGNE à
// LIGNE dans la bulle (comportement voulu, cf. commentaire du bloc), tout en
// évitant le décodage quadratique. On vérifie donc que plusieurs états
// intermédiaires du corps sont poussés, et que la dernière ligne arrive.
func TestCorpsEcritureDiffuseParLigne(t *testing.T) {
	testHome(t)
	content := "premiere ligne\ndeuxieme ligne\ntroisieme ligne\n"
	port := sseWriteServer(t, content)
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}

	var bodies []string
	_, err := runChat(context.Background(),
		[]Message{{Role: "user", Content: "écris le fichier"}}, 0.7, Caps{Agent: true},
		func(ev StreamEvent) bool {
			if ev.ToolUsed != nil && ev.ToolUsed.Typing && ev.ToolUsed.Body != "" {
				bodies = append(bodies, ev.ToolUsed.Body)
			}
			return true
		}, nil)
	if err != nil {
		t.Fatalf("runChat : %v", err)
	}
	if len(bodies) < 3 {
		t.Fatalf("le corps n'a été diffusé que %d fois : l'aperçu en direct a régressé (%q)", len(bodies), bodies)
	}
	if last := bodies[len(bodies)-1]; !strings.Contains(last, "troisieme ligne") {
		t.Fatalf("le corps diffusé n'atteint pas la dernière ligne : %q", last)
	}
}
