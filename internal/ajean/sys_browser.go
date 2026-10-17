package ajean

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Où vivent les navigateurs, et comment en choisir un nommément.
//
// Pourquoi ce fichier existe : l'interface AJEAN tourne dans un navigateur lancé
// en `--app=` par openAppWindow, et c'est findChromium qui le choisit — Edge en
// premier sous Windows. Un lien cliqué héritant du moteur hôte, il s'ouvrait
// donc toujours dans Edge, y compris sur une machine dont le navigateur par
// défaut est Chrome. Le réglage `link_browser` (voir web_open.go) permet de
// choisir ; il ne déplace que les liens, pas la fenêtre.
//
// Une seule table pour tout le dépôt : deux résolveurs portaient la même liste
// dans deux ordres différents (findChromium pour la fenêtre, chromePath pour le
// pilotage), et c'est cette duplication qui a laissé le défaut passer.

// linkBrowserNames : les navigateurs proposés nommément dans le réglage.
// « auto » n'en fait pas partie : il passe par le gestionnaire de l'OS, ce qui
// couvre aussi les navigateurs absents de cette liste (Firefox, etc.).
var linkBrowserNames = []string{"chrome", "edge", "brave", "chromium"}

var (
	// browserWinRel : emplacement relatif à ProgramFiles / LOCALAPPDATA.
	browserWinRel = map[string][]string{
		"chrome":   {`Google\Chrome\Application\chrome.exe`},
		"edge":     {`Microsoft\Edge\Application\msedge.exe`},
		"brave":    {`BraveSoftware\Brave-Browser\Application\brave.exe`},
		"chromium": {`Chromium\Application\chrome.exe`},
	}
	// browserMacApp : relatif à /Applications et ~/Applications.
	browserMacApp = map[string][]string{
		"chrome":   {"Google Chrome.app/Contents/MacOS/Google Chrome"},
		"edge":     {"Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
		"brave":    {"Brave Browser.app/Contents/MacOS/Brave Browser"},
		"chromium": {"Chromium.app/Contents/MacOS/Chromium"},
	}
	// browserUnixNames : noms de commande cherchés dans le PATH.
	browserUnixNames = map[string][]string{
		"chrome":   {"google-chrome", "google-chrome-stable"},
		"edge":     {"microsoft-edge", "microsoft-edge-stable"},
		"brave":    {"brave-browser"},
		"chromium": {"chromium", "chromium-browser"},
	}
)

// browserCandidates renvoie les emplacements à essayer pour un navigateur nommé,
// dans l'ordre. Pure — goos, le foyer et l'environnement sont injectés — pour que
// l'ordre soit vérifiable par un test qui tourne sur n'importe quelle machine :
// la CI exécute les tests sous Linux et macOS, jamais sous Windows, et c'est
// justement l'ordre Windows qui portait le défaut.
func browserCandidates(name, goos, home string, getenv func(string) string) []string {
	switch goos {
	case "windows":
		rels, ok := browserWinRel[name]
		if !ok {
			return nil
		}
		var roots []string
		for _, e := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
			if v := getenv(e); v != "" {
				roots = append(roots, v)
			}
		}
		out := make([]string, 0, len(rels)*len(roots))
		for _, rel := range rels {
			for _, r := range roots {
				out = append(out, filepath.Join(r, rel))
			}
		}
		return out
	case "darwin":
		apps, ok := browserMacApp[name]
		if !ok {
			return nil
		}
		var out []string
		for _, app := range apps {
			out = append(out, "/Applications/"+app)
			if home != "" {
				out = append(out, filepath.Join(home, "Applications", app))
			}
		}
		return out
	default:
		return browserUnixNames[name]
	}
}

// firstExisting renvoie le premier candidat présent sur la machine, "" sinon.
// Un candidat qui contient un séparateur est un chemin (Windows, macOS) : on le
// teste tel quel. Les autres sont des noms de commande (Linux) : on les cherche
// dans le PATH.
func firstExisting(cands []string) string {
	for _, c := range cands {
		if strings.ContainsAny(c, `/\`) {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// resolveNamedBrowser renvoie le chemin du navigateur demandé, "" s'il n'est pas
// installé sur cette machine. Contrairement à findChromium, il n'y a pas d'ordre
// à deviner : l'utilisateur en a nommé un.
func resolveNamedBrowser(name string) string {
	home, _ := os.UserHomeDir()
	return firstExisting(browserCandidates(name, runtime.GOOS, home, os.Getenv))
}
