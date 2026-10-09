package ajean

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type hookSpec struct {
	Matcher    string `json:"matcher"`
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeoutSec"`
	OnError    string `json:"onError"`
	Async      *bool  `json:"async"` // PostToolUse seulement ; asynchrone par défaut
	re         *regexp.Regexp
}

type hookConfig map[string][]hookSpec

func hooksDir() string { return filepath.Join(AjeanHome(), "hooks") }

var hookCache struct {
	sync.Mutex
	path  string
	mtime time.Time
	cfg   hookConfig
}

// loadHooks relit hooks.json seulement si son mtime a changé ; un fichier absent
// ou illisible donne une config vide (aucun hook, aucun coût).
func loadHooks() hookConfig {
	if os.Getenv("AJEAN_NO_HOOKS") != "" {
		return nil
	}
	p := filepath.Join(hooksDir(), "hooks.json")
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	hookCache.Lock()
	defer hookCache.Unlock()
	if hookCache.path == p && hookCache.mtime.Equal(fi.ModTime()) {
		return hookCache.cfg
	}
	cfg := parseHooks(p)
	hookCache.path, hookCache.mtime, hookCache.cfg = p, fi.ModTime(), cfg
	return cfg
}

func parseHooks(path string) hookConfig {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg hookConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[hooks] %s illisible : %v\n", path, err)
		return nil
	}
	for ev, specs := range cfg {
		kept := specs[:0]
		for _, s := range specs {
			re, err := regexp.Compile(s.Matcher)
			if err != nil || strings.TrimSpace(s.Command) == "" {
				fmt.Fprintf(os.Stderr, "[hooks] %s : hook ignoré (matcher %q invalide ou commande vide)\n", ev, s.Matcher)
				continue
			}
			s.re = re
			kept = append(kept, s)
		}
		cfg[ev] = kept
	}
	return cfg
}

// matchingHooks renvoie, dans l'ordre du fichier, les hooks de cet événement qui visent l'outil.
func matchingHooks(event, tool string) []hookSpec {
	var out []hookSpec
	for _, h := range loadHooks()[event] {
		if h.re.MatchString(tool) {
			out = append(out, h)
		}
	}
	return out
}
