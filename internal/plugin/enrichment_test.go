package plugin

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/errclass"
)

func TestExecuteModelEnrichmentSearchPolicyAndUsage(t *testing.T) {
	const models = `{"data":[{"id":"muse-spark-1.3-contributor"},{"id":"grok-4.7"},{"id":"gpt-6.1-sol"}]}`
	// Preserve billing even when a provider reports usage above the context window.
	const response = `{"id":"r","output":[],"usage":{"input_tokens":1348898,"output_tokens":11237,"total_tokens":1360135,"input_tokens_details":{"cached_tokens":1292620},"output_tokens_details":{"reasoning_tokens":7598}}}`
	for _, model := range []string{"muse-spark-1.3-contributor", "grok-4.7", "gpt-6.1-sol"} {
		for _, disabled := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(model+fmt.Sprintf("/disabled=%t/stream=%t", disabled, stream), func(t *testing.T) {
					responder := upstreamRouter(t, map[string]string{"/v1/responses": response})
					if stream {
						responder = streamResponder(streamScript{upstreamID: "up", frames: []string{
							`data: {"type":"response.completed","response":` + response + "}\n\n",
						}})
					}
					m, f := newTestManager(wrapWithCatalog(models, responder))
					t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
					policy := "enabled"
					if disabled {
						policy = "disabled"
					}
					cfg := testValidYAML + fmt.Sprintf("\nmodel-enrichments:\n  %s:\n    hosted-web-search: %s\n", model, policy)
					mustHandle(t, m, "plugin.register", lifecycleRequestBody(cfg))
					method := "executor.execute"
					request := execReqBody("opencode-go/"+model, "openai-response", []byte(namespaceRequest), false)
					httpMethod := pluginabi.MethodHostHTTPDo
					if stream {
						method = "executor.execute_stream"
						request = execStreamReqBody("opencode-go/"+model, "openai-response", []byte(namespaceRequest), "down")
						httpMethod = pluginabi.MethodHostHTTPDoStream
					}
					result := mustHandle(t, m, method, request)
					var body []byte
					if stream {
						if !m.bridge.WaitForInFlight(5 * time.Second) {
							t.Fatal("stream did not finish")
						}
						for _, call := range f.callsOf(pluginabi.MethodHostStreamEmit) {
							frame := string(wireBody(t, decodePayload(t, call), "payload"))
							for _, line := range strings.Split(frame, "\n") {
								if strings.HasPrefix(line, "data: ") {
									var event struct {
										Response json.RawMessage `json:"response"`
									}
									if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
										t.Fatal(err)
									}
									if event.Response != nil {
										body = event.Response
									}
								}
							}
						}
					} else {
						var out pluginapi.ExecutorResponse
						decodeResult(t, result, &out)
						body = out.Payload
					}
					var got, want map[string]any
					if err := json.Unmarshal(body, &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(response), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got["usage"], want["usage"]) {
						t.Fatalf("billing usage changed: %s", body)
					}
					var sent struct{ Tools []struct{ Type, Name string } }
					if err := json.Unmarshal(wireBody(t, lastWire(t, f, httpMethod), "body"), &sent); err != nil {
						t.Fatal(err)
					}
					wantTools := 2
					if disabled {
						wantTools = 1
					}
					wantName, wantType := "functions__exec", "function"
					if model == "gpt-6.1-sol" {
						wantName, wantType = "functions", "namespace"
					}
					if len(sent.Tools) != wantTools || sent.Tools[0].Name != wantName || sent.Tools[0].Type != wantType {
						t.Fatalf("sent tools = %+v, want %d", sent.Tools, wantTools)
					}
					var catalog pluginapi.ModelResponse
					decodeResult(t, mustHandle(t, m, "model.static", nil), &catalog)
					if catalog.Models[0].ContextLength != 1_048_576 || catalog.Models[1].ContextLength != 0 {
						t.Fatalf("model context limits = %+v", catalog.Models)
					}
				})
			}
		}
	}
}

func TestExecuteDisabledHostedSearchRejectsBeforeHTTP(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, choice := range []string{`{"type":"web_search"}`, `"required"`} {
			t.Run(fmt.Sprintf("stream=%t/choice=%s", stream, choice), func(t *testing.T) {
				m, f := newTestManager(wrapWithCatalog(`{"data":[{"id":"muse-spark-1.3-contributor"}]}`, upstreamRouter(t, nil)))
				t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
				decodeResult(t, mustHandle(t, m, "plugin.register", lifecycleRequestBody(testValidYAML+"\nmodel-enrichments:\n  muse-spark-1.3-contributor:\n    hosted-web-search: disabled\n")), &registrationResult{})
				httpCalls := len(f.callsOf(pluginabi.MethodHostHTTPDo))
				body := []byte(`{"input":"hi","tools":[{"type":"web_search"}],"tool_choice":` + choice + `}`)
				method := "executor.execute"
				request := execReqBody("opencode-go/muse-spark-1.3-contributor", "openai-response", body, false)
				if stream {
					method = "executor.execute_stream"
					request = execStreamReqBody("opencode-go/muse-spark-1.3-contributor", "openai-response", body, "down")
				}
				env := decodeEnv(t, mustHandle(t, m, method, request))
				if env.OK || env.Error == nil || env.Error.Code != string(errclass.ClassUnsupported) || env.Error.HTTPStatus != 400 {
					t.Fatalf("disabled search envelope = %+v", env)
				}
				if len(f.callsOf(pluginabi.MethodHostHTTPDo)) != httpCalls || len(f.callsOf(pluginabi.MethodHostHTTPDoStream)) != 0 {
					t.Fatal("disabled hosted search reached the provider")
				}
			})
		}
	}
}

func TestModelEnrichmentPublishesConfiguredCapabilities(t *testing.T) {
	m, _ := newTestManager(catalogResponder(true, `{"data":[{"id":"gpt-custom","context_length":4096,"thinking":{"levels":["low"],"max":64000}}]}`))
	t.Cleanup(func() { _, _ = m.HandleCall("plugin.shutdown", nil) })
	cfg := testValidYAML + `
model-enrichments:
  gpt-custom:
    reasoning-efforts: [none, ultra, auto]
    context-window: 131072
    hosted-web-search: disabled
`
	decodeResult(t, mustHandle(t, m, "plugin.register", lifecycleRequestBody(cfg)), &registrationResult{})
	var models pluginapi.ModelResponse
	decodeResult(t, mustHandle(t, m, "model.static", nil), &models)
	if len(models.Models) != 1 {
		t.Fatalf("published models = %+v", models)
	}
	got := models.Models[0]
	if got.ID != "opencode-go/gpt-custom" || got.ContextLength != 131_072 || got.Thinking == nil ||
		!reflect.DeepEqual(got.Thinking.Levels, []string{"none", "ultra", "auto"}) || got.Thinking.Max != 64000 {
		t.Fatalf("published enrichment = %+v", got)
	}
}
