package ajean

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// L'ordre des candidats EST le comportement : c'est lui qui décide quel
// navigateur reçoit la fenêtre AJEAN. Ce test épingle les séquences telles
// qu'elles étaient avant que la table ne soit partagée entre la fenêtre et le
// choix des liens — la liste de la fenêtre doit rester identique au caractère
// près, sinon la fenêtre change de navigateur sans prévenir.
//
// Le test est pur : il passe goos="windows" sur n'importe quelle machine, ce qui
// est indispensable ici — la CI teste sous Linux et macOS, jamais sous Windows,
// et c'est justement l'ordre Windows qui portait le défaut.
func TestAppWindowBrowserOrder(t *testing.T) {
	winEnv := map[string]string{
		"ProgramFiles(x86)": `C:\PF86`,
		"ProgramFiles":      `C:\PF`,
		"LOCALAPPDATA":      `C:\LA`,
	}
	// Sous Windows le séparateur vient de la machine qui exécute le test — les
	// deux côtés passent par filepath.Join, donc le test porte sur l'ordre et sur
	// les chemins relatifs, pas sur le séparateur.
	win := func(rel string) []string {
		return []string{
			filepath.Join(`C:\PF86`, rel),
			filepath.Join(`C:\PF`, rel),
			filepath.Join(`C:\LA`, rel),
		}
	}
	edgeRel := `Microsoft\Edge\Application\msedge.exe`
	chromeRel := `Google\Chrome\Application\chrome.exe`
	braveRel := `BraveSoftware\Brave-Browser\Application\brave.exe`
	chromiumRel := `Chromium\Application\chrome.exe`

	cases := []struct {
		name string
		goos string
		home string
		env  map[string]string
		want []string
	}{
		{
			name: "windows : Edge d'abord (installé d'office), puis Chrome, Brave, Chromium",
			goos: "windows", env: winEnv,
			want: slices.Concat(win(edgeRel), win(chromeRel), win(braveRel), win(chromiumRel)),
		},
		{
			name: "windows : sans ProgramFiles(x86), les racines restantes gardent leur ordre",
			goos: "windows",
			env:  map[string]string{"ProgramFiles": `C:\PF`},
			want: slices.Concat(
				[]string{filepath.Join(`C:\PF`, edgeRel)},
				[]string{filepath.Join(`C:\PF`, chromeRel)},
				[]string{filepath.Join(`C:\PF`, braveRel)},
				[]string{filepath.Join(`C:\PF`, chromiumRel)},
			),
		},
		{
			name: "linux : l'ordre de la liste d'origine, Chrome puis Chromium puis Edge",
			goos: "linux",
			want: []string{
				"google-chrome", "google-chrome-stable",
				"chromium", "chromium-browser",
				"microsoft-edge", "microsoft-edge-stable",
				"brave-browser",
			},
		},
		{
			name: "darwin : Chrome d'abord, /Applications avant ~/Applications",
			goos: "darwin", home: "/Users/x",
			want: []string{
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				filepath.Join("/Users/x", "Applications", "Google Chrome.app/Contents/MacOS/Google Chrome"),
				"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
				filepath.Join("/Users/x", "Applications", "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
				"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
				filepath.Join("/Users/x", "Applications", "Brave Browser.app/Contents/MacOS/Brave Browser"),
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
				filepath.Join("/Users/x", "Applications", "Chromium.app/Contents/MacOS/Chromium"),
			},
		},
		{
			name: "darwin : sans foyer connu, seules les applications système sont proposées",
			goos: "darwin",
			want: []string{
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
				"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, n := range appWindowBrowserOrder(c.goos) {
				got = append(got, browserCandidates(n, c.goos, c.home, fakeEnv(c.env))...)
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("ordre des candidats modifié :\n got  %q\n want %q", got, c.want)
			}
		})
	}
}

// Le défaut d'origine, en une assertion : sous Windows, le premier candidat de la
// fenêtre est Edge. Le réglage `link_browser` ne doit pas toucher à ceci — il ne
// déplace que les liens cliqués.
func TestAppWindowStaysEdgeFirstOnWindows(t *testing.T) {
	env := fakeEnv(map[string]string{"ProgramFiles": `C:\PF`})
	cands := browserCandidates(appWindowBrowserOrder("windows")[0], "windows", "", env)
	if len(cands) == 0 || !strings.Contains(strings.ToLower(cands[0]), "msedge") {
		t.Fatalf("la fenêtre ne cherche plus Edge en premier : %q", cands)
	}
}

// resolveNamedBrowser ne doit jamais rendre un navigateur d'une autre famille :
// c'est très exactement le défaut signalé — demander Chrome et recevoir Edge.
func TestResolveNamedBrowserStaysInFamily(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("chemins de bundles non vérifiables hors macOS")
	}
	familles := map[string][]string{
		"chrome":   {"chrome", "google-chrome"},
		"edge":     {"msedge", "microsoft-edge"},
		"brave":    {"brave"},
		"chromium": {"chromium"},
	}
	for nom, marques := range familles {
		t.Run(nom, func(t *testing.T) {
			got := resolveNamedBrowser(nom)
			if got == "" {
				t.Skipf("%s n'est pas installé sur cette machine", nom)
			}
			bas := strings.ToLower(filepath.Base(got))
			if !slices.ContainsFunc(marques, func(m string) bool { return strings.Contains(bas, m) }) {
				t.Fatalf("resolveNamedBrowser(%q) a rendu %q", nom, got)
			}
		})
	}
}

func TestBrowserCandidatesNomInconnu(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if got := browserCandidates("firefox", goos, "/home/x", fakeEnv(nil)); got != nil {
			t.Errorf("%s : un nom hors famille doit ne rien proposer, obtenu %q", goos, got)
		}
	}
}
