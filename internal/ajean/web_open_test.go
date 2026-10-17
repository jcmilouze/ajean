package ajean

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Un lien de conversation ne peut être qu'un http(s). Ce contrôle est la seule
// barrière devant exec, et il porte aussi l'anti-injection de drapeau : il doit
// être sans trou.
func TestValidExternalURL(t *testing.T) {
	long := "https://exemple.fr/" + strings.Repeat("a", maxOpenURLLen)
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"http simple", "http://exemple.fr", true},
		{"https avec requête et ancre", "https://exemple.fr/a?b=1&c=2#ancre", true},
		{"schéma en majuscules", "HTTP://EXEMPLE.FR", true},
		{"port explicite", "http://127.0.0.1:8090/x", true},
		{"espaces autour", "  https://exemple.fr  ", true},
		{"vide", "", false},
		{"que des espaces", "   ", false},
		{"sans schéma", "exemple.fr", false},
		{"protocole relatif", "//exemple.fr/x", false},
		{"chemin relatif", "/api/status", false},
		{"hôte vide", "http://", false},
		{"fichier local", "file:///C:/Windows/System32/calc.exe", false},
		{"javascript", "javascript:alert(1)", false},
		{"data", "data:text/html,<script>x</script>", false},
		{"vbscript", "vbscript:msgbox(1)", false},
		{"courriel", "mailto:a@b.fr", false},
		{"injection de drapeau", "--user-data-dir=C:\\x", false},
		{"drapeau dans le chemin (autorisé : l'argument commence par http)", "http://a/--x", true},
		{"retour à la ligne", "http://a/\r\nHost: evil", false},
		{"trop long", long, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := validExternalURL(c.in)
			if !c.ok {
				if err == nil {
					t.Fatalf("accepté à tort : %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("refusé à tort : %v", err)
			}
			if got != strings.TrimSpace(c.in) {
				t.Fatalf("l'URL ouverte doit être celle cliquée, obtenu %q", got)
			}
		})
	}
}

// stubLaunch remplace le lanceur de navigateur pour qu'aucun test n'ouvre de
// vraie fenêtre, et rend la liste des URL lancées.
func stubLaunch(t *testing.T) *[]string {
	t.Helper()
	var launched []string
	openURLExternally = func(u string) (string, error) {
		launched = append(launched, u)
		return "chrome", nil
	}
	t.Cleanup(func() { openURLExternally = launchExternalBrowser })
	return &launched
}

// localReq : une requête telle que la fenêtre AJEAN l'envoie — depuis
// localhost. httptest poserait « example.com », que le contrôle d'accès refuse
// à juste titre quand aucune clé de pilotage n'est configurée.
func localReq(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "localhost:8090"
	return req
}

func postOpen(t *testing.T, h http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": url})
	rr := httptest.NewRecorder()
	req := localReq("POST", "/api/open", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)
	return rr
}

// Un lien valide est ouvert une fois ; tout le reste est refusé en 400 SANS rien
// lancer. C'est l'assertion qui compte : le refus doit précéder l'exec.
func TestHandleOpenURLOuvreLesLiensValidesEtRienDautre(t *testing.T) {
	testHome(t)
	launched := stubLaunch(t)
	h := requireWebAuth(handleOpenURL)

	if rr := postOpen(t, h, "https://exemple.fr/a?b=1"); rr.Code != 200 {
		t.Fatalf("lien valide refusé : %d %s", rr.Code, rr.Body)
	}
	if len(*launched) != 1 || (*launched)[0] != "https://exemple.fr/a?b=1" {
		t.Fatalf("URL lancée inattendue : %q", *launched)
	}

	for _, bad := range []string{
		"", "exemple.fr", "/api/status", "http://",
		"file:///C:/Windows/System32/calc.exe",
		"javascript:alert(1)", "data:text/html,x", "vbscript:msgbox(1)",
		"--user-data-dir=C:\\x",
	} {
		*launched = nil
		rr := postOpen(t, h, bad)
		if rr.Code != 400 {
			t.Errorf("%q devrait être refusé en 400, obtenu %d", bad, rr.Code)
		}
		if len(*launched) != 0 {
			t.Errorf("%q a tout de même lancé %q", bad, *launched)
		}
	}
}

// L'endpoint est bien derrière la clé de pilotage : sans elle, il ne lance rien.
func TestHandleOpenURLDerriereLaCleDePilotage(t *testing.T) {
	testHome(t)
	if err := storeWebKey("ma-cle"); err != nil {
		t.Fatal(err)
	}
	openURLExternally = func(string) (string, error) {
		t.Fatal("le lanceur ne doit jamais être atteint sans clé")
		return "", nil
	}
	t.Cleanup(func() { openURLExternally = launchExternalBrowser })

	if rr := postOpen(t, requireWebAuth(handleOpenURL), "https://exemple.fr"); rr.Code != 401 {
		t.Fatalf("sans clé de pilotage : attendu 401, obtenu %d", rr.Code)
	}
}

func TestHandleOpenURLRefuseLesAutresMethodes(t *testing.T) {
	testHome(t)
	rr := httptest.NewRecorder()
	requireWebAuth(handleOpenURL)(rr, localReq("PUT", "/api/open", nil))
	if rr.Code != 405 {
		t.Fatalf("attendu 405, obtenu %d", rr.Code)
	}
}

// Le GET sert la ligne de réglage : réglage courant + navigateurs réellement
// installés sur la machine.
func TestHandleOpenURLStatut(t *testing.T) {
	testHome(t)
	rr := httptest.NewRecorder()
	requireWebAuth(handleOpenURL)(rr, localReq("GET", "/api/open", nil))
	if rr.Code != 200 {
		t.Fatalf("attendu 200, obtenu %d", rr.Code)
	}
	var got struct {
		OK        bool            `json:"ok"`
		Pref      string          `json:"pref"`
		Available map[string]bool `json:"available"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Pref != "auto" {
		t.Fatalf("réglage par défaut attendu « auto », obtenu %q", got.Pref)
	}
	for _, n := range linkBrowserNames {
		if _, ok := got.Available[n]; !ok {
			t.Errorf("%s manque à la liste des navigateurs", n)
		}
	}
}

// Le réglage : « auto » par défaut, normalisé, et une valeur hors liste ne prend
// jamais la place du choix existant.
func TestLinkBrowserPref(t *testing.T) {
	testHome(t)
	if got := linkBrowserPref(); got != "auto" {
		t.Fatalf("défaut attendu « auto », obtenu %q", got)
	}
	if _, err := saveWebPrefs(map[string]string{"link_browser": "  CHROME  "}); err != nil {
		t.Fatal(err)
	}
	if got := linkBrowserPref(); got != "chrome" {
		t.Fatalf("valeur non normalisée : %q", got)
	}
	if _, err := saveWebPrefs(map[string]string{"link_browser": "firefox"}); err != nil {
		t.Fatal(err)
	}
	if got := linkBrowserPref(); got != "chrome" {
		t.Fatalf("une valeur hors liste a écrasé le choix : %q", got)
	}
	// Une valeur écrite à la main dans le bucket ne doit pas faire échouer
	// l'ouverture d'un lien : on retombe sur le gestionnaire de l'OS.
	if err := putStr(bkPrefs, "link_browser", "safari"); err != nil {
		t.Fatal(err)
	}
	if got := linkBrowserPref(); got != "auto" {
		t.Fatalf("valeur inconnue : attendu « auto », obtenu %q", got)
	}
}
