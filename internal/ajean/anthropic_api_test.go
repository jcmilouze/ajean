package ajean

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Requête type Claude Code : système en blocs, tool_use puis tool_result + texte
// dans le même message user, bloc thinking rejoué.
func TestAntToOpenAINormalise(t *testing.T) {
	raw := `{"model":"claude-sonnet","max_tokens":100,
	 "system":[{"type":"text","text":"A","cache_control":{"type":"ephemeral"}},{"type":"text","text":"B"}],
	 "messages":[
	  {"role":"user","content":[{"type":"text","text":"salut"},{"type":"text","text":"ça va"}]},
	  {"role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"x"},{"type":"tool_use","id":"t1","name":"Read","input":{"path":"a"}}]},
	  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"contenu"}]},{"type":"text","text":"suite"}]}
	 ],
	 "tools":[{"name":"Read","description":"lit","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"}],
	 "tool_choice":{"type":"any"}}`
	var in antRequest
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	out := antToOpenAI(in, "ajean")
	msgs := out["messages"].([]oaMsg)
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	if got := strings.Join(roles, ","); got != "system,user,assistant,tool,user" {
		t.Fatalf("rôles = %s", got)
	}
	if msgs[0].Content != "A\n\nB" || msgs[1].Content != "salut\n\nça va" {
		t.Fatalf("system/user mal aplatis : %#v %#v", msgs[0].Content, msgs[1].Content)
	}
	if msgs[2].Content != nil || len(msgs[2].ToolCalls) != 1 || msgs[2].ToolCalls[0].Function.Arguments != `{"path":"a"}` {
		t.Fatalf("assistant = %#v", msgs[2])
	}
	if msgs[3].ToolCallID != "t1" || msgs[3].Content != "contenu" {
		t.Fatalf("tool = %#v", msgs[3])
	}
	if tools := out["tools"].([]map[string]any); len(tools) != 1 {
		t.Fatalf("outil serveur non filtré : %d", len(tools))
	}
	if out["tool_choice"] != "required" || out["model"] != "ajean" {
		t.Fatalf("tool_choice/model = %v %v", out["tool_choice"], out["model"])
	}
}

// Flux complet : moteur OpenAI simulé (raisonnement, texte, appel d'outil en
// deux morceaux) → événements Anthropic.
func TestAnthropicStream(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("stream non transmis")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range []string{
			`{"choices":[{"delta":{"reasoning_content":"je réfléchis"}}]}`,
			`{"choices":[{"delta":{"content":"Bonjour"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"Read","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":1}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5}}`,
		} {
			w.Write([]byte("data: " + c + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer engine.Close()

	rr := httptest.NewRecorder()
	antStreamFrom(t, rr, engine.URL)
	s := rr.Body.String()
	for _, want := range []string{
		"event: message_start", `"type":"thinking_delta"`, `"text":"Bonjour"`,
		`{"id":"c1","input":{},"name":"Read","type":"tool_use"}`, `"partial_json":"{\"pa"`, `"partial_json":"th\":1}"`,
		`"stop_reason":"tool_use"`, `"output_tokens":5`, "event: message_stop",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("absent du flux : %s\n%s", want, s)
		}
	}
	if n := strings.Count(s, "event: content_block_start"); n != 3 {
		t.Fatalf("%d blocs ouverts, 3 attendus", n)
	}
}

func antStreamFrom(t *testing.T, w http.ResponseWriter, url string) {
	resp, err := http.Post(url, "application/json", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	antStream(w, resp.Body, "claude-sonnet")
}

func TestAntKeyOK(t *testing.T) {
	r := httptest.NewRequest("POST", "http://m.oai.ajean.link/v1/messages", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	if readAPIKey() == "" && antKeyOK(r) {
		t.Fatal("sans clé, un Host public ne doit pas passer")
	}
}
