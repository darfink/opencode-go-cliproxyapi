package plugin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestModelsPublishAllVerifiedEfforts(t *testing.T) {
	want := map[string]string{
		"muse-spark-1.3-contributor": "minimal,low,medium,high,xhigh,max",
		"grok-4.7":                   "minimal,low,medium,high,xhigh",
		"deepseek-v4-pro":            "none,minimal,low,medium,high,xhigh,max,ultra",
		"deepseek-v4.1-flash":        "none,minimal,low,medium,high,xhigh,max,ultra",
		"glm-5.1":                    "low,medium,high,xhigh,max",
		"glm-5.3":                    "low,medium,high,xhigh,max",
		"glm-5.3-flash":              "none,minimal,low,medium,high,xhigh,max",
		"hy3":                        "none,minimal,low,medium,high,xhigh,max",
		"hy4-preview":                "none,minimal,low,medium,high,xhigh,max",
		"kimi-k2.6":                  "none,minimal,low,medium,high,xhigh,max",
		"kimi-k2.7-code":             "minimal,low,medium,high,xhigh,max,ultra",
		"kimi-k3":                    "none,minimal,low,medium,high,xhigh,max",
		"longcat-2.0":                "none,minimal,low,medium,high,xhigh,max,ultra",
		"longcat-2.5-preview-free":   "none,minimal,low,medium,high,xhigh,max,ultra",
		"mimo-v2.6-flash":            "none,low,medium,high",
		"mimo-v2.6-pro":              "none,low,medium,high",
		"minimax-m2.5":               "none,minimal,low,medium,high,xhigh,max",
		"minimax-m3":                 "none,minimal,low,medium,high,xhigh,max",
		"qwen3.6-plus":               "none,minimal,low,medium,high,xhigh",
		"qwen3.7-max":                "none,minimal,low,medium,high,xhigh,max",
		"qwen3.8-flash":              "none,minimal,low,medium,high,xhigh,max",
		"qwen3.8-max":                "none,minimal,low,medium,high,xhigh,max",
		"glm-future":                 "low,medium,high",
	}
	entries := make([]map[string]string, 0, len(want))
	for id := range want {
		entries = append(entries, map[string]string{"id": id})
	}
	body, err := json.Marshal(map[string]any{"data": entries})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newTestManager(catalogResponder(true, string(body)))
	t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
	mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
	var static, forAuth pluginapi.ModelResponse
	decodeResult(t, mustHandle(t, m, "model.static", nil), &static)
	decodeResult(t, mustHandle(t, m, "model.for_auth", []byte("{}")), &forAuth)
	if len(static.Models) != len(want) || !reflect.DeepEqual(static, forAuth) {
		t.Fatalf("model providers disagree: static=%+v for_auth=%+v", static, forAuth)
	}
	for _, model := range static.Models {
		id := strings.TrimPrefix(model.ID, "opencode-go/")
		levels, ok := want[id]
		if !ok || model.Thinking == nil || strings.Join(model.Thinking.Levels, ",") != levels {
			t.Fatalf("published thinking for %s = %+v, want %s", id, model.Thinking, levels)
		}
	}
}

func TestModelsPublishExplicitThinkingInsteadOfFallback(t *testing.T) {
	m, _ := newTestManager(catalogResponder(true,
		`{"data":[{"id":"muse-spark-1.3-contributor","thinking":{"levels":["low","high"]}}]}`))
	t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
	mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
	for _, method := range []string{"model.static", "model.for_auth"} {
		var models pluginapi.ModelResponse
		decodeResult(t, mustHandle(t, m, method, []byte("{}")), &models)
		if len(models.Models) != 1 || models.Models[0].Thinking == nil ||
			strings.Join(models.Models[0].Thinking.Levels, ",") != "low,high" {
			t.Fatalf("%s ignored declared thinking: %+v", method, models)
		}
	}
}

func TestExecuteForwardsNewlyAdvertisedEfforts(t *testing.T) {
	for _, tc := range []struct {
		id     string
		effort string
		native bool
	}{
		{"muse-spark-1.3-contributor", "max", true},
		{"grok-4.7", "xhigh", true},
		{"glm-5.3", "max", false},
		{"deepseek-v4-pro", "none", false},
		{"deepseek-v4-pro", "ultra", false},
		{"longcat-2.5-preview-free", "ultra", false},
		{"mimo-v2.6-pro", "none", false},
		{"kimi-k2.7-code", "minimal", false},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.id+"/"+tc.effort+map[bool]string{true: "/stream", false: "/non-stream"}[stream], func(t *testing.T) {
				endpoint, result := "/v1/chat/completions", ccResponseBody
				frames := []string{
					`data: {"id":"r","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":"stop"}]}` + "\n\n",
					"data: [DONE]\n\n",
				}
				if tc.native {
					endpoint = "/v1/responses"
					result = `{"id":"r","object":"response","status":"completed","output":[]}`
					frames = []string{"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + result + "}\n\n"}
				}
				responder := upstreamRouter(t, map[string]string{endpoint: result})
				httpMethod := pluginabi.MethodHostHTTPDo
				if stream {
					responder = streamResponder(streamScript{upstreamID: "up", frames: frames})
					httpMethod = pluginabi.MethodHostHTTPDoStream
				}
				catalogBody, err := json.Marshal(map[string]any{"data": []map[string]string{{"id": tc.id}}})
				if err != nil {
					t.Fatal(err)
				}
				m, f := newTestManager(wrapWithCatalog(string(catalogBody), responder))
				t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
				mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
				request, err := json.Marshal(map[string]any{
					"input": "Reply with OK.", "reasoning": map[string]string{"effort": tc.effort}, "stream": stream,
				})
				if err != nil {
					t.Fatal(err)
				}
				mustHandle(t, m, "executor.execute", execReqBody("opencode-go/"+tc.id, "openai-response", request, stream))
				if stream && !m.bridge.WaitForInFlight(5*time.Second) {
					t.Fatal("stream did not finish")
				}
				var upstream struct {
					ReasoningEffort string `json:"reasoning_effort"`
					Reasoning       struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
				}
				if err := json.Unmarshal(wireBody(t, lastWire(t, f, httpMethod), "body"), &upstream); err != nil {
					t.Fatal(err)
				}
				got := upstream.ReasoningEffort
				if tc.native {
					got = upstream.Reasoning.Effort
				}
				if got != tc.effort {
					t.Fatalf("forwarded effort = %q, want %q", got, tc.effort)
				}
			})
		}
	}
}

func TestExecuteForwardsVerifiedMessagesEfforts(t *testing.T) {
	for _, id := range []string{"minimax-m2.5", "minimax-m3", "qwen3.6-plus", "qwen3.7-max", "qwen3.8-flash", "qwen3.8-max"} {
		for _, tc := range []struct {
			effort string
			budget int64
		}{
			{"none", 0}, {"minimal", 512}, {"low", 1024}, {"medium", 8192},
			{"high", 24576}, {"xhigh", 32768}, {"max", 128000},
		} {
			if id == "qwen3.6-plus" && tc.effort == "max" {
				continue // This conversion fails upstream and must not be advertised.
			}
			for _, stream := range []bool{false, true} {
				t.Run(id+"/"+tc.effort+map[bool]string{true: "/stream", false: "/non-stream"}[stream], func(t *testing.T) {
					responder := upstreamRouter(t, map[string]string{"/v1/messages": claudeResponseBody})
					httpMethod := pluginabi.MethodHostHTTPDo
					if stream {
						httpMethod = pluginabi.MethodHostHTTPDoStream
						responder = streamResponder(streamScript{upstreamID: "up", frames: []string{
							`data: {"type":"message_start","message":{"id":"r","usage":{"input_tokens":1}}}` + "\n\n",
							`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n",
							`data: {"type":"message_stop"}` + "\n\n",
						}})
					}
					catalogBody, err := json.Marshal(map[string]any{"data": []map[string]string{{"id": id}}})
					if err != nil {
						t.Fatal(err)
					}
					m, f := newTestManager(wrapWithCatalog(string(catalogBody), responder))
					t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
					mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
					request, err := json.Marshal(map[string]any{
						"input": "Reply with OK.", "reasoning": map[string]string{"effort": tc.effort},
						"stream": stream, "max_output_tokens": 256,
					})
					if err != nil {
						t.Fatal(err)
					}
					mustHandle(t, m, "executor.execute", execReqBody("opencode-go/"+id, "openai-response", request, stream))
					if stream && !m.bridge.WaitForInFlight(5*time.Second) {
						t.Fatal("stream did not finish")
					}
					var upstream struct {
						MaxTokens int64 `json:"max_tokens"`
						Thinking  *struct {
							Type         string `json:"type"`
							BudgetTokens int64  `json:"budget_tokens"`
						} `json:"thinking"`
					}
					if err := json.Unmarshal(wireBody(t, lastWire(t, f, httpMethod), "body"), &upstream); err != nil {
						t.Fatal(err)
					}
					if tc.budget == 0 {
						if upstream.Thinking != nil || upstream.MaxTokens != 256 {
							t.Fatalf("off control changed: %+v", upstream)
						}
					} else if upstream.Thinking == nil || upstream.Thinking.Type != "enabled" ||
						upstream.Thinking.BudgetTokens != tc.budget || upstream.MaxTokens != tc.budget+1024 {
						t.Fatalf("budget control changed: %+v thinking=%+v", upstream, upstream.Thinking)
					}
				})
			}
		}
	}
}
