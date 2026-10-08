package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/errclass"
)

const namespaceRequest = `{"model":"opencode-go/glm-5.3","input":"Use exec","reasoning":{"effort":"high"},"stream":true,
	"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec","parameters":{"type":"object","properties":{}}}]},{"type":"web_search"}]}`

func TestExecuteCodexNamespaceTools(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, model := range []string{"opencode-go/glm-5.3", "opencode-go/minimax-m3"} {
			t.Run(model+map[bool]string{false: "/non-stream", true: "/stream"}[stream], func(t *testing.T) {
				var m *Manager
				var f *fakeCaller
				if stream {
					frames := []string{
						`data: {"id":"r","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"functions__exec","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n",
						"data: [DONE]\n\n",
					}
					if strings.Contains(model, "minimax") {
						frames = []string{
							`data: {"type":"message_start","message":{"id":"r","usage":{"input_tokens":1}}}` + "\n\n",
							`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"functions__exec"}}` + "\n\n",
							`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}` + "\n\n",
							`data: {"type":"content_block_stop","index":0}` + "\n\n",
							`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}` + "\n\n",
							`data: {"type":"message_stop"}` + "\n\n",
						}
					}
					m, f = newStreamManager(t, streamScript{upstreamID: "up", frames: frames})
				} else {
					m, f = newExecManager(t)
					f.responder = wrapWithCatalog(multiRouteCatalog, upstreamRouter(t, map[string]string{
						"/v1/chat/completions": `{"id":"r","choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"functions__exec","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
						"/v1/messages":         `{"id":"r","content":[{"type":"tool_use","id":"call_1","name":"functions__exec","input":{}}],"stop_reason":"tool_use"}`,
					}))
				}
				request := []byte(namespaceRequest)
				method := "executor.execute"
				payload := execReqBody(model, "openai-response", request, false)
				if stream {
					method = "executor.execute_stream"
					payload = execStreamReqBody(model, "openai-response", request, "down")
				}
				resp, err := m.HandleCall(method, payload)
				if err != nil || !decodeEnv(t, resp).OK {
					t.Fatalf("execute = %v %s", err, resp)
				}
				var result []byte
				httpMethod := pluginabi.MethodHostHTTPDo
				if stream {
					httpMethod = pluginabi.MethodHostHTTPDoStream
					if !m.bridge.WaitForInFlight(5 * time.Second) {
						t.Fatal("stream did not finish")
					}
					for _, call := range f.callsOf(pluginabi.MethodHostStreamEmit) {
						result = append(result, wireBody(t, decodePayload(t, call), "payload")...)
					}
					if !strings.Contains(string(result), "response.completed") || !strings.Contains(string(result), "\n\n") {
						t.Fatalf("stream framing/completion lost: %s", result)
					}
				} else {
					var response pluginapi.ExecutorResponse
					decodeResult(t, resp, &response)
					result = response.Payload
				}
				if !strings.Contains(string(result), `"namespace":"functions"`) || !strings.Contains(string(result), `"name":"exec"`) {
					t.Fatalf("tool identity lost through executor: %s", result)
				}
				body := wireBody(t, lastWire(t, f, httpMethod), "body")
				var upstream map[string]any
				if err := json.Unmarshal(body, &upstream); err != nil {
					t.Fatal(err)
				}
				if len(upstream["tools"].([]any)) != 1 || strings.Contains(string(body), "web_search") {
					t.Fatalf("upstream tools = %s", body)
				}
			})
		}
	}
}

func TestExecuteRejectsUnsupportedMessagesEffortBeforeHTTP(t *testing.T) {
	m, f := newTestManager(catalogResponder(true,
		`{"data":[{"id":"qwen3.6-plus"},{"id":"minimax-m3"}]}`))
	t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
	mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
	before := len(f.callsOf(pluginabi.MethodHostHTTPDo))
	for _, tc := range []struct{ model, effort string }{
		{"qwen3.6-plus", "max"}, {"qwen3.6-plus", "ultra"},
		{"minimax-m3", "maximum"},
	} {
		for _, stream := range []bool{false, true} {
			body := []byte(`{"input":"hi","reasoning":{"effort":"` + tc.effort + `"}}`)
			resp, err := m.HandleCall("executor.execute", execReqBody("opencode-go/"+tc.model, "openai-response", body, stream))
			if err != nil {
				t.Fatal(err)
			}
			env := decodeEnv(t, resp)
			if env.OK || env.Error == nil || env.Error.Code != string(errclass.ClassUnsupported) {
				t.Fatalf("unsupported effort %s/%s = %s", tc.model, tc.effort, resp)
			}
		}
	}
	if len(f.callsOf(pluginabi.MethodHostHTTPDo)) != before || len(f.callsOf(pluginabi.MethodHostHTTPDoStream)) != 0 {
		t.Fatal("invalid request reached upstream")
	}
}

func TestExecuteNamedEffortReachesUpstreamAndReturnsItsError(t *testing.T) {
	const models = `{"data":[{"id":"glm-5.3"},{"id":"muse-spark-1.3-contributor"},{"id":"grok-4.7"}]}`
	const errorBody = `{"error":{"message":"upstream rejected reasoning effort"}}`
	for _, tc := range []struct{ model, effort string }{
		{"glm-5.3", "ultra"},
		{"muse-spark-1.3-contributor", "ultra"}, {"grok-4.7", "max"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.model+"/stream="+fmt.Sprint(stream), func(t *testing.T) {
				responder := streamResponder(streamScript{startStatus: 400, upstreamID: "up-error", frames: []string{errorBody}})
				if !stream {
					responder = func(method string, _ []byte) ([]byte, error) {
						if method == pluginabi.MethodHostHTTPDo {
							return hostOK(pluginapi.HTTPResponse{StatusCode: 400, Body: []byte(errorBody)}), nil
						}
						return hostOK(map[string]any{}), nil
					}
				}
				m, f := newTestManager(wrapWithCatalog(models, responder))
				t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
				mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML))
				body := []byte(`{"input":"hi","reasoning":{"effort":"` + tc.effort + `"}}`)
				resp, err := m.HandleCall("executor.execute", execReqBody("opencode-go/"+tc.model, "openai-response", body, stream))
				if err != nil {
					t.Fatal(err)
				}
				env := decodeEnv(t, resp)
				if env.OK || env.Error == nil || env.Error.Message != "upstream rejected reasoning effort" || env.Error.HTTPStatus != 400 {
					t.Fatalf("upstream error lost: %s", resp)
				}
				method := pluginabi.MethodHostHTTPDo
				if stream {
					method = pluginabi.MethodHostHTTPDoStream
				}
				var upstream map[string]any
				if err := json.Unmarshal(wireBody(t, lastWire(t, f, method), "body"), &upstream); err != nil {
					t.Fatal(err)
				}
				effort := upstream["reasoning_effort"]
				if reasoning, ok := upstream["reasoning"].(map[string]any); ok {
					effort = reasoning["effort"]
				}
				if effort != tc.effort {
					t.Fatalf("effort lost upstream: %+v", upstream)
				}
			})
		}
	}
}

func TestExecuteNativeResponsesToolNormalization(t *testing.T) {
	const nativeCatalog = `{"data":[{"id":"muse-spark-1.3-contributor"},{"id":"grok-4.7"}]}`
	const nativeResult = `{"id":"native","status":"completed","output":[{"type":"function_call","id":"fc","call_id":"call_1","name":"functions__exec","arguments":"{}"}],"usage":{"total_tokens":3}}`
	for _, model := range []string{"opencode-go/muse-spark-1.3-contributor", "opencode-go/grok-4.7"} {
		for _, stream := range []bool{false, true} {
			t.Run(model+map[bool]string{true: "/stream", false: "/non-stream"}[stream], func(t *testing.T) {
				responder := upstreamRouter(t, map[string]string{"/v1/responses": nativeResult})
				httpMethod := pluginabi.MethodHostHTTPDo
				if stream {
					httpMethod = pluginabi.MethodHostHTTPDoStream
					responder = streamResponder(streamScript{upstreamID: "native-up", frames: []string{
						`event: response.output_item.added` + "\n" + `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call_1","name":"functions__exec","arguments":""}}` + "\n\n",
						`event: response.completed` + "\n" + `data: {"type":"response.completed","response":` + nativeResult + `}` + "\n\n",
					}})
				}
				m, f := newTestManager(wrapWithCatalog(nativeCatalog, responder))
				t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
				if _, err := m.HandleCall("plugin.register", lifecycleRequestBody(testValidYAML)); err != nil {
					t.Fatal(err)
				}
				resp, err := m.HandleCall("executor.execute", execReqBody(model, "openai-response", []byte(namespaceRequest), stream))
				if err != nil || !decodeEnv(t, resp).OK {
					t.Fatalf("native execute = %s %v", resp, err)
				}
				var result []byte
				if stream {
					if !m.bridge.WaitForInFlight(5 * time.Second) {
						t.Fatal("native stream did not finish")
					}
					for _, call := range f.callsOf(pluginabi.MethodHostStreamEmit) {
						result = append(result, wireBody(t, decodePayload(t, call), "payload")...)
					}
				} else {
					var response pluginapi.ExecutorResponse
					decodeResult(t, resp, &response)
					result = response.Payload
				}
				if !strings.Contains(string(result), `"namespace":"functions"`) || !strings.Contains(string(result), `"name":"exec"`) ||
					!strings.Contains(string(result), `"total_tokens":3`) {
					t.Fatalf("native response identity/usage = %s", result)
				}
				wire := lastWire(t, f, httpMethod)
				body := wireBody(t, wire, "body")
				var upstream map[string]any
				if err := json.Unmarshal(body, &upstream); err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(wire["url"].(string), "/responses") || len(upstream["tools"].([]any)) != 2 ||
					!strings.Contains(string(body), `"name":"functions__exec"`) || !strings.Contains(string(body), `"type":"web_search"`) {
					t.Fatalf("native route/tools = %s", body)
				}
				if _, ok := upstream["input"].([]any); !ok {
					t.Fatalf("native input was not normalized to an array: %s", body)
				}
			})
		}
	}
}

func TestNativeResponsesCompatibilityScope(t *testing.T) {
	for _, tc := range []struct {
		model, format string
		want          bool
	}{
		{"muse-spark-1.3-contributor", "openai-response", true},
		{"grok-4.7", "openai-response", true},
		{"gpt-5.6-luna", "openai-response", false},
		{"muse-spark-1.3-contributor", "openai", false},
	} {
		if got := usesResponsesCompat("responses", tc.format, tc.model); got != tc.want {
			t.Fatalf("compatibility scope for %+v = %v", tc, got)
		}
	}
}
