package ajean

import (
	"encoding/json"
	"net/http"
)

// handleCap lit (GET) ou règle (POST {"on":bool}) l'interrupteur des suggestions
// de suite. Réglage de l'installation, partagé par tous les appareils.
func handleCap(w http.ResponseWriter, r *http.Request) {
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
		if err := setCapEnabled(req.On); err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	default:
		sendJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "GET ou POST requis"})
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"ok": true, "on": capEnabled()})
}
