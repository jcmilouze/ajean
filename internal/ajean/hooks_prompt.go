package ajean

import (
	"context"
	"strings"
)

const hookPromptMaxRunes = 4000

// L'interrupteur du menu + coupe l'événement entier sans toucher à hooks.json.
// Coupé par défaut : poser un hook UserPromptSubmit ne change rien tant qu'on ne
// l'allume pas. Réglage de l'installation, comme les suggestions de suite.
func promptHooksEnabled() bool { return getBool(bkState, "prompt_hook") }

func setPromptHooksEnabled(on bool) error { return putBool(bkState, "prompt_hook", on) }

// lastUserPrompt renvoie le texte de la dernière demande de l'utilisateur. Une
// image n'est signalée que par « [image] » : le marqueur de compaction
// (imageLostMarker) est une phrase anglaise sans intérêt pour un hook.
func lastUserPrompt(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := msgText(messages[i])
		text = strings.ReplaceAll(text, imageLostMarker, " [image]")
		text = strings.ReplaceAll(text, videoLostMarker, " [video]")
		return strings.TrimSpace(text)
	}
	return ""
}

// notifyUserPrompt notifie les hooks UserPromptSubmit au début d'un tour.
// Observation seule, asynchrone par défaut : un hook ne retarde jamais le
// modèle. Le matcher porte sur un nom d'outil, qui n'existe pas ici : écrire ".*".
// Comme TurnEnd, la révision de mémoire de Jean n'est pas une demande
// utilisateur ; les messages mis en file pendant la génération (injectQueued)
// ne déclenchent pas l'événement.
func notifyUserPrompt(ctx context.Context, messages []Message, caps Caps) {
	if isJeanReflecting(ctx) || len(matchingHooks("UserPromptSubmit", "")) == 0 || !promptHooksEnabled() {
		return
	}
	prompt := lastUserPrompt(messages)
	if prompt == "" {
		return
	}
	fireObservers(ctx, hookEvent{
		Event:  "UserPromptSubmit",
		Jean:   caps.Jean,
		Prompt: clipRunes(prompt, hookPromptMaxRunes),
		Preset: activePresetID(),
	})
}
