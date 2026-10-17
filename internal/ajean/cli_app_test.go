package ajean

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// La sonde doit distinguer un vrai AJEAN de n'importe quoi d'autre qui occupe le
// port. C'est la non-régression du défaut observé en usage réel le 2026-10-16 :
// AJEAN déduisait « c'est moi » sans vérifier, et ouvrait la fenêtre sur la page
// d'un autre logiciel — un listing de dossier laissé par l'aperçu d'un IDE.
func TestAjeanAnswersOn(t *testing.T) {
	testHome(t)

	portDe := func(t *testing.T, h http.HandlerFunc) int {
		t.Helper()
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		p, err := strconv.Atoi(u.Port())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name string
		h    http.HandlerFunc
		want bool
	}{
		{
			name: "le ping d'AJEAN",
			h: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/ping" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"service":"ajean","version":"0.18.1"}`))
			},
			want: true,
		},
		{
			name: "un listing de dossier — exactement ce qui est arrivé",
			h: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<!DOCTYPE HTML><title>Directory listing for /</title>`))
			},
			want: false,
		},
		{
			name: "un serveur qui ne connaît pas /api/ping",
			h:    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
			want: false,
		},
		{
			name: "un JSON qui n'est pas AJEAN",
			h: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"ok":true,"service":"autre"}`))
			},
			want: false,
		},
		{
			name: "un AJEAN protégé par une clé de pilotage",
			h: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="ajean"`)
				w.WriteHeader(http.StatusUnauthorized)
			},
			want: true,
		},
		{
			name: "un autre serveur qui refuse l'accès",
			h:    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ajeanAnswersOn(portDe(t, c.h)); got != c.want {
				t.Fatalf("attendu %v, obtenu %v", c.want, got)
			}
		})
	}

	// Personne n'écoute : faux, et sans paniquer.
	if ajeanAnswersOn(1) {
		t.Error("un port fermé ne doit pas passer pour AJEAN")
	}
}
