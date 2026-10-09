package ajean

import (
	"context"
	"time"
)

// turnRecorder observe les événements d'UN tour de runChat pour en tirer
// l'événement TurnEnd, sans rien changer à la boucle d'outils : il enveloppe le
// callback, qui voit déjà passer les appels d'outils, les stats et les erreurs.
type turnRecorder struct {
	start     time.Time
	toolCalls int
	ctxTokens int
	errText   string
}

func newTurnRecorder() *turnRecorder { return &turnRecorder{start: time.Now()} }

func (t *turnRecorder) wrap(cb ChatCallback) ChatCallback {
	return func(ev StreamEvent) bool {
		switch {
		case ev.ToolUsed != nil && ev.ToolUsed.Done:
			t.toolCalls++
		case ev.Stats != nil && ev.Stats.PromptTokensTotal > 0:
			t.ctxTokens = ev.Stats.PromptTokensTotal + ev.Stats.GenTokens
		case ev.Err != nil:
			t.errText = ev.Err.Error()
		}
		return cb(ev)
	}
}

func (t *turnRecorder) event(ctx context.Context, caps Caps) hookEvent {
	status := "ok"
	switch {
	case t.errText != "":
		status = "error"
	case ctx.Err() != nil:
		status = "stopped"
	}
	return hookEvent{
		Event:         "TurnEnd",
		Jean:          caps.Jean,
		DurationMs:    time.Since(t.start).Milliseconds(),
		ToolCalls:     t.toolCalls,
		ContextTokens: t.ctxTokens,
		Status:        status,
		Message:       t.errText,
	}
}

// finish tire l'événement et le notifie. Le contexte du tour est déjà annulé
// quand l'utilisateur a appuyé sur stop : un hook synchrone en hériterait et
// échouerait aussitôt, d'où WithoutCancel (le statut, lui, est lu avant).
func (t *turnRecorder) finish(ctx context.Context, caps Caps) {
	runTurnEndHooks(context.WithoutCancel(ctx), t.event(ctx, caps))
}

// startTurnRecorder renvoie nil quand rien n'écoute TurnEnd (le cas général) ou
// quand le tour n'en est pas un : la révision de mémoire de Jean est du ménage
// de fond, pas une réponse à l'utilisateur.
func startTurnRecorder(ctx context.Context) *turnRecorder {
	if isJeanReflecting(ctx) || len(matchingHooks("TurnEnd", "")) == 0 {
		return nil
	}
	return newTurnRecorder()
}
