package ajean

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func turnEndCfg(hooks ...map[string]any) map[string]any {
	return map[string]any{"TurnEnd": hooks}
}

func TestTurnRecorderBuildsEventFromStreamEvents(t *testing.T) {
	rec := newTurnRecorder()
	sink := func(StreamEvent) bool { return true }
	cb := rec.wrap(sink)
	cb(StreamEvent{ToolUsed: &ToolUsedEvent{Name: "bash", Done: false}}) // annoncé, pas terminé
	cb(StreamEvent{ToolUsed: &ToolUsedEvent{Name: "bash", Done: true}})
	cb(StreamEvent{ToolUsed: &ToolUsedEvent{Name: "write", Done: true}})
	cb(StreamEvent{Stats: &StatsEvent{PromptTokensTotal: 40000, GenTokens: 300}})
	cb(StreamEvent{Stats: &StatsEvent{PromptTokensTotal: 41000, GenTokens: 500}})
	cb(StreamEvent{Stats: &StatsEvent{GenTokens: 7}}) // sans usage : ne remplace pas la taille connue

	ev := rec.event(context.Background(), Caps{})
	if ev.Event != "TurnEnd" || ev.Status != "ok" {
		t.Fatalf("événement ou statut incorrect : %+v", ev)
	}
	if ev.ToolCalls != 2 {
		t.Errorf("toolCalls = %d, attendu 2 (seuls les appels terminés comptent)", ev.ToolCalls)
	}
	if ev.ContextTokens != 41500 {
		t.Errorf("contextTokens = %d, attendu 41500 (dernier prompt total + génération)", ev.ContextTokens)
	}
}

func TestTurnRecorderStatuses(t *testing.T) {
	rec := newTurnRecorder()
	rec.wrap(func(StreamEvent) bool { return true })(StreamEvent{Err: errors.New("moteur injoignable")})
	ev := rec.event(context.Background(), Caps{})
	if ev.Status != "error" || !strings.Contains(ev.Message, "moteur injoignable") {
		t.Fatalf("erreur attendue avec son message : %+v", ev)
	}

	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if got := newTurnRecorder().event(stopped, Caps{}).Status; got != "stopped" {
		t.Fatalf("contexte annulé = arrêt demandé, reçu %q", got)
	}
}

func TestTurnRecorderPassesEventsThroughAndKeepsAbort(t *testing.T) {
	var got []string
	cb := newTurnRecorder().wrap(func(ev StreamEvent) bool {
		got = append(got, ev.Content)
		return ev.Content != "stop"
	})
	if !cb(StreamEvent{Content: "a"}) || cb(StreamEvent{Content: "stop"}) {
		t.Fatal("la valeur de retour du callback d'origine (abandon du flux) doit être conservée")
	}
	if len(got) != 2 {
		t.Fatalf("les événements doivent être transmis tels quels : %v", got)
	}
}

func TestLoopFiresTurnEndHookOnce(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(t.TempDir(), "turn.json")
	cfg, _ := json.Marshal(turnEndCfg(preHook(".*", "dump:"+dump, nil)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	runBashTurn(t, filepath.Join(t.TempDir(), "ran"))
	if !flushPostHooks(10 * time.Second) {
		t.Fatal("la file ne s'est pas vidée")
	}
	var got hookEvent
	if err := json.Unmarshal([]byte(readFileString(t, dump)), &got); err != nil {
		t.Fatalf("stdin du hook illisible : %v", err)
	}
	if got.Event != "TurnEnd" || got.Status != "ok" || got.ToolCalls != 1 || got.DurationMs <= 0 {
		t.Fatalf("événement TurnEnd incorrect : %+v", got)
	}
}

func TestLoopSkipsTurnEndForJeanReflection(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(t.TempDir(), "turn.json")
	cfg, _ := json.Marshal(turnEndCfg(preHook(".*", "dump:"+dump, nil)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	port, _ := toolCallServer(t, "echo x")
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), jeanReflectKey{}, true)
	if _, err := runChat(ctx, []Message{{Role: "user", Content: "go"}}, 0.7, Caps{}, func(StreamEvent) bool { return true }, nil); err != nil {
		t.Fatalf("runChat : %v", err)
	}
	flushPostHooks(5 * time.Second)
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("la révision de mémoire de Jean n'est pas un tour utilisateur : pas de TurnEnd")
	}
}

func TestHooksListKnowsTurnEnd(t *testing.T) {
	writeHooksJSON(t, turnEndCfg(preHook(".*", "allow", nil)))
	out := listOutput(t)
	if strings.Contains(out, "inconnu") || !strings.Contains(out, "TurnEnd") || !strings.Contains(out, "asynchrone") {
		t.Fatalf("TurnEnd doit être un événement connu, asynchrone par défaut :\n%s", out)
	}
}
