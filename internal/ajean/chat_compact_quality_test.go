package ajean

import (
	"strings"
	"testing"
)

// Un résumé de compaction précédent doit arriver au résumeur COMME TEL (mémoire
// des tours plus anciens à reporter), pas comme une parole de l'utilisateur :
// sinon les vieux faits se diluent un peu plus à chaque compactage.
func TestRenderTranscriptLabelsPreviousSummary(t *testing.T) {
	prev := compactSummaryUserMsg("- Port du moteur : 8080\n- Fichier : G:\\ajean-test\\ajean.db", nil)
	out := renderTranscript([]Message{
		{Role: "user", Content: prev},
		{Role: "user", Content: "et maintenant ?"},
	})
	if !strings.Contains(out, "Previous summary") {
		t.Fatalf("le résumé précédent doit être étiqueté :\n%s", out)
	}
	if strings.Contains(out, "User: "+compactSummaryPrefix) {
		t.Fatalf("le résumé précédent ne doit pas passer pour une parole de l'utilisateur :\n%s", out)
	}
	if !strings.Contains(out, "Port du moteur : 8080") {
		t.Fatalf("le contenu du résumé précédent doit être conservé :\n%s", out)
	}
	if !strings.Contains(out, "User: et maintenant ?") {
		t.Fatalf("une vraie demande doit rester une demande :\n%s", out)
	}
}

func TestRenderTranscriptKeepsOrdinaryUserMessages(t *testing.T) {
	out := renderTranscript([]Message{{Role: "user", Content: "bonjour"}})
	if out != "User: bonjour\n" {
		t.Fatalf("rendu d'un message ordinaire modifié : %q", out)
	}
}

// Le garde-fou de longueur coupe la FIN du résumé : l'objectif courant et la
// prochaine étape doivent donc venir avant le reste.
func TestCompactPromptPutsCurrentGoalFirst(t *testing.T) {
	p := compactSystemPrompt
	order := []string{"CURRENT GOAL", "USER CONSTRAINTS", "FINDINGS", "WORK DONE", "DECISIONS"}
	last := -1
	for _, h := range order {
		i := strings.Index(p, h)
		if i < 0 {
			t.Fatalf("section %q absente du prompt du résumeur", h)
		}
		if i < last {
			t.Fatalf("section %q hors ordre : l'essentiel doit venir en premier", h)
		}
		last = i
	}
}

func TestCompactPromptCoversBestPractices(t *testing.T) {
	p := strings.ToLower(compactSystemPrompt)
	for what, needle := range map[string]string{
		"résumé précédent reporté":       "previous summary",
		"identifiants copiés tels quels": "verbatim",
		"impasses et échecs":             "dead ends",
		"corrections de l'utilisateur":   "corrected",
		"contenu d'outil non fiable":     "untrusted",
		"même langue":                    "same language",
	} {
		if !strings.Contains(p, needle) {
			t.Errorf("le prompt du résumeur ne couvre pas : %s (%q)", what, needle)
		}
	}
}

// Après une reprise, l'état modifiable (fichiers, processus) a pu changer : le
// message de compaction doit inviter à le revérifier plutôt qu'à s'y fier.
func TestCompactSummaryMsgAsksToReverifyState(t *testing.T) {
	msg := strings.ToLower(compactSummaryUserMsg("- un fait utile pour la suite de la tâche", nil))
	if !strings.Contains(msg, "re-check") {
		t.Fatalf("le message de compaction doit demander de revérifier l'état modifiable :\n%s", msg)
	}
}
