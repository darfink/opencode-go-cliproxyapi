package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/catalog"
)

func TestPrepareModelReasoning(t *testing.T) {
	for _, tc := range []struct {
		name               string
		policy             catalog.ReasoningPolicy
		format, body, kind string
		bad                bool
	}{
		{"no policy", "", "openai", `{"reasoning_effort":"ultra"}`, "", false},
		{"fixed", catalog.ReasoningFixed, "openai-response", `{"reasoning":{"effort":"max","summary":"auto"}}`, "", false},
		{"alias enabled", catalog.ReasoningToggle, "openai", `{"reasoning_effort":"ultra"}`, "enabled", false},
		{"adaptive", catalog.ReasoningAdaptiveToggle, "openai-response", `{"reasoning":{"effort":"high"}}`, "adaptive", false},
		{"disabled", catalog.ReasoningAdaptiveToggle, "openai", `{"reasoning_effort":"none"}`, "disabled", false},
		{"native enabled", catalog.ReasoningAdaptiveToggle, "claude", `{"thinking":{"type":"enabled","budget_tokens":8192}}`, "enabled", false},
		{"native adaptive", catalog.ReasoningAdaptiveToggle, "claude", `{"thinking":{"type":"adaptive"}}`, "adaptive", false},
		{"native effort", catalog.ReasoningAdaptiveToggle, "claude", `{"output_config":{"effort":"high","other":9007199254740993}}`, "adaptive", false},
		{"native off wins", catalog.ReasoningAdaptiveToggle, "claude", `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`, "disabled", false},
		{"unspecified", catalog.ReasoningToggle, "openai", `{}`, "", false},
		{"unknown effort", catalog.ReasoningToggle, "openai", `{"reasoning_effort":"maximum"}`, "", true},
		{"unknown type", catalog.ReasoningToggle, "claude", `{"thinking":{"type":"unknown"}}`, "", true},
		{"invalid effort", catalog.ReasoningToggle, "openai", `{"reasoning_effort":12}`, "", true},
		{"invalid thinking", catalog.ReasoningToggle, "claude", `{"thinking":[]}`, "", true},
		{"invalid control", catalog.ReasoningFixed, "claude", `{"output_config":[]}`, "", true},
		{"null body", catalog.ReasoningFixed, "openai", `null`, "", true},
		{"malformed body", catalog.ReasoningToggle, "openai", `{`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, kind, err := prepareModelReasoning(tc.policy, tc.format, []byte(tc.body))
			if (err != nil) != tc.bad || kind != tc.kind {
				t.Fatalf("prepare = %s %q %v", body, kind, err)
			}
			if tc.bad {
				return
			}
			if tc.policy == "" {
				if string(body) != tc.body {
					t.Fatal("unrelated model changed")
				}
				return
			}
			out, eErr := applyModelReasoning(tc.policy, kind, body)
			if eErr != nil {
				t.Fatal(eErr)
			}
			if strings.Contains(string(out), `"effort"`) || strings.Contains(string(out), `"budget_tokens"`) {
				t.Fatalf("synthetic control survived: %s", out)
			}
			if strings.Contains(tc.body, "9007199254740993") && !strings.Contains(string(out), "9007199254740993") {
				t.Fatalf("unrelated integer changed: %s", out)
			}
			if strings.Contains(tc.body, `"summary"`) && !strings.Contains(string(out), `"summary":"auto"`) {
				t.Fatalf("reasoning summary changed: %s", out)
			}
		})
	}
}

func TestExecuteFixedAndToggleReasoning(t *testing.T) {
	for _, tc := range []struct {
		id, enabled string
		messages    bool
	}{
		{"longcat-2.0", "enabled", false},
		{"longcat-2.5-preview-free", "enabled", false},
		{"minimax-m3", "adaptive", true},
		{"kimi-k2.6", "", false},
		{"kimi-k2.7-code", "", false},
		{"mimo-v2.6-flash", "", false},
		{"mimo-v2.6-pro", "", false},
		{"minimax-m2.5", "", true},
	} {
		for _, format := range []string{"openai-response", "openai", "claude"} {
			for _, effort := range []string{"none", "high", "ultra"} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/stream=%t", tc.id, format, effort, stream), func(t *testing.T) {
						endpoint, response := "/v1/chat/completions", ccResponseBody
						frames := []string{`data: {"id":"r","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":"stop"}]}` + "\n\n", "data: [DONE]\n\n"}
						if tc.messages {
							endpoint, response = "/v1/messages", claudeResponseBody
							frames = []string{
								`data: {"type":"message_start","message":{"id":"r","usage":{"input_tokens":1}}}` + "\n\n",
								`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n",
								`data: {"type":"message_stop"}` + "\n\n",
							}
						}
						responder := upstreamRouter(t, map[string]string{endpoint: response})
						method := pluginabi.MethodHostHTTPDo
						if stream {
							responder = streamResponder(streamScript{upstreamID: "up", frames: frames})
							method = pluginabi.MethodHostHTTPDoStream
						}
						m, f := newTestManager(wrapWithCatalog(`{"data":[{"id":"`+tc.id+`"}]}`, responder))
						t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
						mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
						request := map[string]any{"max_tokens": 256, "messages": []map[string]string{{"role": "user", "content": "hi"}}, "stream": stream}
						switch format {
						case "openai-response":
							request = map[string]any{"input": "hi", "max_output_tokens": 256, "reasoning": map[string]string{"effort": effort}, "stream": stream}
						case "openai":
							request["reasoning_effort"] = effort
						case "claude":
							request["output_config"] = map[string]string{"effort": effort}
						}
						body, err := json.Marshal(request)
						if err != nil {
							t.Fatal(err)
						}
						result := mustHandle(t, m, "executor.execute", execReqBody("opencode-go/"+tc.id, format, body, stream))
						if !decodeEnv(t, result).OK {
							t.Fatalf("execute failed: %s", result)
						}
						if stream && !m.bridge.WaitForInFlight(5*time.Second) {
							t.Fatal("stream did not finish")
						}
						var wire struct {
							MaxTokens int64 `json:"max_tokens"`
							Thinking  *struct {
								Type   string `json:"type"`
								Budget *int64 `json:"budget_tokens"`
							} `json:"thinking"`
							ReasoningEffort string `json:"reasoning_effort"`
						}
						out := wireBody(t, lastWire(t, f, method), "body")
						if err := json.Unmarshal(out, &wire); err != nil {
							t.Fatal(err)
						}
						kind := tc.enabled
						if effort == "none" && kind != "" {
							kind = "disabled"
						}
						if wire.ReasoningEffort != "" || strings.Contains(string(out), `"effort"`) {
							t.Fatalf("fake tier survived: %s", out)
						}
						if kind == "" {
							if wire.Thinking != nil {
								t.Fatalf("fixed model gained a toggle: %s", out)
							}
						} else if wire.Thinking == nil || wire.Thinking.Type != kind || wire.Thinking.Budget != nil {
							t.Fatalf("thinking = %+v, want %q without budget: %s", wire.Thinking, kind, out)
						}
						if wire.MaxTokens != 256 {
							t.Fatalf("output budget changed: %s", out)
						}
					})
				}
			}
		}
	}
}

func TestApplyModelReasoningRejectsMalformedTranslation(t *testing.T) {
	for _, body := range []string{`{`, `null`, `[]`, `{"reasoning":[]}`, `{"output_config":true}`} {
		if out, err := applyModelReasoning(catalog.ReasoningToggle, "enabled", []byte(body)); err == nil {
			t.Fatalf("invalid translation accepted: %s -> %s", body, out)
		}
	}
	body := []byte(`{"reasoning_effort":"high"}`)
	if out, err := applyModelReasoning("", "", body); err != nil || string(out) != string(body) {
		t.Fatalf("unrelated model changed: %s %v", out, err)
	}
}

func TestModelsHideEquivalentMessagesBudgetsOnly(t *testing.T) {
	m, _ := newTestManager(catalogResponder(true, `{"data":[
		{"id":"qwen-test","thinking":{"levels":["minimal","low","medium","high","xhigh","max"],"min":1024,"max":32768,"zero_allowed":true}},
		{"id":"glm-test","thinking":{"levels":["minimal","low","medium","high","xhigh","max"],"min":1024,"max":32768,"zero_allowed":true}}]}`))
	t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
	mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
	var models pluginapi.ModelResponse
	decodeResult(t, mustHandle(t, m, "model.static", nil), &models)
	for _, model := range models.Models {
		want := "none,low,medium,high,xhigh"
		if model.ID == "opencode-go/glm-test" {
			want = "none,minimal,low,medium,high,xhigh,max"
		}
		if got := strings.Join(model.Thinking.Levels, ","); got != want {
			t.Fatalf("%s: %s, want %s", model.ID, got, want)
		}
	}
}
