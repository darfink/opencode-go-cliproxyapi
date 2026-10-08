package responsescompat

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/errclass"
)

func nativeToolResponse(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"id": "native_response", "status": "completed", "model": "muse-spark-test", "provider_extra": map[string]any{"test": true},
		"output": []any{
			map[string]any{"type": "reasoning", "id": "rs", "encrypted_content": "opaque", "summary": []any{}},
			map[string]any{"type": "message", "id": "msg", "role": "assistant", "content": []any{map[string]any{
				"type": "output_text", "text": "hi", "annotations": []any{map[string]any{"type": "url_citation", "url": "https://example.test"}},
			}}},
			map[string]any{"type": "function_call", "id": "fc_native", "name": "functions__exec", "call_id": "call_f", "arguments": `{"command":"pwd"}`, "status": "completed", "provider_extra": "keep"},
			map[string]any{"type": "function_call", "id": "ct_native", "name": "editor__patch", "call_id": "call_c", "arguments": `{"input":"*** Begin Patch\n*** End Patch"}`, "status": "completed"},
		},
		"usage": map[string]any{"input_tokens": 11, "output_tokens": 4, "total_tokens": 15, "output_tokens_details": map[string]any{"reasoning_tokens": 2}},
	}
}

func TestNativeResponseRoundTripPreservesMetadata(t *testing.T) {
	original := []byte(codexToolsRequest)
	body := nativeToolResponse(t)
	out, eErr := NewNative("muse-spark-test", original).ConvertNonStream(200, encodeValue(t, body))
	if eErr != nil {
		t.Fatal(eErr)
	}
	response := decodeObject(t, out)
	output := response["output"].([]any)
	assertToolIdentity(t, output[2:])
	if !reflect.DeepEqual(output[:2], body["output"].([]any)[:2]) || output[2].(map[string]any)["id"] != "fc_native" ||
		output[2].(map[string]any)["status"] != "completed" || output[2].(map[string]any)["provider_extra"] != "keep" {
		t.Fatalf("native items changed beyond tool identity: %s", out)
	}
	for _, field := range []string{"id", "status", "model", "usage", "provider_extra"} {
		if !reflect.DeepEqual(response[field], decodeObject(t, encodeValue(t, body))[field]) {
			t.Fatalf("native field %s changed", field)
		}
	}

	replay := decodeObject(t, original)
	input := append(replay["input"].([]any), output...)
	input = append(input, map[string]any{"type": "function_call_output", "call_id": "call_f", "output": "/tmp"},
		map[string]any{"type": "custom_tool_call_output", "call_id": "call_c", "output": "done"})
	replay["input"] = input
	upstream, eErr := BuildNativeRequest("muse-spark-test", encodeValue(t, replay))
	if eErr != nil {
		t.Fatal(eErr)
	}
	// Return native reasoning to the client, but do not replay incompatible blobs.
	if bytes.Contains(upstream, []byte(`"encrypted_content"`)) || !bytes.Contains(upstream, []byte(`"name":"editor__patch"`)) ||
		!bytes.Contains(upstream, []byte(`"type":"function_call_output"`)) {
		t.Fatalf("native replay fields lost: %s", upstream)
	}
}

func TestNativeCustomInputWrappers(t *testing.T) {
	for _, key := range []string{"input", "code", "arguments", "cmd", "command"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", key, stream), func(t *testing.T) {
				item := map[string]any{"type": "function_call", "id": "ct", "call_id": "call_c", "name": "editor__patch",
					"arguments": string(encodeValue(t, map[string]any{key: "patch data"})), "status": "completed"}
				body := encodeValue(t, map[string]any{"output": []any{item}})
				conv := NewNative("muse-spark-test", []byte(codexToolsRequest))
				if !stream {
					out, eErr := conv.ConvertNonStream(200, body)
					if eErr != nil {
						t.Fatal(eErr)
					}
					got := decodeObject(t, out)["output"].([]any)[0].(map[string]any)
					if got["type"] != "custom_tool_call" || got["name"] != "patch" || got["namespace"] != "editor" || got["input"] != "patch data" {
						t.Fatalf("custom input wrapper leaked: %s", out)
					}
					return
				}
				frames := []map[string]any{
					{"type": "response.output_item.added", "output_index": 0, "item": item},
					{"type": "response.function_call_arguments.done", "output_index": 0, "item_id": "ct", "arguments": item["arguments"]},
					{"type": "response.completed", "response": decodeObject(t, body)},
				}
				for _, frame := range frames {
					events, _, eErr := conv.Feed([]byte("data: " + string(encodeValue(t, frame)) + "\n\n"))
					if eErr != nil || len(events) != 1 || !bytes.Contains(events[0], []byte(`"input":"patch data"`)) {
						t.Fatalf("stream custom input wrapper leaked: %s (%v)", events, eErr)
					}
				}
			})
		}
	}
}

func TestNativeStreamFragmentationAndCustomInput(t *testing.T) {
	for _, size := range []int{1, 23, 4096} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := nativeToolResponse(t)
			output := body["output"].([]any)
			var upstream bytes.Buffer
			appendEvent := func(kind string, event map[string]any) {
				event["type"] = kind
				upstream.WriteString("event: " + kind + "\ndata: ")
				upstream.Write(encodeValue(t, event))
				upstream.WriteString("\n\n")
			}
			for i, raw := range output[2:] {
				item := decodeObject(t, encodeValue(t, raw))
				args := item["arguments"]
				item["arguments"], item["status"] = "", "in_progress"
				appendEvent("response.output_item.added", map[string]any{"sequence_number": i*4 + 1, "output_index": i + 2, "item": item})
				appendEvent("response.function_call_arguments.delta", map[string]any{"sequence_number": i*4 + 2, "output_index": i + 2, "item_id": item["id"], "delta": args})
				appendEvent("response.function_call_arguments.done", map[string]any{"sequence_number": i*4 + 3, "output_index": i + 2, "item_id": item["id"], "arguments": args})
				item["arguments"], item["status"] = args, "completed"
				appendEvent("response.output_item.done", map[string]any{"sequence_number": i*4 + 4, "output_index": i + 2, "item": item})
			}
			appendEvent("response.completed", map[string]any{"sequence_number": 10, "response": body})
			conv := NewNative("muse-spark-test", []byte(codexToolsRequest))
			var downstream bytes.Buffer
			raw := upstream.Bytes()
			done := false
			for len(raw) > 0 {
				n := min(size, len(raw))
				events, terminal, eErr := conv.Feed(raw[:n])
				if eErr != nil {
					t.Fatal(eErr)
				}
				for _, event := range events {
					downstream.Write(event)
				}
				done = terminal
				raw = raw[n:]
			}
			if !done {
				t.Fatal("terminal event lost")
			}
			framer := shared.NewSSEFramer(false)
			framer.Push(downstream.Bytes())
			customDone, completed := 0, 0
			for {
				kind, data, _, ok := framer.Next()
				if !ok {
					break
				}
				event := decodeObject(t, []byte(data))
				switch kind {
				case "response.function_call_arguments.delta":
					if event["item_id"] == "ct_native" {
						t.Fatal("custom JSON wrapper leaked into function arguments")
					}
				case "response.custom_tool_call_input.done":
					customDone++
					if event["input"] != "*** Begin Patch\n*** End Patch" || event["item_id"] != "ct_native" || event["output_index"] != float64(3) || event["sequence_number"] != float64(7) {
						t.Fatalf("custom input event = %+v", event)
					}
				case "response.completed":
					completed++
					response := event["response"].(map[string]any)
					assertToolIdentity(t, response["output"].([]any)[2:])
					if !reflect.DeepEqual(response["usage"], decodeObject(t, encodeValue(t, body))["usage"]) {
						t.Fatal("native streaming usage changed")
					}
				}
			}
			if completed != 1 || customDone != 1 {
				t.Fatalf("terminal counts = %d %d", completed, customDone)
			}
		})
	}
}

func TestNativePassthroughAndErrors(t *testing.T) {
	body := []byte(`{ "id": "r", "output": [{"type":"reasoning","encrypted_content":"opaque"}], "usage": {"total_tokens":9007199254740993} }`)
	conv := NewNative("grok-test", []byte(`{"input":"hi"}`))
	out, eErr := conv.ConvertNonStream(200, body)
	if eErr != nil || !bytes.Equal(out, body) {
		t.Fatalf("no-tool native response changed: %s %v", out, eErr)
	}
	frame := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\n" +
		"data: \"delta\":\"hi\"}\n\n")
	events, done, eErr := conv.Feed(frame)
	if eErr != nil || done || len(events) != 1 || !bytes.Equal(events[0], frame) {
		t.Fatalf("unrelated multiline native event changed: %q %v", events, eErr)
	}
	for _, invalid := range []string{"{bad}", "null", `{"output":{}}`} {
		if _, eErr := conv.ConvertNonStream(200, []byte(invalid)); eErr == nil || eErr.Class != errclass.ClassTranslation {
			t.Fatalf("invalid response accepted: %s %v", invalid, eErr)
		}
	}
	if _, eErr := conv.ConvertNonStream(429, []byte("quota exhausted")); eErr == nil || eErr.Class != errclass.ClassQuota {
		t.Fatalf("status error = %v", eErr)
	}
	if _, _, eErr := conv.Feed([]byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"failed\"}}}\n\n")); eErr == nil || eErr.Class != errclass.ClassUpstream {
		t.Fatalf("native stream error = %v", eErr)
	}
}
