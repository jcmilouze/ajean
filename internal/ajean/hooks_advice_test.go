package ajean

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAdvice(t *testing.T) {
	a := parseAdvice(` {"advice":{"preset":" p1 ","label":"Preset 1","reason":"debug"}} `)
	if a == nil || a.Preset != "p1" || a.Label != "Preset 1" || a.Reason != "debug" {
		t.Fatalf("conseil valide mal lu : %+v", a)
	}
	for name, out := range map[string]string{
		"vide":           "",
		"pas du json":    "toast affiché",
		"sans conseil":   `{"decision":"allow"}`,
		"sans preset":    `{"advice":{"label":"x"}}`,
		"preset blanc":   `{"advice":{"preset":"   "}}`,
		"conseil nul":    `{"advice":null}`,
		"mauvais type":   `{"advice":"p1"}`,
		"tableau racine": `[{"advice":{"preset":"p1"}}]`,
	} {
		if got := parseAdvice(out); got != nil {
			t.Errorf("%s : aucun conseil attendu, reçu %+v", name, got)
		}
	}
	long := strings.Repeat("é", 1000)
	b := parseAdvice(`{"advice":{"preset":"p","label":"` + long + `","reason":"` + long + `"}}`)
	if b == nil || len([]rune(b.Label)) != 81 || len([]rune(b.Reason)) != 301 {
		t.Fatalf("label et raison doivent être bornés : %+v", b)
	}
}

func adviceEvents(t *testing.T, hookExtra map[string]any) []*AdviceEvent {
	t.Helper()
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(promptCfg(preHook(".*", "advice", hookExtra)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setPromptHooksEnabled(true); err != nil {
		t.Fatal(err)
	}
	port, _ := toolCallServer(t, "echo x")
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	var got []*AdviceEvent
	_, err := runChat(context.Background(), []Message{{Role: "user", Content: "corrige ce bug"}}, 0.7, Caps{},
		func(ev StreamEvent) bool {
			if ev.Advice != nil {
				got = append(got, ev.Advice)
			}
			return true
		}, nil)
	if err != nil {
		t.Fatalf("runChat : %v", err)
	}
	flushPostHooks(5_000_000_000)
	return got
}

func TestSyncUserPromptHookDeliversAdvice(t *testing.T) {
	got := adviceEvents(t, map[string]any{"async": false})
	if len(got) != 1 || got[0].Preset != "preset-b" || got[0].Label != "Preset B" {
		t.Fatalf("un conseil était attendu, reçu %+v", got)
	}
}

func TestAsyncUserPromptHookDeliversNoAdvice(t *testing.T) {
	if got := adviceEvents(t, nil); len(got) != 0 {
		t.Fatalf("un hook asynchrone n'a personne pour lire sa sortie : %+v", got)
	}
}
