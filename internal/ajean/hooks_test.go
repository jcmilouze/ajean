package ajean

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHookHelperProcess est le « hook » lancé par les tests : le binaire de test
// lui-même, ce qui évite tout script dépendant de la plateforme. Il sort TOUJOURS
// par os.Exit, sinon le runner de test ajouterait « PASS » sur stdout.
func TestHookHelperProcess(t *testing.T) {
	i := -1
	for n, a := range os.Args {
		if a == "--" {
			i = n
		}
	}
	if i < 0 || i+1 >= len(os.Args) {
		return
	}
	mode := os.Args[i+1]
	switch {
	case mode == "allow":
		os.Exit(0)
	case mode == "allow-json":
		fmt.Print(`{"decision":"allow"}`)
		os.Exit(0)
	case mode == "deny-json":
		fmt.Print(`{"decision":"deny","reason":"nope"}`)
		os.Exit(0)
	case mode == "deny2":
		fmt.Fprint(os.Stderr, "interdit")
		os.Exit(2)
	case mode == "crash":
		os.Exit(1)
	case mode == "garbage":
		fmt.Print("not json")
		os.Exit(0)
	case mode == "slow":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case strings.HasPrefix(mode, "dump:"):
		b, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(strings.TrimPrefix(mode, "dump:"), b, 0o644)
		os.Exit(0)
	}
	os.Exit(0)
}

func helperCmd(mode string) string {
	return fmt.Sprintf(`"%s" -test.run=TestHookHelperProcess -- %s`, os.Args[0], mode)
}

func writeHooksJSON(t *testing.T, cfg any) {
	t.Helper()
	t.Setenv("AJEAN_HOME", t.TempDir())
	t.Setenv("AJEAN_NO_HOOKS", "")
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func preHook(matcher, mode string, extra map[string]any) map[string]any {
	h := map[string]any{"matcher": matcher, "command": helperCmd(mode)}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func preCfg(hooks ...map[string]any) map[string]any {
	return map[string]any{"PreToolUse": hooks}
}

func bashEvent() hookEvent {
	return hookEvent{Tool: "bash", Args: map[string]any{"command": "rm -rf build"}, Label: "rm -rf build"}
}

func TestPreHooksNoConfigAllows(t *testing.T) {
	t.Setenv("AJEAN_HOME", t.TempDir())
	if got := runPreHooks(context.Background(), bashEvent()); got != "" {
		t.Fatalf("sans hooks.json l'appel doit passer, reçu %q", got)
	}
}

func TestPreHooksAllow(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^bash$", "allow", nil), preHook(".*", "allow-json", nil)))
	if got := runPreHooks(context.Background(), bashEvent()); got != "" {
		t.Fatalf("attendu autorisé, reçu %q", got)
	}
}

func TestPreHooksDenyExitCode2UsesStderrAsReason(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^bash$", "deny2", nil)))
	got := runPreHooks(context.Background(), bashEvent())
	if !strings.HasPrefix(got, "[erreur]") || !strings.Contains(got, "interdit") {
		t.Fatalf("refus attendu avec la raison de stderr, reçu %q", got)
	}
}

func TestPreHooksDenyJSON(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^bash$", "deny-json", nil)))
	got := runPreHooks(context.Background(), bashEvent())
	if !strings.HasPrefix(got, "[erreur]") || !strings.Contains(got, "nope") {
		t.Fatalf("refus attendu avec la raison du JSON, reçu %q", got)
	}
}

func TestPreHooksMatcherSkipsOtherTools(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^(write|edit)$", "deny2", nil)))
	if got := runPreHooks(context.Background(), bashEvent()); got != "" {
		t.Fatalf("le matcher ne couvre pas bash, reçu %q", got)
	}
}

func TestPreHooksFirstDenyStopsChain(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "second-ran")
	writeHooksJSON(t, preCfg(
		preHook(".*", "deny2", nil),
		preHook(".*", "dump:"+marker, nil),
	))
	if got := runPreHooks(context.Background(), bashEvent()); got == "" {
		t.Fatal("refus attendu")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("le second hook ne devait pas tourner après un refus")
	}
}

func TestPreHooksErrorPolicy(t *testing.T) {
	for _, mode := range []string{"crash", "garbage"} {
		writeHooksJSON(t, preCfg(preHook(".*", mode, nil)))
		if got := runPreHooks(context.Background(), bashEvent()); got != "" {
			t.Fatalf("%s : onError par défaut = allow, reçu %q", mode, got)
		}
		writeHooksJSON(t, preCfg(preHook(".*", mode, map[string]any{"onError": "deny"})))
		if got := runPreHooks(context.Background(), bashEvent()); !strings.HasPrefix(got, "[erreur]") {
			t.Fatalf("%s : onError=deny doit refuser, reçu %q", mode, got)
		}
	}
}

func TestPreHooksTimeoutFollowsErrorPolicy(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook(".*", "slow", map[string]any{"timeoutSec": 1, "onError": "deny"})))
	start := time.Now()
	got := runPreHooks(context.Background(), bashEvent())
	if !strings.HasPrefix(got, "[erreur]") {
		t.Fatalf("timeout avec onError=deny doit refuser, reçu %q", got)
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Fatalf("le délai n'a pas été appliqué : %v", d)
	}
}

func TestPreHooksReceiveEventOnStdin(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "stdin.json")
	writeHooksJSON(t, preCfg(preHook("^bash$", "dump:"+dump, nil)))
	ev := bashEvent()
	ev.Jean = true
	runPreHooks(context.Background(), ev)
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Event string
		Tool  string
		Args  map[string]any
		Label string
		Jean  bool
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("stdin illisible : %v (%s)", err, b)
	}
	if got.Event != "PreToolUse" || got.Tool != "bash" || got.Args["command"] != "rm -rf build" || got.Label != "rm -rf build" || !got.Jean {
		t.Fatalf("événement incorrect : %+v", got)
	}
}

func TestPreHooksKillSwitch(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook(".*", "deny2", nil)))
	t.Setenv("AJEAN_NO_HOOKS", "1")
	if got := runPreHooks(context.Background(), bashEvent()); got != "" {
		t.Fatalf("AJEAN_NO_HOOKS=1 doit tout désactiver, reçu %q", got)
	}
}

func TestPreHooksReloadOnFileChange(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook(".*", "allow", nil)))
	if got := runPreHooks(context.Background(), bashEvent()); got != "" {
		t.Fatalf("attendu autorisé, reçu %q", got)
	}
	b, _ := json.Marshal(preCfg(preHook(".*", "deny2", nil)))
	p := filepath.Join(hooksDir(), "hooks.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, future, future)
	if got := runPreHooks(context.Background(), bashEvent()); got == "" {
		t.Fatal("le hooks.json modifié doit être relu")
	}
}

func TestHooksDirIsToolOnly(t *testing.T) {
	t.Setenv("AJEAN_HOME", t.TempDir())
	target := filepath.Join(hooksDir(), "hooks.json")
	if msg := guardToolOnlyPath(target); msg == "" {
		t.Fatal("write/edit ne doivent pas pouvoir toucher hooks/")
	}
	if msg := guardToolOnlyCommand("del " + target); msg == "" {
		t.Fatal("bash ne doit pas pouvoir toucher hooks/")
	}
}
