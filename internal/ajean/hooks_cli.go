package ajean

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var knownHookEvents = []string{"PreToolUse", "PostToolUse"}

func cmdHooks(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list":
		return hooksList(os.Stdout)
	case "test":
		if len(args) < 2 {
			break
		}
		argsJSON := ""
		if len(args) > 2 {
			argsJSON = args[2]
		}
		return hooksTest(context.Background(), os.Stdout, args[1], argsJSON)
	}
	return fmt.Errorf("usage: ajean hooks [list | test <outil> ['<arguments en JSON>']]")
}

// hooksList affiche ce que le fichier hooks.json déclare, y compris ce que le
// moteur ignorerait en silence : un événement mal orthographié ne ferait jamais
// tourner son hook, sans aucun message.
func hooksList(w io.Writer) error {
	path := filepath.Join(hooksDir(), "hooks.json")
	fmt.Fprintf(w, "%s %s\n", cyan("Hooks AJEAN"), dim(path))
	if os.Getenv("AJEAN_NO_HOOKS") != "" {
		fmt.Fprintln(w, yellow("AJEAN_NO_HOOKS est défini : tous les hooks sont désactivés."))
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(w, "Aucun hook configuré : %s n'existe pas (format dans docs/hooks-spec.md).\n", path)
		return nil
	}
	cfg := parseHooks(path)
	if len(cfg) == 0 {
		fmt.Fprintln(w, "Aucun hook actif : le fichier est vide ou illisible.")
		return nil
	}
	for _, ev := range orderedHookEvents(cfg) {
		known := ev == "PreToolUse" || ev == "PostToolUse"
		title := bold(ev)
		if !known {
			title += " " + red("(événement inconnu : ignoré)")
		}
		fmt.Fprintf(w, "\n%s\n", title)
		for i, h := range cfg[ev] {
			fmt.Fprintf(w, "  %d. %s  →  %s\n     %s\n", i+1, h.Matcher, h.Command, dim(hookSettings(ev, h)))
		}
	}
	return nil
}

func orderedHookEvents(cfg hookConfig) []string {
	var out, others []string
	for _, k := range knownHookEvents {
		if _, ok := cfg[k]; ok {
			out = append(out, k)
		}
	}
	for k := range cfg {
		if k != "PreToolUse" && k != "PostToolUse" {
			others = append(others, k)
		}
	}
	sort.Strings(others)
	return append(out, others...)
}

func hookSettings(event string, h hookSpec) string {
	parts := []string{fmt.Sprintf("%ds", hookTimeoutSec(h))}
	switch event {
	case "PreToolUse":
		onErr := "allow"
		if h.OnError == "deny" {
			onErr = "deny"
		}
		parts = append(parts, "onError="+onErr)
	case "PostToolUse":
		if hookIsAsync(h) {
			parts = append(parts, "asynchrone")
		} else {
			parts = append(parts, "synchrone")
		}
	}
	return strings.Join(parts, ", ")
}

// hooksTest rejoue les hooks PreToolUse sur un appel simulé, sans exécuter l'outil :
// c'est le moyen de vérifier un garde-fou avant de lui confier l'agent.
func hooksTest(ctx context.Context, w io.Writer, tool, argsJSON string) error {
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Errorf("arguments JSON invalides : %v", err)
		}
	}
	specs := matchingHooks("PreToolUse", tool)
	if len(specs) == 0 {
		fmt.Fprintf(w, "Aucun hook PreToolUse ne vise « %s ».", tool)
		if os.Getenv("AJEAN_NO_HOOKS") != "" {
			fmt.Fprint(w, " "+yellow("(AJEAN_NO_HOOKS est défini)"))
		}
		fmt.Fprintln(w)
		return nil
	}
	fmt.Fprintf(w, "%d hook(s) PreToolUse visent « %s » :\n", len(specs), tool)
	for _, h := range specs {
		fmt.Fprintf(w, "  - %s\n", h.Command)
	}
	if denied := runPreHooks(ctx, hookEvent{Tool: tool, Args: args}); denied != "" {
		fmt.Fprintf(w, "\nRésultat : %s\n  %s\n", red("BLOQUÉ"), denied)
	} else {
		fmt.Fprintf(w, "\nRésultat : %s\n", green("autorisé"))
	}
	if n := len(matchingHooks("PostToolUse", tool)); n > 0 {
		fmt.Fprintf(w, "%s\n", dim(fmt.Sprintf("%d hook(s) PostToolUse se déclencheraient après l'appel (non exécutés par ce test).", n)))
	}
	return nil
}
