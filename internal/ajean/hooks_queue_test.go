package ajean

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHookQueueKeepsOrder(t *testing.T) {
	var mu sync.Mutex
	var got []string
	q := newHookQueue(16, func(j hookJob) {
		mu.Lock()
		got = append(got, j.Tool)
		mu.Unlock()
	})
	want := []string{"a", "b", "c", "d", "e"}
	for _, n := range want {
		q.enqueue(hookJob{Tool: n})
	}
	if !q.drain(2 * time.Second) {
		t.Fatal("la file ne s'est pas vidée")
	}
	if strings.Join(got, "") != "abcde" {
		t.Fatalf("ordre perdu : %v", got)
	}
}

func TestHookQueueDropsNewestWhenFullWithoutBlocking(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	q := newHookQueue(2, func(hookJob) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	})
	q.enqueue(hookJob{Tool: "1"})
	<-started // le worker tient le 1er ; le canal est vide
	q.enqueue(hookJob{Tool: "2"})
	q.enqueue(hookJob{Tool: "3"})

	done := make(chan bool)
	go func() { done <- q.enqueue(hookJob{Tool: "4"}) }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("file pleine : l'événement aurait dû être abandonné")
		}
	case <-time.After(time.Second):
		t.Fatal("enqueue a bloqué l'appelant sur une file pleine")
	}
	if d := q.dropped.Load(); d != 1 {
		t.Fatalf("dropped = %d, attendu 1", d)
	}
	close(release)
	if !q.drain(2 * time.Second) {
		t.Fatal("la file ne s'est pas vidée")
	}
}

func TestHookQueueSlowHandlerDoesNotDelayEnqueue(t *testing.T) {
	release := make(chan struct{})
	q := newHookQueue(8, func(hookJob) { <-release })
	start := time.Now()
	for i := 0; i < 5; i++ {
		q.enqueue(hookJob{Tool: "x"})
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("enqueue dépend du handler : %v", d)
	}
	close(release)
	q.drain(2 * time.Second)
}

func TestHookQueueDrainTimesOut(t *testing.T) {
	release := make(chan struct{})
	q := newHookQueue(4, func(hookJob) { <-release })
	q.enqueue(hookJob{Tool: "x"})
	if q.drain(100 * time.Millisecond) {
		t.Fatal("drain doit rendre false quand le handler bloque")
	}
	close(release)
	q.drain(2 * time.Second)
}

func postCfg(hooks ...map[string]any) map[string]any {
	return map[string]any{"PostToolUse": hooks}
}

func TestPostHooksAreAsyncByDefault(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "post.json")
	writeHooksJSON(t, postCfg(preHook("^bash$", "dump:"+dump, nil)))
	ev := bashEvent()
	ev.Result, ev.Error = "[erreur] boom", true
	runPostHooks(context.Background(), ev)
	if !flushPostHooks(5 * time.Second) {
		t.Fatal("la file ne s'est pas vidée")
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("le hook PostToolUse n'a pas tourné : %v", err)
	}
	var got hookEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("stdin illisible : %v (%s)", err, b)
	}
	if got.Event != "PostToolUse" || got.Tool != "bash" || got.Result != "[erreur] boom" || !got.Error {
		t.Fatalf("événement incorrect : %+v", got)
	}
}

func TestPostHooksSyncOptOutRunsBeforeReturn(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "post.json")
	writeHooksJSON(t, postCfg(preHook(".*", "dump:"+dump, map[string]any{"async": false})))
	runPostHooks(context.Background(), bashEvent())
	if _, err := os.Stat(dump); err != nil {
		t.Fatalf("async=false doit s'exécuter avant le retour : %v", err)
	}
}

func TestPostHooksSkipNonMatchingTools(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "post.json")
	writeHooksJSON(t, postCfg(preHook("^(write|edit)$", "dump:"+dump, nil)))
	runPostHooks(context.Background(), bashEvent())
	flushPostHooks(5 * time.Second)
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("le matcher ne couvre pas bash")
	}
}

func TestPostHooksCannotBlockOrAlterTheCall(t *testing.T) {
	writeHooksJSON(t, postCfg(preHook(".*", "deny2", map[string]any{"async": false})))
	runPostHooks(context.Background(), bashEvent()) // ne renvoie rien : observation seule
}

func TestLoopFiresPostHookWithResult(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(hooksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(t.TempDir(), "post.json")
	cfg, _ := json.Marshal(postCfg(preHook("^bash$", "dump:"+dump, nil)))
	if err := os.WriteFile(filepath.Join(hooksDir(), "hooks.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	runBashTurn(t, filepath.Join(t.TempDir(), "ran"))
	flushPostHooks(5 * time.Second)
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("PostToolUse non déclenché par la boucle : %v", err)
	}
	if !strings.Contains(string(b), `"tool":"bash"`) || !strings.Contains(string(b), `"result"`) {
		t.Fatalf("événement incomplet : %s", b)
	}
}
