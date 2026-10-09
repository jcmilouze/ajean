package ajean

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
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

// AdviceEvent : un conseil de preset rendu par un hook UserPromptSubmit
// SYNCHRONE ("async": false) sur sa sortie standard, {"advice":{...}}. L'interface
// l'affiche en pastille ; rien ne bascule sans le clic de l'utilisateur.
type AdviceEvent struct {
	Preset string `json:"preset"` // id du preset conseillé (nom de fichier)
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// parseAdvice lit le conseil d'une sortie de hook ; nil si absente ou inutilisable.
// Les champs sont du texte affiché : bornés, jamais interprétés.
func parseAdvice(out string) *AdviceEvent {
	var v struct {
		Advice *AdviceEvent `json:"advice"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &v) != nil || v.Advice == nil {
		return nil
	}
	a := v.Advice
	a.Preset = strings.TrimSpace(clipRunes(a.Preset, 200))
	if a.Preset == "" {
		return nil
	}
	a.Label = clipRunes(strings.TrimSpace(a.Label), 80)
	a.Reason = clipRunes(strings.TrimSpace(a.Reason), 300)
	return a
}

// notifyUserPrompt notifie les hooks UserPromptSubmit au début d'un tour.
// Asynchrone par défaut : un hook ne retarde alors jamais le modèle. Un hook
// marqué "async": false tourne avant le tour (délai = sa durée) et peut rendre un
// conseil, transmis par cb. Le matcher porte sur un nom d'outil, qui n'existe pas
// ici : écrire ".*". Comme TurnEnd, la révision de mémoire de Jean n'est pas une
// demande utilisateur ; les messages mis en file pendant la génération
// (injectQueued) ne déclenchent pas l'événement.
func notifyUserPrompt(ctx context.Context, messages []Message, caps Caps, cb ChatCallback) {
	if isJeanReflecting(ctx) || len(matchingHooks("UserPromptSubmit", "")) == 0 || !promptHooksEnabled() {
		return
	}
	prompt := lastUserPrompt(messages)
	if prompt == "" {
		return
	}
	fireObserversOut(ctx, hookEvent{
		Event:       "UserPromptSubmit",
		Jean:        caps.Jean,
		Prompt:      clipRunes(prompt, hookPromptMaxRunes),
		PromptChars: utf8.RuneCountInString(prompt),
		Preset:      activePresetID(),
	}, func(out string) {
		if a := parseAdvice(out); a != nil && cb != nil {
			cb(StreamEvent{Advice: a})
		}
	})
}
