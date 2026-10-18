package ajean

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Faux moteur d'embeddings : un axe par thème, assez pour vérifier que la
// recherche suit le SENS (des mots différents, le même thème).
func fakeEmbedServer(t *testing.T) int {
	t.Helper()
	themes := [][]string{
		{"voiture", "garage", "clio", "mécanicien", "vidange"},
		{"recette", "gâteau", "tarte", "cuisine"},
		{"facture", "électricité", "payer"},
	}
	vec := func(s string) []float64 {
		s = strings.ToLower(s)
		v := make([]float64, len(themes)+1)
		v[len(themes)] = 0.1
		for i, ws := range themes {
			for _, w := range ws {
				if strings.Contains(s, w) {
					v[i]++
				}
			}
		}
		return v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var data []map[string]any
		for _, in := range req.Input {
			data = append(data, map[string]any{"embedding": vec(in)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestJeanEmbedQuantRoundTrip(t *testing.T) {
	v := normVec([]float64{0.3, -0.7, 0.1, 0.05, -0.2})
	got := dequantVec(quantVec(v))
	if len(got) != len(v) || dotVec(v, got) < 0.999 {
		t.Fatalf("aller-retour int8 trop imprécis : %v / %v", v, got)
	}
	idx := map[string][]float32{"a1": v, "b2": normVec([]float64{1, 0, 0, 0, 0})}
	back := parseEmbedIndex(formatEmbedIndex(idx))
	if len(back) != 2 || dotVec(back["a1"], v) < 0.999 {
		t.Fatalf("index relu : %v", back)
	}
	// autre modèle : l'index est ignoré (sera reconstruit)
	if len(parseEmbedIndex("jean-embed autre-modele\na1 AAAA\n")) != 0 {
		t.Fatal("index d'un autre modèle relu")
	}
	if math.IsNaN(float64(dotVec(normVec([]float64{0, 0}), v))) {
		t.Fatal("vecteur nul : NaN")
	}
}

// Bout en bout : le journal est indexé, puis une question formulée avec d'autres
// mots retrouve le bon souvenir, que la recherche par mots ratait.
func TestJeanSearchByMeaning(t *testing.T) {
	testHome(t)
	jeanEmbedTestPort = fakeEmbedServer(t)
	t.Cleanup(func() { jeanEmbedTestPort = 0 })
	jeanEmbedCache.Lock()
	jeanEmbedCache.files = nil
	jeanEmbedCache.Unlock()
	for _, n := range []string{"Il faut emmener la Clio au garage jeudi pour la vidange", "La recette de la tarte aux pommes de grand-mère", "Penser à payer la facture d'électricité"} {
		if _, err := JeanNote(n); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // identifiants à la milliseconde
	}
	q := "le rendez-vous chez le mécanicien pour la voiture"
	// Recherche par le sens coupée : les mots ne trouvent rien (et les petits mots
	// « le », « la », « pour » ne ramènent plus tout le journal).
	if r := JeanSearch(q, 3); !strings.HasPrefix(r, "[aucun résultat]") {
		t.Fatalf("la recherche par mots aurait dû échouer : %s", r)
	}
	if err := putBool(bkState, "jean_embed", true); err != nil {
		t.Fatal(err)
	}
	n, err := jeanEmbedIndexOnce(t.Context(), 100)
	if err != nil || n != 3 {
		t.Fatalf("indexation : %d entrées, %v", n, err)
	}
	r := JeanSearch(q, 3)
	if !strings.Contains(r, "Clio") {
		t.Fatalf("la recherche par le sens n'a pas retrouvé le souvenir : %s", r)
	}
	if first := strings.SplitN(r, "\n", 2)[0]; !strings.Contains(first, "Clio") {
		t.Fatalf("le bon souvenir doit venir en premier : %s", r)
	}
	st := jeanEmbedState()
	if !st.Enabled || st.Indexed != 3 || st.Total != 3 {
		t.Fatalf("état : %+v", st)
	}
	// Moteur coupé : retour à la recherche par mots, sans erreur.
	jeanEmbedTestPort = 0
	_ = putBool(bkState, "jean_embed", false)
	if r := JeanSearch("vidange Clio", 3); !strings.Contains(r, "Clio") {
		t.Fatalf("repli par mots : %s", r)
	}
}
