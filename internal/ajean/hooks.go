package ajean

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	hookDefaultTimeout = 10
	hookMaxTimeout     = 60
	hookMaxOutput      = 64 * 1024
	hookMaxReason      = 500
)

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
	// TurnEnd seulement : ce que le hook apprend sur le tour qui vient de finir.
	DurationMs    int64  `json:"durationMs,omitempty"`
	ToolCalls     int    `json:"toolCalls,omitempty"`
	ContextTokens int    `json:"contextTokens,omitempty"` // taille du contexte en fin de tour
	Status        string `json:"status,omitempty"`        // ok | error | stopped
	Message       string `json:"message,omitempty"`       // texte de l'erreur
}

// runPreHooks exécute, dans l'ordre, les hooks PreToolUse qui visent cet outil.
// Renvoie "" si l'appel est autorisé, sinon le résultat « [erreur] … » à donner
// au modèle à la place de l'exécution.
func runPreHooks(ctx context.Context, ev hookEvent) string {
	ev.Event = "PreToolUse"
	specs := matchingHooks(ev.Event, ev.Tool)
	if len(specs) == 0 {
		return ""
	}
	payload, ok := hookPayload(ctx, ev)
	if !ok {
		return ""
	}
	for _, h := range specs {
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

func hookPayload(ctx context.Context, ev hookEvent) ([]byte, bool) {
	if ev.Cwd == "" {
		ev.Cwd = spaceWorkspace(ctx)
	}
	b, err := json.Marshal(ev)
	return b, err == nil
}

func hookTimeoutSec(h hookSpec) int {
	if h.TimeoutSec <= 0 {
		return hookDefaultTimeout
	}
	return min(h.TimeoutSec, hookMaxTimeout)
}

// runHook lance un hook et renvoie la raison du refus ("" = autorisé), ou une
// erreur si le hook lui-même a échoué (plantage, délai, sortie illisible).
func runHook(parent context.Context, h hookSpec, payload []byte) (string, error) {
	to := hookTimeoutSec(h)
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
