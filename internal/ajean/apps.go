package ajean

// apps.go — les « apps » : petites applications (HTML + CSS + JS) que l'IA crée
// à la demande (« crée-moi une app qui… ») et que l'utilisateur ouvre depuis
// l'écran Applications. Une app n'a aucun réseau : elle récupère ses données par
// des SCRIPTS que l'IA écrit pour elle, rangés avec l'app.
//
// Zone dédiée, hors du workspace jetable (comme scripts/) :
//
//	AJEAN_HOME/apps/<id>/
//	  app.json            nom, description, icône, version courante, historique
//	  versions/<n>/       index.html, style.css, app.js (jamais modifiés ensuite)
//	  scripts/            scripts de l'app (lancés par ajean.scripts.run)
//	  data/               dossier de travail des scripts + storage.json
//
// Ce dossier est « tool-only » (voir toolOnlyDirs) : bash/write/edit n'y touchent
// pas, tout passe par l'outil `app` et l'API /api/apps. Un ménage du workspace ne
// peut donc pas effacer une app.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appMaxApps      = 100
	appMaxVersions  = 30
	appMaxCodeBytes = 256 << 10 // HTML + CSS + JS d'une version
	appMaxScript    = 128 << 10
	appMaxScripts   = 30
	appMaxStorage   = 1 << 20 // storage.json d'une app
	appScriptMaxOut = 1 << 20 // sortie standard rendue à l'app
	appScriptTO     = 60 * time.Second
	appScriptMaxTO  = 300 * time.Second
)

// appsMu sérialise les écritures (création, version, scripts, storage) : deux
// tours concurrents ne doivent pas se marcher dessus sur app.json.
var appsMu sync.Mutex

func appsDir() string { return filepath.Join(AjeanHome(), "apps") }

var appIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
var appScriptRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}\.(py|js|sh|ps1|bat|cmd)$`)

// AppMeta est le contenu de app.json.
type AppMeta struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Icon        string       `json:"icon,omitempty"` // un emoji
	Version     int          `json:"version"`        // version courante
	Last        int          `json:"last"`           // dernier numéro attribué
	Versions    []AppVersion `json:"versions"`
	Created     int64        `json:"created"`
	Updated     int64        `json:"updated"`
	Opens       int          `json:"opens"`
}

type AppVersion struct {
	N    int    `json:"n"`
	Note string `json:"note,omitempty"`
	At   int64  `json:"at"`
}

// AppCode est le code d'une version.
type AppCode struct {
	HTML string `json:"html"`
	CSS  string `json:"css"`
	JS   string `json:"js"`
}

func appDir(id string) (string, error) {
	if !appIDRe.MatchString(id) {
		return "", fmt.Errorf("identifiant d'app invalide : %q", id)
	}
	return filepath.Join(appsDir(), id), nil
}

func readAppMeta(id string) (*AppMeta, error) {
	d, err := appDir(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(d, "app.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("app introuvable : %s", id)
		}
		return nil, err
	}
	var m AppMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("app.json illisible (%s) : %v", id, err)
	}
	m.ID = id
	return &m, nil
}

func writeAppMeta(m *AppMeta) error {
	d, err := appDir(m.ID)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return writeFileAtomicApp(filepath.Join(d, "app.json"), b)
}

func writeFileAtomicApp(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// listApps renvoie toutes les apps, les plus récemment modifiées d'abord.
func listApps() []AppMeta {
	ents, _ := os.ReadDir(appsDir())
	out := []AppMeta{}
	for _, e := range ents {
		if !e.IsDir() || !appIDRe.MatchString(e.Name()) {
			continue
		}
		if m, err := readAppMeta(e.Name()); err == nil {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out
}

func readAppCode(id string, n int) (*AppCode, error) {
	d, err := appDir(id)
	if err != nil {
		return nil, err
	}
	vd := filepath.Join(d, "versions", strconv.Itoa(n))
	if _, err := os.Stat(vd); err != nil {
		return nil, fmt.Errorf("version %d introuvable pour l'app %s", n, id)
	}
	rd := func(f string) string { b, _ := os.ReadFile(filepath.Join(vd, f)); return string(b) }
	return &AppCode{HTML: rd("index.html"), CSS: rd("style.css"), JS: rd("app.js")}, nil
}

var (
	appHTMLScriptRe  = regexp.MustCompile(`(?i)<\s*(script|meta|base|iframe|object|embed)\b`)
	appHTMLHandlerRe = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	appCloseTagRe    = regexp.MustCompile(`(?i)</\s*(script|style)`)
)

// validateAppCode contrôle une version avant enregistrement. Les messages sont
// écrits pour que l'IA puisse corriger d'elle-même.
func validateAppCode(c *AppCode) error {
	if strings.TrimSpace(c.HTML) == "" && strings.TrimSpace(c.JS) == "" {
		return errors.New("l'app est vide : fournis au moins html ou js")
	}
	if len(c.HTML)+len(c.CSS)+len(c.JS) > appMaxCodeBytes {
		return fmt.Errorf("code trop gros (%d Ko, max %d Ko pour html+css+js)", (len(c.HTML)+len(c.CSS)+len(c.JS))>>10, appMaxCodeBytes>>10)
	}
	if m := appHTMLScriptRe.FindString(c.HTML); m != "" {
		return fmt.Errorf("html : balise interdite %q. Le JS va dans la partie js, pas de <script>, <meta>, <base>, <iframe>", m)
	}
	if appHTMLHandlerRe.MatchString(c.HTML) {
		return errors.New("html : attributs on…= interdits (onclick, onload…). Branche les événements dans la partie js avec addEventListener")
	}
	if appCloseTagRe.MatchString(c.CSS) || appCloseTagRe.MatchString(c.JS) {
		return errors.New("css/js : la séquence </script ou </style est interdite (écris '<\\/script' dans une chaîne si besoin)")
	}
	return nil
}

func appNow() int64 { return time.Now().UnixMilli() }

// createApp crée une app et sa version 1. id dérivé du nom (unique).
func createApp(name, desc, icon string, c *AppCode, note string) (*AppMeta, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nom manquant")
	}
	if err := validateAppCode(c); err != nil {
		return nil, err
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	if len(listApps()) >= appMaxApps {
		return nil, fmt.Errorf("limite de %d apps atteinte : demande à l'utilisateur d'en supprimer une", appMaxApps)
	}
	base := slugify(name)
	if base == "" || !appIDRe.MatchString(base) {
		base = "app"
	}
	id := base
	for i := 2; ; i++ {
		d, _ := appDir(id)
		if _, err := os.Stat(d); os.IsNotExist(err) {
			break
		}
		suf := "-" + strconv.Itoa(i)
		if len(base)+len(suf) > 40 {
			id = base[:40-len(suf)] + suf
		} else {
			id = base + suf
		}
	}
	now := appNow()
	m := &AppMeta{ID: id, Name: clipRunes(name, 60), Description: clipRunes(strings.TrimSpace(desc), 300), Icon: appIcon(icon), Created: now}
	if err := saveAppVersion(m, c, note); err != nil {
		return nil, err
	}
	d, _ := appDir(id)
	_ = os.MkdirAll(filepath.Join(d, "scripts"), 0o755)
	_ = os.MkdirAll(filepath.Join(d, "data"), 0o755)
	return m, nil
}

// saveAppVersion écrit une nouvelle version (numéro suivant le dernier), la rend
// courante, et élague les plus anciennes au-delà de appMaxVersions. appsMu tenu.
func saveAppVersion(m *AppMeta, c *AppCode, note string) error {
	d, err := appDir(m.ID)
	if err != nil {
		return err
	}
	n := m.Last + 1
	vd := filepath.Join(d, "versions", strconv.Itoa(n))
	if err := os.MkdirAll(vd, 0o755); err != nil {
		return err
	}
	for f, s := range map[string]string{"index.html": c.HTML, "style.css": c.CSS, "app.js": c.JS} {
		if err := os.WriteFile(filepath.Join(vd, f), []byte(s), 0o644); err != nil {
			return err
		}
	}
	m.Last, m.Version, m.Updated = n, n, appNow()
	m.Versions = append(m.Versions, AppVersion{N: n, Note: clipRunes(strings.TrimSpace(note), 200), At: m.Updated})
	for len(m.Versions) > appMaxVersions {
		old := m.Versions[0]
		m.Versions = m.Versions[1:]
		if old.N != m.Version {
			_ = os.RemoveAll(filepath.Join(d, "versions", strconv.Itoa(old.N)))
		}
	}
	return writeAppMeta(m)
}

// AppPatch décrit une modification : parties entières et/ou retouches ciblées.
type AppPatch struct {
	HTML, CSS, JS *string
	Edits         []AppEdit
}

type AppEdit struct {
	Part string `json:"part"` // html | css | js
	Old  string `json:"old"`
	New  string `json:"new"`
}

func updateApp(id string, p AppPatch, note string) (*AppMeta, error) {
	appsMu.Lock()
	defer appsMu.Unlock()
	m, err := readAppMeta(id)
	if err != nil {
		return nil, err
	}
	c, err := readAppCode(id, m.Version)
	if err != nil {
		return nil, err
	}
	if p.HTML != nil {
		c.HTML = *p.HTML
	}
	if p.CSS != nil {
		c.CSS = *p.CSS
	}
	if p.JS != nil {
		c.JS = *p.JS
	}
	for i, e := range p.Edits {
		var tgt *string
		switch strings.ToLower(e.Part) {
		case "html":
			tgt = &c.HTML
		case "css":
			tgt = &c.CSS
		case "js":
			tgt = &c.JS
		default:
			return nil, fmt.Errorf("retouche %d : part doit être html, css ou js", i+1)
		}
		if e.Old == "" {
			return nil, fmt.Errorf("retouche %d : old vide", i+1)
		}
		switch strings.Count(*tgt, e.Old) {
		case 0:
			return nil, fmt.Errorf("retouche %d : extrait introuvable dans la partie %s.%s", i+1, e.Part, appNearHint(*tgt, e.Old))
		case 1:
			*tgt = strings.Replace(*tgt, e.Old, e.New, 1)
		default:
			return nil, fmt.Errorf("retouche %d : extrait présent plusieurs fois dans %s, donne un extrait plus long", i+1, e.Part)
		}
	}
	if p.HTML == nil && p.CSS == nil && p.JS == nil && len(p.Edits) == 0 {
		return m, nil
	}
	if err := validateAppCode(c); err != nil {
		return nil, err
	}
	if err := saveAppVersion(m, c, note); err != nil {
		return nil, err
	}
	return m, nil
}

// setAppMeta change nom / description / icône sans créer de version.
func setAppMeta(id string, name, desc, icon *string) (*AppMeta, error) {
	appsMu.Lock()
	defer appsMu.Unlock()
	m, err := readAppMeta(id)
	if err != nil {
		return nil, err
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		m.Name = clipRunes(strings.TrimSpace(*name), 60)
	}
	if desc != nil {
		m.Description = clipRunes(strings.TrimSpace(*desc), 300)
	}
	if icon != nil {
		m.Icon = appIcon(*icon)
	}
	return m, writeAppMeta(m)
}

func restoreAppVersion(id string, n int) (*AppMeta, error) {
	appsMu.Lock()
	defer appsMu.Unlock()
	m, err := readAppMeta(id)
	if err != nil {
		return nil, err
	}
	c, err := readAppCode(id, n)
	if err != nil {
		return nil, err
	}
	if err := saveAppVersion(m, c, fmt.Sprintf("retour à la version %d", n)); err != nil {
		return nil, err
	}
	return m, nil
}

func deleteApp(id string) error {
	appsMu.Lock()
	defer appsMu.Unlock()
	d, err := appDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(d, "app.json")); err != nil {
		return fmt.Errorf("app introuvable : %s", id)
	}
	return os.RemoveAll(d)
}

func countAppOpen(id string) {
	appsMu.Lock()
	defer appsMu.Unlock()
	if m, err := readAppMeta(id); err == nil {
		m.Opens++
		_ = writeAppMeta(m)
	}
}

// appIcon garde un emoji court (l'icône de la tuile). Vide → icône par défaut.
func appIcon(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 32 || strings.ContainsAny(s, "<>\"'&") {
		return "🧩"
	}
	return s
}

// ---------- scripts ----------

func appScriptPath(id, name string) (string, error) {
	if !appScriptRe.MatchString(name) {
		return "", fmt.Errorf("nom de script invalide %q : lettres, chiffres, - et _, extension .py .js .sh .ps1 .bat ou .cmd", name)
	}
	ext := filepath.Ext(name)
	if runtime.GOOS != "windows" && (ext == ".bat" || ext == ".cmd") {
		return "", fmt.Errorf("%s : les .bat/.cmd ne tournent que sous Windows", name)
	}
	d, err := appDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "scripts", name), nil
}

type AppScript struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func listAppScripts(id string) []AppScript {
	d, err := appDir(id)
	if err != nil {
		return nil
	}
	ents, _ := os.ReadDir(filepath.Join(d, "scripts"))
	out := []AppScript{}
	for _, e := range ents {
		if e.IsDir() || !appScriptRe.MatchString(e.Name()) {
			continue
		}
		fi, _ := e.Info()
		var sz int64
		if fi != nil {
			sz = fi.Size()
		}
		out = append(out, AppScript{Name: e.Name(), Size: sz})
	}
	return out
}

func writeAppScript(id, name, content string) error {
	if _, err := readAppMeta(id); err != nil {
		return err
	}
	p, err := appScriptPath(id, name)
	if err != nil {
		return err
	}
	if len(content) > appMaxScript {
		return fmt.Errorf("script trop gros (max %d Ko)", appMaxScript>>10)
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	if _, err := os.Stat(p); os.IsNotExist(err) && len(listAppScripts(id)) >= appMaxScripts {
		return fmt.Errorf("limite de %d scripts par app atteinte", appMaxScripts)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o755)
}

func deleteAppScript(id, name string) error {
	p, err := appScriptPath(id, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("script introuvable : %s", name)
	}
	return nil
}

// appScriptCmd construit la commande qui lance le script selon son extension,
// SANS shell : le chemin est un argument, aucune interprétation possible.
func appScriptCmd(ctx context.Context, path string) *exec.Cmd {
	win := runtime.GOOS == "windows"
	var cmd *exec.Cmd
	switch filepath.Ext(path) {
	case ".py":
		py := "python3"
		if win {
			py = "python"
		}
		cmd = exec.CommandContext(ctx, py, "-X", "utf8", path)
	case ".js":
		cmd = exec.CommandContext(ctx, "node", path)
	case ".ps1":
		if win {
			cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
		} else {
			cmd = exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-File", path)
		}
	case ".bat", ".cmd":
		cmd = exec.CommandContext(ctx, "cmd", "/C", path)
	default: // .sh
		cmd = exec.CommandContext(ctx, "bash", path)
	}
	return hideCmd(cmd)
}

// AppRunResult est ce que reçoit l'app (et l'IA via script_test).
type AppRunResult struct {
	OK        bool   `json:"ok"`
	Exit      int    `json:"exit"`
	Data      any    `json:"data,omitempty"` // stdout lu comme JSON quand c'en est
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr,omitempty"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Ms        int64  `json:"ms"`
}

type capBuf struct {
	b   bytes.Buffer
	max int
	cut bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
			c.cut = true
		} else {
			c.b.Write(p)
		}
	} else if len(p) > 0 {
		c.cut = true
	}
	return len(p), nil
}

// runAppScript lance un script de l'app. L'entrée (JSON) arrive sur stdin et
// dans la variable AJEAN_APP_INPUT ; jamais dans la ligne de commande. Le script
// tourne dans le dossier data/ de l'app (AJEAN_APP_DATA).
func runAppScript(parent context.Context, id, name string, input any, timeout time.Duration) AppRunResult {
	start := time.Now()
	res := func(r AppRunResult) AppRunResult { r.Ms = time.Since(start).Milliseconds(); return r }
	p, err := appScriptPath(id, name)
	if err != nil {
		return res(AppRunResult{Exit: -1, Error: err.Error()})
	}
	if _, err := os.Stat(p); err != nil {
		return res(AppRunResult{Exit: -1, Error: "script introuvable : " + name})
	}
	in := []byte("null")
	if input != nil {
		if b, err := json.Marshal(input); err == nil {
			in = b
		}
	}
	if len(in) > 4<<20 {
		return res(AppRunResult{Exit: -1, Error: "entrée trop grosse (max 4 Mo) : passe les images et documents par ajean.files, le script les lit dans AJEAN_APP_FILES"})
	}
	if timeout <= 0 {
		timeout = appScriptTO
	}
	if timeout > appScriptMaxTO {
		timeout = appScriptMaxTO
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	d, _ := appDir(id)
	data := filepath.Join(d, "data")
	_ = os.MkdirAll(data, 0o755)
	cmd := appScriptCmd(ctx, p)
	cmd.Dir = data
	files, _ := appFilesDir(id)
	_ = os.MkdirAll(files, 0o755)
	cmd.Env = append(os.Environ(), "AJEAN_APP_ID="+id, "AJEAN_APP_DATA="+data, "AJEAN_APP_FILES="+files, "PYTHONIOENCODING=utf-8")
	// Une variable d'environnement de plus de 128 Ko fait échouer le lancement
	// sous Linux (E2BIG) : au-delà, l'entrée n'arrive que par stdin.
	if len(in) <= 32<<10 {
		cmd.Env = append(cmd.Env, "AJEAN_APP_INPUT="+string(in))
	}
	cmd.Stdin = bytes.NewReader(in)
	out := &capBuf{max: appScriptMaxOut}
	errb := &capBuf{max: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, errb
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	r := AppRunResult{Stdout: out.b.String(), Stderr: errb.b.String(), Truncated: out.cut}
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.Exit, r.Error = -1, fmt.Sprintf("délai dépassé (%ds)", int(timeout/time.Second))
	case err != nil:
		if ee, ok := err.(*exec.ExitError); ok {
			r.Exit = ee.ExitCode()
			r.Error = fmt.Sprintf("le script a quitté avec le code %d", r.Exit)
		} else {
			r.Exit, r.Error = -1, "lancement impossible : "+err.Error()
		}
	default:
		r.OK = true
	}
	if s := strings.TrimSpace(r.Stdout); s != "" && !r.Truncated && (s[0] == '{' || s[0] == '[' || s[0] == '"') {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			r.Data = v
		}
	}
	return res(r)
}

// ---------- storage ----------

func appStoragePath(id string) (string, error) {
	d, err := appDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "data", "storage.json"), nil
}

func readAppStorage(id string) (map[string]json.RawMessage, error) {
	p, err := appStoragePath(id)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	b, err := os.ReadFile(p)
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m, nil
}

// appStorageOp applique get/set/delete/list au storage de l'app.
func appStorageOp(id, op, key string, value json.RawMessage, prefix string) (any, error) {
	if _, err := readAppMeta(id); err != nil {
		return nil, err
	}
	if op != "list" && (key == "" || len(key) > 200) {
		return nil, errors.New("clé vide ou trop longue (200 caractères max)")
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	m, err := readAppStorage(id)
	if err != nil {
		return nil, err
	}
	save := func() error {
		b, _ := json.Marshal(m)
		if len(b) > appMaxStorage {
			return fmt.Errorf("mémoire de l'app pleine (%d Ko max)", appMaxStorage>>10)
		}
		p, _ := appStoragePath(id)
		return writeFileAtomicApp(p, b)
	}
	switch op {
	case "get":
		if v, ok := m[key]; ok {
			return v, nil
		}
		return nil, nil
	case "set":
		if len(value) == 0 || !json.Valid(value) {
			return nil, errors.New("valeur JSON invalide")
		}
		m[key] = value
		return true, save()
	case "delete":
		_, had := m[key]
		delete(m, key)
		return had, save()
	case "list":
		keys := []string{}
		for k := range m {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		return keys, nil
	}
	return nil, fmt.Errorf("opération inconnue : %s", op)
}

// appNearHint : quand une retouche ne trouve pas son extrait, montre où se
// trouve sa première ligne dans le code actuel (souvent un espace ou une
// indentation qui diffère), pour corriger sans relire toute la partie.
func appNearHint(code, old string) string {
	first := ""
	for _, l := range strings.Split(old, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			first = t
			break
		}
	}
	if first == "" {
		return ""
	}
	lines := strings.Split(code, "\n")
	for i, l := range lines {
		if strings.Contains(l, first) {
			lo, hi := i-2, i+3
			if lo < 0 {
				lo = 0
			}
			if hi > len(lines) {
				hi = len(lines)
			}
			var b strings.Builder
			fmt.Fprintf(&b, " Sa première ligne est à la ligne %d ; le texte exact actuel autour :\n", i+1)
			for j := lo; j < hi; j++ {
				fmt.Fprintf(&b, "%d| %s\n", j+1, lines[j])
			}
			return b.String()
		}
	}
	return " Sa première ligne n'existe pas non plus : relis la partie (action=read, part=…)."
}

// ---------- fichiers ----------
//
// Fichiers d'une app (photos, PDF produits, exports…) : data/files/ de l'app.
// L'app les écrit et les relit par le pont (ajean.files, en morceaux pour
// passer le relais ajean.link), ses scripts les trouvent dans AJEAN_APP_FILES.
// C'est ce qui évite de faire transiter des images en base64 dans l'entrée JSON
// d'un script, et qui permet de télécharger ou partager un PDF produit.

const (
	appMaxFile       = 50 << 20  // un fichier
	appMaxFilesTotal = 500 << 20 // tous les fichiers d'une app
	appFileChunk     = 4 << 20   // un morceau lu ou écrit par le pont
)

var appFileRe = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N} _.,()'+-]{0,119}$`)

type AppFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

func appFilesDir(id string) (string, error) {
	d, err := appDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "data", "files"), nil
}

func appFilePath(id, name string) (string, error) {
	if !appFileRe.MatchString(name) || strings.Contains(name, "..") {
		return "", fmt.Errorf("nom de fichier invalide %q : lettres, chiffres, espaces et . _ - ( ) , ' +, sans dossier", name)
	}
	d, err := appFilesDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

func listAppFiles(id string) []AppFile {
	d, err := appFilesDir(id)
	out := []AppFile{}
	if err != nil {
		return out
	}
	ents, _ := os.ReadDir(d)
	for _, e := range ents {
		if e.IsDir() || !appFileRe.MatchString(e.Name()) {
			continue
		}
		if fi, err := e.Info(); err == nil {
			out = append(out, AppFile{Name: e.Name(), Size: fi.Size(), Modified: fi.ModTime().UnixMilli()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out
}

// writeAppFileChunk écrit un morceau : offset 0 (re)crée le fichier, sinon le
// morceau doit suivre exactement la fin actuelle.
func writeAppFileChunk(id, name string, offset int64, b []byte) (int64, error) {
	if _, err := readAppMeta(id); err != nil {
		return 0, err
	}
	p, err := appFilePath(id, name)
	if err != nil {
		return 0, err
	}
	if len(b) > appFileChunk {
		return 0, fmt.Errorf("morceau trop gros (%d Mo max)", appFileChunk>>20)
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	var cur int64
	if fi, err := os.Stat(p); err == nil {
		cur = fi.Size()
	}
	if offset != 0 && offset != cur {
		return 0, fmt.Errorf("morceau hors séquence (attendu à %d, reçu à %d)", cur, offset)
	}
	if offset+int64(len(b)) > appMaxFile {
		return 0, fmt.Errorf("fichier trop gros (%d Mo max)", appMaxFile>>20)
	}
	var total int64
	for _, f := range listAppFiles(id) {
		if f.Name != name {
			total += f.Size
		}
	}
	if total+offset+int64(len(b)) > appMaxFilesTotal {
		return 0, fmt.Errorf("fichiers de l'app pleins (%d Mo max) : supprimer d'anciens fichiers", appMaxFilesTotal>>20)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if offset == 0 {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(p, flag, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return 0, err
	}
	return offset + int64(len(b)), nil
}

// readAppFileChunk lit au plus length octets à partir de offset.
func readAppFileChunk(id, name string, offset, length int64) ([]byte, int64, error) {
	p, err := appFilePath(id, name)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, fmt.Errorf("fichier introuvable : %s", name)
	}
	defer f.Close()
	fi, _ := f.Stat()
	size := fi.Size()
	if length <= 0 || length > appFileChunk {
		length = appFileChunk
	}
	if offset < 0 || offset > size {
		offset = size
	}
	if offset+length > size {
		length = size - offset
	}
	b := make([]byte, length)
	n, err := f.ReadAt(b, offset)
	if err != nil && n != len(b) {
		return nil, size, err
	}
	return b[:n], size, nil
}

func deleteAppFile(id, name string) error {
	p, err := appFilePath(id, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("fichier introuvable : %s", name)
	}
	return nil
}
