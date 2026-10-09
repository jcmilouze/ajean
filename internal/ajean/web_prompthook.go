package ajean

import (
	"encoding/json"
	"net/http"
)

// handlePromptHook lit (GET) ou règle (POST {"on":bool}) l'interrupteur des hooks
// UserPromptSubmit (hooks_prompt.go). Réglage de l'installation, partagé par
// tous les appareils ; il ne s'édite que par ce endpoint, jamais par hooks.json.
func handlePromptHook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req struct {
			On bool `json:"on"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "requête illisible"})
			return
		}
		if err := setPromptHooksEnabled(req.On); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	default:
		sendJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "GET ou POST requis"})
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"ok": true, "on": promptHooksEnabled()})
}
