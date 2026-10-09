package ajean

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// chatOnceReq : un appel ponctuel au modèle déjà chargé, sans outils ni flux.
type chatOnceReq struct {
	System      string
	User        string
	MaxTokens   int
	Temperature float64
	ErrPrefix   string // préfixe des erreurs côté moteur (« résumé », « amélioration »…)
}

// summarizeResp modélise le sous-ensemble utile d'une réponse non-streamée de
// /v1/chat/completions.
type summarizeResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// chatOnce envoie system + user au moteur actif (llama-server local ou API
// externe) et renvoie la réponse nettoyée : bloc <think> retiré, blancs rognés.
func chatOnce(ctx context.Context, r chatOnceReq) (string, error) {
	ep := resolveChatEndpoint()
	payload := map[string]any{
		"model": ep.Model,
		"messages": []Message{
			{Role: "system", Content: r.System},
			{Role: "user", Content: r.User},
		},
		"stream":      false,
		"temperature": r.Temperature,
		// Borne dure : un modèle bavard (surtout à reasoning) produirait une réponse
		// énorme et lente.
		"max_tokens": r.MaxTokens,
	}
	// Pas de réflexion : plus rapide, plus dense, et évite qu'un modèle hybride
	// gaspille tout le budget en <think> (réponse vide). llama.cpp passe ces kwargs
	// au gabarit Jinja (--jinja). Champ propre à llama.cpp : on ne l'envoie PAS à
	// une API externe (OpenAI le rejetterait en 400).
	if !ep.External {
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", ep.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if ep.External {
		if ep.Key != "" {
			req.Header.Set("Authorization", "Bearer "+ep.Key)
		}
	} else {
		authHeader(req)
	}
	resp, err := doLLM(ctx, req, body, ep)
	if err != nil {
		return "", friendlyLLMError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", fmt.Errorf("%s: llama-server %d: %s", r.ErrPrefix, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out summarizeResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%s: réponse vide", r.ErrPrefix)
	}
	c := out.Choices[0].Message.Content
	// Certains modèles à raisonnement préfixent un bloc <think>…</think> : on ne
	// garde que la réponse finale.
	if i := strings.LastIndex(c, thinkClose); i >= 0 {
		c = c[i+len(thinkClose):]
	}
	return strings.TrimSpace(c), nil
}
