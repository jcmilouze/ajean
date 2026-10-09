package ajean

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
	"strings"
	"sync"
	"time"
)

const (
	hookDefaultTimeout = 10
	hookMaxTimeout     = 60
	hookMaxOutput      = 64 * 1024
	hookMaxReason      = 500
)

type hookSpec struct {
	Matcher    string `json:"matcher"`
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeoutSec"`
	OnError    string `json:"onError"`
	re         *regexp.Regexp
}

type hookConfig map[string][]hookSpec

// hookEvent est ce que le hook reçoit sur stdin. Result/Error ne servent qu'à PostToolUse.
type hookEvent struct {
	Event  string         `json:"event"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
	Label  string         `json:"label"`
	Cwd    string         `json:"cwd"`
	Jean   bool           `json:"jean"`
	Result string         `json:"result,omitempty"`
	Error  bool           `json:"error,omitempty"`
}

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

// runPreHooks exécute, dans l'ordre, les hooks PreToolUse qui visent cet outil.
// Renvoie "" si l'appel est autorisé, sinon le résultat « [erreur] … » à donner
// au modèle à la place de l'exécution.
func runPreHooks(ctx context.Context, ev hookEvent) string {
	ev.Event = "PreToolUse"
	if ev.Cwd == "" {
		ev.Cwd = spaceWorkspace(ctx)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return ""
	}
	for _, h := range loadHooks()["PreToolUse"] {
		if !h.re.MatchString(ev.Tool) {
			continue
		}
		deny, herr := runHook(ctx, h, payload)
		switch {
		case herr != nil && h.OnError == "deny":
			return "[erreur] hook en échec (onError=deny) : " + clipReason(herr.Error())
		case herr != nil:
			fmt.Fprintf(os.Stderr, "[hooks] PreToolUse %q ignoré : %v\n", h.Command, herr)
		case deny != "":
			return "[erreur] bloqué par un hook : " + clipReason(deny)
		}
	}
	return ""
}

// runHook lance un hook et renvoie la raison du refus ("" = autorisé), ou une
// erreur si le hook lui-même a échoué (plantage, délai, sortie illisible).
func runHook(parent context.Context, h hookSpec, payload []byte) (string, error) {
	to := h.TimeoutSec
	if to <= 0 {
		to = hookDefaultTimeout
	}
	if to > hookMaxTimeout {
		to = hookMaxTimeout
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(to)*time.Second)
	defer cancel()
	cmd, cleanup := newShellCmd(ctx, h.Command)
	defer cleanup()
	var stdout, stderr cappedBuffer
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// Même garde que runShell : un process laissé en arrière-plan ne doit pas
	// retenir les tubes, donc le tour, indéfiniment.
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("délai de %ds dépassé", to)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() == 2 {
			return nonEmpty(strings.TrimSpace(stderr.String()), "refusé (code 2)"), nil
		}
		return "", fmt.Errorf("code de sortie %d", ee.ExitCode())
	}
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return "", err
	}
	return parseDecision(stdout.String())
}

func parseDecision(out string) (string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return "", nil
	}
	var d struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return "", fmt.Errorf("sortie JSON illisible : %v", err)
	}
	switch d.Decision {
	case "allow":
		return "", nil
	case "deny":
		return nonEmpty(d.Reason, "refusé"), nil
	}
	return "", fmt.Errorf("decision inconnue %q", d.Decision)
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func clipReason(s string) string {
	r := []rune(s)
	if len(r) <= hookMaxReason {
		return s
	}
	return string(r[:hookMaxReason]) + "…"
}

// cappedBuffer borne la sortie d'un hook : le surplus est jeté, sans erreur
// (une erreur d'écriture ferait échouer le process).
type cappedBuffer struct{ bytes.Buffer }

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := hookMaxOutput - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
