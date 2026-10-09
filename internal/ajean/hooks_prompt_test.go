package ajean

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func promptCfg(hooks ...map[string]any) map[string]any {
	return map[string]any{"UserPromptSubmit": hooks}
}

func installPromptHookSwitch(t *testing.T, on bool) (dump string) {
	t.Helper()
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dump = filepath.Join(t.TempDir(), "prompt.json")
	cfg, _ := json.Marshal(promptCfg(preHook(".*", "dump:"+dump, nil)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setPromptHooksEnabled(on); err != nil {
		t.Fatal(err)
	}
	port, _ := toolCallServer(t, "echo x")
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	return dump
}

func installPromptHook(t *testing.T) string { return installPromptHookSwitch(t, true) }

func runPromptTurn(t *testing.T, ctx context.Context, msgs []Message) {
	t.Helper()
	if _, err := runChat(ctx, msgs, 0.7, Caps{}, func(StreamEvent) bool { return true }, nil); err != nil {
		t.Fatalf("runChat : %v", err)
	}
	if !flushPostHooks(10 * time.Second) {
		t.Fatal("la file ne s'est pas vidée")
	}
}

func TestLastUserPromptTakesLatestUserText(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "tu es un agent"},
		{Role: "user", Content: "premier"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "  corrige ce bug  "},
	}
	if got := lastUserPrompt(msgs); got != "corrige ce bug" {
		t.Fatalf("dernière demande = %q", got)
	}
	if got := lastUserPrompt([]Message{{Role: "system", Content: "x"}}); got != "" {
		t.Fatalf("sans message utilisateur : %q", got)
	}
}

func TestLastUserPromptReplacesImageMarker(t *testing.T) {
	msgs := []Message{{Role: "user", Content: []map[string]any{
		{"type": "text", "text": "que montre cette capture ?"},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
	}}}
	got := lastUserPrompt(msgs)
	if !strings.Contains(got, "que montre cette capture ?") || !strings.HasSuffix(got, "[image]") {
		t.Fatalf("le texte et un marqueur [image] court étaient attendus : %q", got)
	}
	if strings.Contains(got, "not kept past this point") {
		t.Fatalf("le marqueur de compaction ne doit pas fuiter vers le hook : %q", got)
	}
}

func TestLoopFiresUserPromptSubmitOnce(t *testing.T) {
	dump := installPromptHook(t)
	runPromptTurn(t, context.Background(), []Message{{Role: "user", Content: "planifie la migration de la base"}})
	var got hookEvent
	if err := json.Unmarshal([]byte(readFileString(t, dump)), &got); err != nil {
		t.Fatalf("stdin du hook illisible : %v", err)
	}
	if got.Event != "UserPromptSubmit" || got.Prompt != "planifie la migration de la base" {
		t.Fatalf("événement incorrect : %+v", got)
	}
}

func TestUserPromptSubmitClipsLongPrompt(t *testing.T) {
	dump := installPromptHook(t)
	long := strings.Repeat("é", hookPromptMaxRunes+500)
	runPromptTurn(t, context.Background(), []Message{{Role: "user", Content: long}})
	var got hookEvent
	if err := json.Unmarshal([]byte(readFileString(t, dump)), &got); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got.Prompt)); n != hookPromptMaxRunes+1 {
		t.Fatalf("la demande doit être tronquée à %d caractères plus « … », reçu %d", hookPromptMaxRunes, n)
	}
}

func TestUserPromptSubmitSkipsJeanReflection(t *testing.T) {
	dump := installPromptHook(t)
	ctx := context.WithValue(context.Background(), jeanReflectKey{}, true)
	runPromptTurn(t, ctx, []Message{{Role: "user", Content: "révise la mémoire"}})
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("la révision de mémoire de Jean n'est pas une demande utilisateur : pas de UserPromptSubmit")
	}
}

func TestUserPromptSubmitSkipsWhenNoUserMessage(t *testing.T) {
	dump := installPromptHook(t)
	runPromptTurn(t, context.Background(), []Message{{Role: "system", Content: "contexte seul"}})
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("sans message utilisateur, aucun événement ne doit partir")
	}
}

func TestHooksListKnowsUserPromptSubmit(t *testing.T) {
	writeHooksJSON(t, promptCfg(preHook(".*", "allow", nil)))
	out := listOutput(t)
	if strings.Contains(out, "inconnu") || !strings.Contains(out, "UserPromptSubmit") || !strings.Contains(out, "asynchrone") {
		t.Fatalf("UserPromptSubmit doit être un événement connu, asynchrone par défaut :\n%s", out)
	}
}

func TestUserPromptSubmitOffByDefault(t *testing.T) {
	dump := installPromptHookSwitch(t, false)
	runPromptTurn(t, context.Background(), []Message{{Role: "user", Content: "corrige ce bug"}})
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("interrupteur coupé : le hook configuré ne doit pas être lancé")
	}
}

func promptHookRequest(t *testing.T, method, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	handlePromptHook(w, httptest.NewRequest(method, "/api/prompthook", strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestHandlePromptHookToggle(t *testing.T) {
	testHome(t)
	if code, out := promptHookRequest(t, "GET", ""); code != 200 || out["on"] != false {
		t.Fatalf("GET par défaut : %d %v (attendu on=false)", code, out)
	}
	if code, out := promptHookRequest(t, "POST", `{"on":true}`); code != 200 || out["on"] != true {
		t.Fatalf("POST on : %d %v", code, out)
	}
	if !promptHooksEnabled() {
		t.Fatal("l'interrupteur doit être mémorisé")
	}
	if code, out := promptHookRequest(t, "POST", `{"on":false}`); code != 200 || out["on"] != false {
		t.Fatalf("POST off : %d %v", code, out)
	}
	if code, _ := promptHookRequest(t, "POST", `pas du json`); code != 400 {
		t.Fatalf("corps illisible : %d, attendu 400", code)
	}
	if code, _ := promptHookRequest(t, "DELETE", ""); code != 405 {
		t.Fatalf("méthode refusée : %d, attendu 405", code)
	}
}
