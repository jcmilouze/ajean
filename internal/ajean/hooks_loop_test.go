package ajean

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// toolCallServer répond au premier appel par un tool_call bash, puis au suivant
// par du texte. Il garde le corps de chaque requête pour inspecter ce que le
// modèle a reçu comme résultat d'outil.
func toolCallServer(t *testing.T, command string) (port string, bodies func() []string) {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"command": command})
	call, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{"index": 0, "id": "c1", "type": "function",
			"function": map[string]any{"name": "bash", "arguments": string(args)}}},
	}}}})
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, string(b))
		n := len(seen)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = w.Write([]byte("data: " + string(call) + "\n\n"))
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"))
		} else {
			_, _ = w.Write([]byte(sseChunk("fini")))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u.Port(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func runBashTurn(t *testing.T, marker string) []string {
	t.Helper()
	port, bodies := toolCallServer(t, `echo x> "`+marker+`"`)
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	_, err := runChat(context.Background(), []Message{{Role: "user", Content: "go"}}, 0.7, Caps{Agent: true}, func(StreamEvent) bool { return true }, nil)
	if err != nil {
		t.Fatalf("runChat : %v", err)
	}
	return bodies()
}

func TestLoopRunsToolWithoutHook(t *testing.T) {
	testHome(t)
	marker := filepath.Join(t.TempDir(), "ran")
	runBashTurn(t, marker)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("contre-épreuve : sans hook, bash doit s'exécuter (%v)", err)
	}
}

func TestLoopPreHookBlocksTool(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(preCfg(preHook("^bash$", "deny2", nil)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	bodies := runBashTurn(t, marker)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("la commande a été exécutée malgré le refus du hook")
	}
	if len(bodies) != 2 || !strings.Contains(bodies[1], "bloqué par un hook") || !strings.Contains(bodies[1], "interdit") {
		t.Fatalf("le modèle doit recevoir le refus comme résultat d'outil, reçu %d requêtes : %v", len(bodies), bodies)
	}
}
