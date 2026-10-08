package responsescompat

import (
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/catalog"
	"opencode-go-cliproxyapi/internal/errclass"
)

func TestNonStreamToolRoundTrip(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		t.Run(string(route), func(t *testing.T) {
			original := []byte(codexToolsRequest)
			upstream, eErr := BuildRequest(route, "test", original, nil)
			if eErr != nil {
				t.Fatal(eErr)
			}
			names := toolNames(t, route, upstream)
			customArgs := string(encodeValue(t, map[string]any{"input": "*** Begin Patch\n*** End Patch"}))
			var body []byte
			if route == catalog.RouteChatCompletions {
				body = encodeValue(t, map[string]any{"id": "r1", "model": "test", "choices": []any{map[string]any{
					"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{
						map[string]any{"id": "call_f", "type": "function", "function": map[string]any{"name": names[0], "arguments": `{"command":"pwd"}`}},
						map[string]any{"id": "call_c", "type": "function", "function": map[string]any{"name": names[1], "arguments": customArgs}},
					}}}}, "usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 4, "total_tokens": 15}})
			} else {
				body = encodeValue(t, map[string]any{"id": "r1", "model": "test", "stop_reason": "tool_use", "content": []any{
					map[string]any{"type": "tool_use", "id": "call_f", "name": names[0], "input": map[string]any{"command": "pwd"}},
					map[string]any{"type": "tool_use", "id": "call_c", "name": names[1], "input": map[string]any{"input": "*** Begin Patch\n*** End Patch"}},
				}, "usage": map[string]any{"input_tokens": 11, "output_tokens": 4}})
			}
			converted, eErr := New(route, "test", original, upstream).ConvertNonStream(http.StatusOK, body)
			if eErr != nil {
				t.Fatal(eErr)
			}
			response := decodeObject(t, converted)
			source := decodeObject(t, original)
			for _, field := range []string{"model", "tools", "reasoning"} {
				if !reflect.DeepEqual(response[field], source[field]) {
					t.Fatalf("client metadata %s changed: %s", field, converted)
				}
			}
			output := response["output"].([]any)
			// The SDK emits a reasoning item when the echoed request requests it.
			if len(output) != 3 || output[0].(map[string]any)["type"] != "reasoning" {
				t.Fatalf("requested reasoning item lost: %s", converted)
			}
			assertToolIdentity(t, output[1:])
			if response["usage"].(map[string]any)["total_tokens"] != float64(15) {
				t.Fatalf("usage = %s", converted)
			}

			// Replay the returned calls and their results, not hand-written aliases.
			replay := decodeObject(t, original)
			input := replay["input"].([]any)
			input = append(input, output...)
			input = append(input, map[string]any{"type": "function_call_output", "call_id": "call_f", "output": "/tmp"},
				map[string]any{"type": "custom_tool_call_output", "call_id": "call_c", "output": "done"})
			replay["input"] = input
			replayed, eErr := BuildRequest(route, "test", encodeValue(t, replay), nil)
			if eErr != nil {
				t.Fatal(eErr)
			}
			for _, name := range names {
				if bytes.Count(replayed, []byte(name)) < 2 {
					t.Fatalf("replayed name missing: %s", replayed)
				}
			}
			if !bytes.Contains(replayed, []byte("/tmp")) || !bytes.Contains(replayed, []byte("done")) {
				t.Fatalf("tool results lost: %s", replayed)
			}
			if route == catalog.RouteChatCompletions && !bytes.Contains(replayed, []byte("reasoning_content")) {
				t.Fatalf("GLM reasoning history missing: %s", replayed)
			}
		})
	}
}

func TestStreamToolRoundTrip(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		for _, fragmentSize := range []int{1, 17, 4096} {
			t.Run(fmt.Sprintf("%s/%d", route, fragmentSize), func(t *testing.T) {
				original := []byte(codexToolsRequest)
				upstream, eErr := BuildRequest(route, "test", original, nil)
				if eErr != nil {
					t.Fatal(eErr)
				}
				names := toolNames(t, route, upstream)
				args := []string{`{"command":"pwd"}`, string(encodeValue(t, map[string]any{"input": "*** Begin Patch\n*** End Patch"}))}
				var stream bytes.Buffer
				appendFrame := func(frame any) {
					stream.WriteString("data: ")
					stream.Write(encodeValue(t, frame))
					stream.WriteString("\n\n")
				}
				if route == catalog.RouteChatCompletions {
					for i, name := range names {
						appendFrame(map[string]any{"id": "r1", "model": "test", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{
							"tool_calls": []any{map[string]any{"index": i, "id": []string{"call_f", "call_c"}[i], "type": "function", "function": map[string]any{"name": name, "arguments": args[i][:4]}}},
						}}}})
						appendFrame(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
							"tool_calls": []any{map[string]any{"index": i, "function": map[string]any{"arguments": args[i][4:]}}},
						}}}})
					}
					appendFrame(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}})
					// Usage arrives after finish_reason, before [DONE].
					appendFrame(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 4, "total_tokens": 15}})
					stream.WriteString("data: [DONE]\n\n")
				} else {
					appendFrame(map[string]any{"type": "message_start", "message": map[string]any{"id": "r1", "model": "test", "usage": map[string]any{"input_tokens": 11}}})
					for i, name := range names {
						appendFrame(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": []string{"call_f", "call_c"}[i], "name": name, "input": map[string]any{}}})
						for _, part := range []string{args[i][:4], args[i][4:]} {
							appendFrame(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": part}})
						}
						appendFrame(map[string]any{"type": "content_block_stop", "index": i})
					}
					appendFrame(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 4}})
					appendFrame(map[string]any{"type": "message_stop"})
				}
				conv := New(route, "test", original, upstream)
				var result bytes.Buffer
				raw := stream.Bytes()
				done := false
				for len(raw) > 0 {
					n := min(fragmentSize, len(raw))
					events, terminal, eErr := conv.Feed(raw[:n])
					if eErr != nil {
						t.Fatal(eErr)
					}
					for _, event := range events {
						result.Write(event)
					}
					done = terminal
					raw = raw[n:]
				}
				if !done || len(conv.Flush()) != 0 {
					t.Fatal("terminal state lost or duplicated")
				}
				framer := shared.NewSSEFramer(false)
				framer.Push(result.Bytes())
				completed := 0
				for {
					event, data, _, ok := framer.Next()
					if !ok {
						break
					}
					if event != "response.completed" {
						continue
					}
					completed++
					response := decodeObject(t, []byte(data))["response"].(map[string]any)
					assertToolIdentity(t, response["output"].([]any))
					if response["usage"].(map[string]any)["total_tokens"] != float64(15) {
						t.Fatalf("late usage lost: %+v", response)
					}
				}
				if completed != 1 {
					t.Fatalf("completed events = %d: %s", completed, result.String())
				}
			})
		}
	}
}

func TestStreamErrorsAndFlush(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		t.Run(string(route), func(t *testing.T) {
			for _, frame := range []string{"data: {bad}\n\n", "data: {\"error\":{\"message\":\"failed\"}}\n\n"} {
				if _, _, eErr := New(route, "m", nil, nil).Feed([]byte(frame)); eErr == nil {
					t.Fatal("invalid stream accepted")
				}
			}
			conv := New(route, "m", nil, nil)
			if len(conv.Flush()) != 0 {
				t.Fatal("empty stream fabricated completion")
			}
			frame := "data: {\"id\":\"r\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"
			if route == catalog.RouteMessages {
				frame = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"r\"}}\n\n" +
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"
			}
			if _, done, eErr := conv.Feed([]byte(frame)); done || eErr != nil {
				t.Fatalf("premature terminal: %v %v", done, eErr)
			}
			var flushed strings.Builder
			for _, event := range conv.Flush() {
				flushed.Write(event)
			}
			if !strings.Contains(flushed.String(), "response.completed") || len(conv.Flush()) != 0 {
				t.Fatalf("EOF terminal missing or duplicated: %s", flushed.String())
			}
		})
	}
}

func TestNonStreamValidationAndClaudeText(t *testing.T) {
	for _, route := range []catalog.Route{catalog.RouteChatCompletions, catalog.RouteMessages} {
		for _, body := range []string{"{bad}", "{}", "null"} {
			if _, eErr := New(route, "m", nil, nil).ConvertNonStream(http.StatusOK, []byte(body)); eErr == nil || eErr.Class != errclass.ClassTranslation {
				t.Fatalf("invalid response accepted: %s", body)
			}
		}
		if _, eErr := New(route, "m", nil, nil).ConvertNonStream(http.StatusTooManyRequests, []byte("quota exhausted")); eErr == nil || eErr.Class != errclass.ClassQuota {
			t.Fatalf("classification = %v", eErr)
		}
	}
	body := []byte(`{"id":"r","content":[{"type":"thinking","thinking":"considering","signature":"sig"},{"type":"text","text":"hello"}],"stop_reason":"max_tokens","usage":{"input_tokens":3,"output_tokens":5}}`)
	out, eErr := New(catalog.RouteMessages, "m", nil, nil).ConvertNonStream(http.StatusOK, body)
	if eErr != nil {
		t.Fatal(eErr)
	}
	obj := decodeObject(t, out)
	if obj["status"] != "incomplete" || !bytes.Contains(out, []byte("hello")) || !bytes.Contains(out, []byte("considering")) {
		t.Fatalf("Claude blocks or stop reason lost: %s", out)
	}
}
