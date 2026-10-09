package ajean

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const postQueueSize = 256

// hookJob est un instantané sérialisé : la boucle d'outils peut modifier ses
// arguments après coup, le hook doit voir ceux de l'appel.
type hookJob struct {
	Event   string
	Tool    string
	Payload []byte
}

// hookQueue : un seul worker (ordre préservé), canal borné, et jamais de blocage
// de l'appelant — plein = l'événement le plus récent est abandonné et compté.
type hookQueue struct {
	ch      chan hookJob
	handle  func(hookJob)
	pending atomic.Int64
	dropped atomic.Int64
}

func newHookQueue(size int, handle func(hookJob)) *hookQueue {
	q := &hookQueue{ch: make(chan hookJob, size), handle: handle}
	go func() {
		for j := range q.ch {
			q.handle(j)
			q.pending.Add(-1)
		}
	}()
	return q
}

func (q *hookQueue) enqueue(j hookJob) bool {
	q.pending.Add(1)
	select {
	case q.ch <- j:
		return true
	default:
		q.pending.Add(-1)
		q.dropped.Add(1)
		fmt.Fprintf(os.Stderr, "[hooks] file pleine, événement %s abandonné\n", j.Tool)
		return false
	}
}

// drain attend que tout ce qui a été mis en file soit traité ; false si le délai est dépassé.
func (q *hookQueue) drain(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for q.pending.Load() > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

var postQueue struct {
	once sync.Once
	q    *hookQueue
}

func postHookQueue() *hookQueue {
	postQueue.once.Do(func() { postQueue.q = newHookQueue(postQueueSize, handlePostJob) })
	return postQueue.q
}

func flushPostHooks(timeout time.Duration) bool { return postHookQueue().drain(timeout) }

func hookIsAsync(h hookSpec) bool { return h.Async == nil || *h.Async }

// runPostHooks notifie les hooks PostToolUse. Observation seule : rien n'est
// renvoyé et un hook ne peut ni refuser ni modifier l'appel.
func runPostHooks(ctx context.Context, ev hookEvent) {
	ev.Event = "PostToolUse"
	fireObservers(ctx, ev)
}

// runTurnEndHooks notifie les hooks TurnEnd (fin d'un tour de l'agent). Le
// matcher porte sur un nom d'outil, qui n'existe pas ici : écrire ".*".
func runTurnEndHooks(ctx context.Context, ev hookEvent) {
	ev.Event = "TurnEnd"
	fireObservers(ctx, ev)
}

// fireObservers : les hooks asynchrones passent par la file ; ceux marqués
// "async": false tournent ici.
func fireObservers(ctx context.Context, ev hookEvent) { fireObserversOut(ctx, ev, nil) }

// fireObserversOut est fireObservers, avec en plus la sortie standard des hooks
// synchrones remise à onOut (nil = ignorée). Les hooks asynchrones n'en ont pas :
// ils tournent après coup, sans personne pour la lire.
func fireObserversOut(ctx context.Context, ev hookEvent, onOut func(stdout string)) {
	specs := matchingHooks(ev.Event, ev.Tool)
	if len(specs) == 0 {
		return
	}
	payload, ok := hookPayload(ctx, ev)
	if !ok {
		return
	}
	queued := false
	for _, h := range specs {
		if hookIsAsync(h) {
			queued = true
			continue
		}
		if onOut == nil {
			logHookError(ev.Event, h, runHookIgnoringDecision(ctx, h, payload))
			continue
		}
		out, _, err := execHook(ctx, h, payload)
		logHookError(ev.Event, h, err)
		if err == nil {
			onOut(out)
		}
	}
	if queued {
		postHookQueue().enqueue(hookJob{Event: ev.Event, Tool: ev.Tool, Payload: payload})
	}
}

func handlePostJob(j hookJob) {
	for _, h := range matchingHooks(j.Event, j.Tool) {
		if hookIsAsync(h) {
			logHookError(j.Event, h, runHookIgnoringDecision(context.Background(), h, j.Payload))
		}
	}
}

// runHookIgnoringDecision : un code 2 ou un JSON « deny » n'a aucun sens après coup,
// seuls les vrais échecs (plantage, délai) sont remontés.
func runHookIgnoringDecision(ctx context.Context, h hookSpec, payload []byte) error {
	_, err := runHook(ctx, h, payload)
	return err
}

func logHookError(event string, h hookSpec, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[hooks] %s %q : %v\n", event, strings.TrimSpace(h.Command), err)
	}
}
