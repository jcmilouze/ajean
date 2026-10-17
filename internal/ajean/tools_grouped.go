package ajean

// tools_grouped.go — UN outil par domaine pour le modèle :
// memory, task, recall, web, browser, see, jean, file. Chacun prend un paramètre
// `action` qui choisit l'opération.
//
// En interne rien ne change : EnabledTools construit toujours la liste fine
// (mem_read, web_search, jean_rule…), groupTools la regroupe au moment de
// l'envoyer au modèle, et expandToolCall retraduit chaque appel reçu vers
// l'outil interne AVANT tout le reste (libellés, exécution, événements de l'UI,
// garde-fous du mode Jean). L'historique garde les appels tels que le modèle
// les a écrits.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type toolGroup struct {
	name, intro string
	// action → outil interne, dans l'ordre d'affichage.
	members [][2]string
	// renommage de paramètres pour un membre (conflit avec `action`) :
	// outil interne → {nom interne: nom dans le groupe}.
	rename map[string]map[string]string
}

var toolGroups = []toolGroup{
	{name: "file", intro: "Write files on disk (read them with bash).",
		members: [][2]string{{"write", "write"}, {"edit", "edit"}}},
	{name: "memory", intro: "Persistent memory pages of the current project.",
		members: [][2]string{{"search", "mem_search"}, {"read", "mem_read"}, {"add", "mem_add"}, {"edit", "mem_edit"}, {"delete", "mem_delete"}}},
	{name: "task", intro: "Your scheduled tasks: they run later on their own.",
		members: [][2]string{{"list", "task_list"}, {"create", "task_create"}, {"update", "task_update"}, {"delete", "task_delete"}}},
	{name: "recall", intro: "Blocks archived when this conversation was compacted.",
		members: [][2]string{{"read", "recall"}, {"search", "recall_search"}}},
	{name: "web", intro: "Internet access.",
		members: [][2]string{{"search", "web_search"}, {"images", "web_images"}, {"open", "web_open"}, {"read", "web_read"}, {"grep", "web_grep"}}},
	{name: "browser", intro: "Drive a real web browser on this machine.",
		members: [][2]string{{"open", "browser_open"}, {"snapshot", "browser_snapshot"}, {"find", "browser_find"}, {"click", "browser_click"}, {"click_xy", "browser_click_xy"},
			{"type", "browser_type"}, {"key", "browser_key"}, {"scroll", "browser_scroll"}, {"screenshot", "browser_screenshot"}}},
	{name: "see", intro: "Look at an image or a video file from disk.",
		members: [][2]string{{"image", "see_image"}, {"video", "see_video"}}},
	{name: "jean", intro: "Your own memory as Jean: profile, fiches, journal, lessons, rules, projects.",
		members: [][2]string{{"search", "jean_search"}, {"remember", "jean_remember"}, {"forget", "jean_forget"}, {"note", "jean_note"}, {"save", "jean_save"},
			{"read", "jean_read"}, {"lesson", "jean_lesson"}, {"patch", "jean_patch"}, {"rule", "jean_rule"}, {"projects", "jean_projects"},
			{"project_mem", "jean_project_mem"}, {"project_file", "jean_project_file"}},
		rename: map[string]map[string]string{"jean_rule": {"action": "rule_action"}}},
}

// groupOf : groupe et action d'un outil interne.
var groupOf = func() map[string][2]string {
	m := map[string][2]string{}
	for _, g := range toolGroups {
		for _, mb := range g.members {
			m[mb[1]] = [2]string{g.name, mb[0]}
		}
	}
	return m
}()

// groupTools remplace les outils internes par leur outil de domaine. Les outils
// sans groupe (bash, app, tracker, MCP…) restent tels quels, à leur place.
func groupTools(tools []Tool) []Tool {
	byName := map[string]Tool{}
	for _, t := range tools {
		byName[t.Function.Name] = t
	}
	out := make([]Tool, 0, len(tools))
	done := map[string]bool{}
	for _, t := range tools {
		gi, ok := groupOf[t.Function.Name]
		if !ok {
			out = append(out, t)
			continue
		}
		if done[gi[0]] {
			continue
		}
		done[gi[0]] = true
		for _, g := range toolGroups {
			if g.name == gi[0] {
				out = append(out, buildGroupTool(g, byName))
			}
		}
	}
	return out
}

func buildGroupTool(g toolGroup, byName map[string]Tool) Tool {
	var desc strings.Builder
	desc.WriteString(g.intro)
	actions := []string{}
	props := map[string]any{}
	for _, mb := range g.members {
		t, ok := byName[mb[1]]
		if !ok {
			continue
		}
		actions = append(actions, mb[0])
		fmt.Fprintf(&desc, "\n- %s: %s", mb[0], strings.TrimSpace(t.Function.Description))
		p, _ := t.Function.Parameters.(map[string]any)
		pp, _ := p["properties"].(map[string]any)
		keys := make([]string, 0, len(pp))
		for k := range pp {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ren := g.rename[mb[1]]
		for _, k := range keys {
			name := k
			if r, ok := ren[k]; ok {
				name = r
			}
			if _, dup := props[name]; !dup {
				props[name] = pp[k]
			}
		}
	}
	props["action"] = map[string]any{"type": "string", "enum": actions}
	return Tool{Type: "function", Function: ToolFunction{
		Name:        g.name,
		Description: desc.String(),
		Parameters:  map[string]any{"type": "object", "properties": props, "required": []string{"action"}},
	}}
}

// expandToolCall traduit un appel d'outil de domaine vers l'outil interne et
// ses arguments. Un outil hors groupe passe tel quel. errMsg non vide : action
// manquante ou inconnue (l'appel n'est pas exécuté).
func expandToolCall(name string, args map[string]any) (string, map[string]any, string) {
	var g *toolGroup
	for i := range toolGroups {
		if toolGroups[i].name == name {
			g = &toolGroups[i]
		}
	}
	if g == nil {
		return name, args, ""
	}
	act := strings.ToLower(strings.TrimSpace(str(args["action"])))
	// see : l'action se déduit de l'extension quand elle manque.
	// recall(id) sans action (comme le citent les résumés de compactage) = lire.
	if act == "" && g.name == "recall" {
		act = "read"
	}
	if act == "" && g.name == "see" {
		act = "image"
		if videoExt(str(args["file"])) {
			act = "video"
		}
	}
	legacy := ""
	names := []string{}
	for _, mb := range g.members {
		names = append(names, mb[0])
		if mb[0] == act {
			legacy = mb[1]
		}
	}
	if legacy == "" {
		return name, args, fmt.Sprintf("[erreur] %s : action %q inconnue. Actions : %s.", name, act, strings.Join(names, ", "))
	}
	out := map[string]any{}
	for k, v := range args {
		if k != "action" {
			out[k] = v
		}
	}
	for in, ext := range g.rename[legacy] {
		if v, ok := out[ext]; ok {
			delete(out, ext)
			out[in] = v
		}
	}
	return legacy, out, ""
}

// legacyToolName : nom interne d'après le nom de domaine et l'action (même
// partielle, pendant la frappe). "" si on ne sait pas encore.
func legacyToolName(name, action string) string {
	if _, ok := groupOf[name]; ok {
		return name
	}
	for _, g := range toolGroups {
		if g.name != name {
			continue
		}
		for _, mb := range g.members {
			if mb[0] == strings.ToLower(strings.TrimSpace(action)) {
				return mb[1]
			}
		}
		return ""
	}
	return name
}

func videoExt(f string) bool {
	switch strings.ToLower(filepath.Ext(f)) {
	case ".mp4", ".mov", ".webm", ".mkv", ".avi", ".m4v":
		return true
	}
	return false
}

// toolCallLegacy : un appel d'outil de l'historique, ramené à l'outil interne
// (pour les relectures de l'historique qui cherchent par exemple les mem_read).
func toolCallLegacy(tc ToolCall) (string, map[string]any) {
	var args map[string]any
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
	n, a, _ := expandToolCall(tc.Function.Name, args)
	return n, a
}
