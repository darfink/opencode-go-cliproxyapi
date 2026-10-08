package responsescompat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/catalog"
	"opencode-go-cliproxyapi/internal/errclass"
)

func TestCodexToolDeclarations(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		t.Run(string(route), func(t *testing.T) {
			out, eErr := BuildRequest(route, "test", []byte(codexToolsRequest), nil)
			if eErr != nil {
				t.Fatal(eErr)
			}
			names := toolNames(t, route, out)
			if len(names) != 2 || names[0] == names[1] || !strings.Contains(names[0], "exec") || !strings.Contains(names[1], "patch") {
				t.Fatalf("tools not flattened correctly: %s", out)
			}
			obj := decodeObject(t, out)
			if obj["model"] != "test" || obj["stream"] != true {
				t.Fatalf("envelope = %s", out)
			}
			if route == catalog.RouteChatCompletions {
				if obj["reasoning_effort"] != "high" || obj["stream_options"].(map[string]any)["include_usage"] != true {
					t.Fatalf("reasoning/usage = %s", out)
				}
			} else if obj["thinking"].(map[string]any)["budget_tokens"] != float64(24576) || obj["max_tokens"].(float64) <= 24576 {
				t.Fatalf("thinking budget = %s", out)
			}
		})
	}
}

func TestRequestValidation(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		for _, tc := range []struct {
			name, body string
			class      errclass.Class
		}{
			{"hosted tool forced", `{"input":"hi","tools":[{"type":"web_search"}],"tool_choice":{"type":"web_search"}}`, errclass.ClassUnsupported},
			{"unknown tool", `{"input":"hi","tools":[{"type":"file_search"}]}`, errclass.ClassUnsupported},
			{"unknown nested tool", `{"input":"hi","tools":[{"type":"namespace","name":"n","tools":[{"type":"computer_use"}]}]}`, errclass.ClassUnsupported},
			{"additional unknown tool", `{"input":[{"type":"additional_tools","tools":[{"type":"mcp"}]}]}`, errclass.ClassUnsupported},
			{"missing tool name", `{"input":"hi","tools":[{"type":"function"}]}`, errclass.ClassTranslation},
			{"nested namespace", `{"input":"hi","tools":[{"type":"namespace","name":"a","tools":[{"type":"namespace","name":"b","tools":[]}]}]}`, errclass.ClassUnsupported},
			{"unknown input", `{"input":[{"type":"item_reference","id":"i"}]}`, errclass.ClassUnsupported},
			{"bad role", `{"input":[{"role":"invalid","content":"hi"}]}`, errclass.ClassUnsupported},
			{"bad content", `{"input":[{"role":"user","content":[{"type":"input_audio"}]}]}`, errclass.ClassUnsupported},
			{"null", `null`, errclass.ClassTranslation},
			{"bad tools", `{"input":"hi","tools":{}}`, errclass.ClassTranslation},
		} {
			t.Run(string(route)+"/"+tc.name, func(t *testing.T) {
				_, eErr := BuildRequest(route, "m", []byte(tc.body), nil)
				if eErr == nil || eErr.Class != tc.class {
					t.Fatalf("error = %v, want %s", eErr, tc.class)
				}
			})
		}
	}
}

func TestNamedEffortsPassThroughButMessagesRequiresBudget(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, effort := range []string{"xhigh", "max", "ultra", "maximum"} {
			body := []byte(fmt.Sprintf(`{"input":"hi","stream":%t,"reasoning":{"effort":"%s"}}`, stream, effort))
			out, eErr := BuildRequest(catalog.RouteChatCompletions, "m", body, nil)
			if eErr != nil || decodeObject(t, out)["reasoning_effort"] != effort {
				t.Fatalf("named effort %s lost: %s %v", effort, out, eErr)
			}
			out, eErr = BuildRequest(catalog.RouteMessages, "m", body, nil)
			if len(out) != 0 || eErr == nil || eErr.Class != errclass.ClassUnsupported {
				t.Fatalf("Messages accepted unsupported budget for %s: %s %v", effort, out, eErr)
			}
		}
	}
}

func TestReasoningCapabilitiesAndSampling(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		t.Run(string(route), func(t *testing.T) {
			ts := &pluginapi.ThinkingSupport{Min: 1024, Max: 9000, Levels: []string{"low", "medium", "high", "xhigh"}, ZeroAllowed: true, DynamicAllowed: true}
			for _, effort := range []string{"xhigh", "none", "auto"} {
				body := []byte(`{"input":"hi","temperature":0.2,"top_p":0.8,"reasoning":{"effort":"` + effort + `"}}`)
				out, eErr := BuildRequest(route, "m", body, ts)
				if eErr != nil {
					t.Fatal(eErr)
				}
				obj := decodeObject(t, out)
				if route == catalog.RouteChatCompletions {
					if obj["reasoning_effort"] != effort || obj["temperature"] != 0.2 || obj["top_p"] != 0.8 {
						t.Fatalf("chat controls = %s", out)
					}
				} else if effort == "xhigh" {
					if obj["thinking"].(map[string]any)["budget_tokens"] != float64(9000) || obj["max_tokens"] != float64(10024) || obj["temperature"] != nil || obj["top_p"] != nil {
						t.Fatalf("Messages thinking = %s", out)
					}
				} else if obj["thinking"] != nil || obj["temperature"] != 0.2 || obj["top_p"] != 0.8 {
					t.Fatalf("Messages off/auto = %s", out)
				}
			}
		})
	}
}

func TestUltraUsesNamedEffortOnly(t *testing.T) {
	ts := &pluginapi.ThinkingSupport{Levels: []string{"high", "ultra"}}
	for _, stream := range []bool{false, true} {
		body := []byte(fmt.Sprintf(`{"input":"hi","stream":%t,"reasoning":{"effort":"ultra"}}`, stream))
		out, eErr := BuildRequest(catalog.RouteChatCompletions, "m", body, ts)
		if eErr != nil || decodeObject(t, out)["reasoning_effort"] != "ultra" {
			t.Fatalf("named effort lost: %s %v", out, eErr)
		}
		out, eErr = BuildRequest(catalog.RouteMessages, "m", body, ts)
		if len(out) != 0 || eErr == nil || eErr.Class != errclass.ClassUnsupported || !strings.Contains(eErr.Message, "no supported Messages token budget") {
			t.Fatalf("named effort silently disabled Messages thinking: %s %v", out, eErr)
		}
	}
}

func TestNamesAndToolChoiceSurviveCollisions(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		t.Run(string(route), func(t *testing.T) {
			prefix := strings.Repeat("long", 20)
			body := []byte(`{"input":"hi","tools":[{"type":"namespace","name":"n","tools":[
				{"type":"function","name":"` + prefix + `a","parameters":{}},
				{"type":"function","name":"` + prefix + `b","parameters":{}}]}],
				"tool_choice":{"type":"function","namespace":"n","name":"` + prefix + `b"}}`)
			out, eErr := BuildRequest(route, "m", body, nil)
			if eErr != nil {
				t.Fatal(eErr)
			}
			names := toolNames(t, route, out)
			if len(names) != 2 || names[0] == names[1] || len(names[0]) > 64 || len(names[1]) > 64 {
				t.Fatalf("names = %+v", names)
			}
			choice := decodeObject(t, out)["tool_choice"].(map[string]any)
			if route == catalog.RouteChatCompletions {
				choice = choice["function"].(map[string]any)
			}
			if choice["name"] != names[1] {
				t.Fatalf("forced tool name = %+v, want %s", choice, names[1])
			}
		})
	}
}
