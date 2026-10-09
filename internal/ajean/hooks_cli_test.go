package ajean

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func listOutput(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := hooksList(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestHooksListWithoutConfigSaysSo(t *testing.T) {
	t.Setenv("AJEAN_HOME", t.TempDir())
	t.Setenv("AJEAN_NO_HOOKS", "")
	out := listOutput(t)
	if !strings.Contains(out, "Aucun hook") || !strings.Contains(out, "hooks.json") {
		t.Fatalf("sans fichier, la sortie doit dire qu'il n'y a aucun hook et où le créer :\n%s", out)
	}
}

func TestHooksListShowsEveryHookWithItsSettings(t *testing.T) {
	writeHooksJSON(t, map[string]any{
		"PreToolUse": []any{preHook("^bash$", "deny2", map[string]any{"timeoutSec": 7, "onError": "deny"})},
		"PostToolUse": []any{
			preHook(".*", "allow", nil),
			preHook("^write$", "allow", map[string]any{"async": false}),
		},
	})
	out := listOutput(t)
	for _, want := range []string{"PreToolUse", "PostToolUse", "^bash$", "^write$", "7s", "onError=deny", "synchrone", "asynchrone"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q manque dans la liste :\n%s", want, out)
		}
	}
	if strings.Index(out, "PreToolUse") > strings.Index(out, "PostToolUse") {
		t.Errorf("PreToolUse doit être listé avant PostToolUse :\n%s", out)
	}
}

func TestHooksListFlagsUnknownEvents(t *testing.T) {
	writeHooksJSON(t, map[string]any{"PreToolUses": []any{preHook(".*", "allow", nil)}})
	out := listOutput(t)
	if !strings.Contains(out, "PreToolUses") || !strings.Contains(out, "inconnu") {
		t.Fatalf("une faute de frappe sur l'événement doit être signalée (sinon le hook ne tourne jamais en silence) :\n%s", out)
	}
}

func TestHooksListReportsKillSwitch(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook(".*", "allow", nil)))
	t.Setenv("AJEAN_NO_HOOKS", "1")
	if out := listOutput(t); !strings.Contains(out, "AJEAN_NO_HOOKS") {
		t.Fatalf("l'interrupteur global doit être signalé :\n%s", out)
	}
}

func runHooksTest(t *testing.T, tool, args string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	err := hooksTest(context.Background(), &b, tool, args)
	return b.String(), err
}

func TestHooksTestReportsBlock(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^bash$", "deny2", nil)))
	out, err := runHooksTest(t, "bash", `{"command":"rm -rf build"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BLOQUÉ") || !strings.Contains(out, "interdit") {
		t.Fatalf("le refus et sa raison doivent apparaître :\n%s", out)
	}
}

func TestHooksTestReportsAllow(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^bash$", "allow", nil)))
	out, err := runHooksTest(t, "bash", `{"command":"echo ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "autorisé") {
		t.Fatalf("l'autorisation doit apparaître :\n%s", out)
	}
}

func TestHooksTestSaysWhenNoHookMatches(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook("^(write|edit)$", "deny2", nil)))
	out, err := runHooksTest(t, "bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Aucun hook PreToolUse ne vise") {
		t.Fatalf("il faut dire qu'aucun hook ne correspond à l'outil :\n%s", out)
	}
}

func TestHooksTestRejectsBadArgsJSON(t *testing.T) {
	writeHooksJSON(t, preCfg(preHook(".*", "allow", nil)))
	if _, err := runHooksTest(t, "bash", `{pas du json`); err == nil {
		t.Fatal("des arguments JSON invalides doivent être refusés")
	}
}

func TestHooksTestPassesArgsToTheHook(t *testing.T) {
	dump := t.TempDir() + "/stdin.json"
	writeHooksJSON(t, preCfg(preHook("^bash$", "dump:"+dump, nil)))
	if _, err := runHooksTest(t, "bash", `{"command":"echo salut"}`); err != nil {
		t.Fatal(err)
	}
	b := readFileString(t, dump)
	if !strings.Contains(b, `"command":"echo salut"`) || !strings.Contains(b, `"event":"PreToolUse"`) {
		t.Fatalf("le hook doit recevoir l'événement simulé : %s", b)
	}
}

func TestCmdHooksUsage(t *testing.T) {
	if err := cmdHooks([]string{"nimporte-quoi"}); err == nil {
		t.Fatal("une sous-commande inconnue doit renvoyer l'usage")
	}
	if err := cmdHooks([]string{"test"}); err == nil {
		t.Fatal("« test » sans outil doit renvoyer l'usage")
	}
}
