package responsescompat

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"opencode-go-cliproxyapi/internal/errclass"
)

func TestNativeRequestToolNormalization(t *testing.T) {
	src := decodeObject(t, []byte(codexToolsRequest))
	src["tool_choice"] = map[string]any{"type": "custom", "namespace": "editor", "name": "patch"}
	src["include"] = []any{"reasoning.encrypted_content"}
	src["service_tier"] = "priority"
	input := src["input"].([]any)
	input = append(input,
		map[string]any{"type": "reasoning", "id": "rs_1", "encrypted_content": "opaque", "summary": []any{}},
		map[string]any{"type": "compaction", "encrypted_content": "another-account-blob"},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "https://example.test/image.png"}}},
	)
	src["input"] = input
	original := encodeValue(t, src)
	out, eErr := BuildNativeRequest("muse-spark-test", original)
	if eErr != nil {
		t.Fatal(eErr)
	}
	got := decodeObject(t, out)
	tools := got["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools = %s", out)
	}
	for i, expectedName := range []string{"functions__exec", "editor__patch"} {
		tool := tools[i].(map[string]any)
		if tool["type"] != "function" || tool["name"] != expectedName || tool["description"] == nil {
			t.Fatalf("native function declaration = %+v", tool)
		}
	}
	if tools[2].(map[string]any)["type"] != "web_search" || got["tool_choice"].(map[string]any)["name"] != "editor__patch" {
		t.Fatalf("hosted tool or forced choice lost: %s", out)
	}
	if got["model"] != "muse-spark-test" || len(got["input"].([]any)) != 2 {
		t.Fatalf("envelope = %s", out)
	}
	for _, field := range []string{"stream", "reasoning", "include", "service_tier"} {
		if !reflect.DeepEqual(got[field], src[field]) {
			t.Fatalf("native field %s changed", field)
		}
	}
	if !reflect.DeepEqual(got["input"].([]any)[1:], input[4:]) {
		t.Fatal("native media inputs changed")
	}
}

func TestNativeRequestHistoryCompatibility(t *testing.T) {
	for _, model := range []string{"muse-spark-1.3-contributor", "grok-4.7"} {
		for _, tc := range []struct{ name, args, want string }{
			{"missing", "", "{}"},
			{"empty", `,"arguments":""`, "{}"},
			{"null", `,"arguments":null`, "{}"},
			{"whitespace", `,"arguments":"   "`, "{}"},
			{"valid", `,"arguments":"{\"n\":1}"`, `{"n":1}`},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				body := []byte(`{"reasoning":{"effort":"max"},"input":[
					{"type":"reasoning","encrypted_content":"opaque"},
					{"type":"compaction","encrypted_content":"pooled-blob"},
					{"type":"function_call","call_id":"f","name":"exec"` + tc.args + `},
					{"type":"function_call_output","call_id":"f","output":[{"type":"input_text","text":"ok"}]},
					{"type":"web_search_call","id":"ws","action":{"type":"search","query":"q"}}]}`)
				out, eErr := BuildNativeRequest(model, body)
				if eErr != nil {
					t.Fatal(eErr)
				}
				got := decodeObject(t, out)
				input := got["input"].([]any)
				if len(input) != 3 || bytes.Contains(out, []byte("encrypted_content")) || got["reasoning"].(map[string]any)["effort"] != "max" {
					t.Fatalf("native history compatibility = %s", out)
				}
				if input[0].(map[string]any)["arguments"] != tc.want {
					t.Fatalf("historical arguments = %s, want %s", out, tc.want)
				}
				src := decodeObject(t, body)["input"].([]any)
				if !reflect.DeepEqual(input[1:], src[3:]) {
					t.Fatalf("tool results or hosted history changed: %s", out)
				}
			})
		}
	}
}

func TestNativeRequestHistoryAndSDKCollisions(t *testing.T) {
	// These names have identical 64-byte tails. Reuse the SDK's collision
	// policy for both declarations and history, not an independent prefix rule.
	tail := strings.Repeat("long", 20)
	names := []string{"a" + tail, "b" + tail}
	request := map[string]any{
		"input": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"type": "function_call", "call_id": "f", "namespace": "n", "name": names[1], "arguments": `{"n":9007199254740993}`},
			map[string]any{"type": "function_call_output", "call_id": "f", "output": "ok"},
			map[string]any{"type": "custom_tool_call", "call_id": "c", "namespace": "editor", "name": "patch", "input": "patch data"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "c", "output": "applied"},
		},
		"tools": []any{
			map[string]any{"type": "namespace", "name": "n", "tools": []any{
				map[string]any{"type": "function", "name": names[0], "parameters": map[string]any{}},
				map[string]any{"type": "function", "name": names[1], "parameters": map[string]any{}},
			}},
			map[string]any{"type": "namespace", "name": "editor", "tools": []any{map[string]any{"type": "custom", "name": "patch"}}},
		},
	}
	original := encodeValue(t, request)
	out, eErr := BuildNativeRequest("grok-test", original)
	if eErr != nil {
		t.Fatal(eErr)
	}
	got := decodeObject(t, out)
	tools := got["tools"].([]any)
	first := tools[0].(map[string]any)["name"].(string)
	second := tools[1].(map[string]any)["name"].(string)
	if first == second || len(first) > 64 || len(second) > 64 {
		t.Fatalf("colliding names = %s %s", first, second)
	}
	input := got["input"].([]any)
	function := input[1].(map[string]any)
	custom := input[3].(map[string]any)
	if function["name"] != second || function["namespace"] != nil || function["arguments"] != `{"n":9007199254740993}` ||
		custom["type"] != "function_call" || custom["namespace"] != nil || custom["name"] != "editor__patch" || custom["arguments"] != `{"input":"patch data"}` ||
		input[4].(map[string]any)["type"] != "function_call_output" {
		t.Fatalf("native history = %s", out)
	}
	converted, eErr := NewNative("grok-test", original).ConvertNonStream(200, encodeValue(t, map[string]any{
		"output": []any{map[string]any{"type": "function_call", "name": second, "call_id": "f", "arguments": "{}"}},
	}))
	if eErr != nil {
		t.Fatal(eErr)
	}
	item := decodeObject(t, converted)["output"].([]any)[0].(map[string]any)
	if item["name"] != names[1] || item["namespace"] != "n" {
		t.Fatalf("long-name reverse mapping = %s", converted)
	}
}

func TestNativeRequestValidationAndPlainInput(t *testing.T) {
	for _, body := range []string{"null", "{broken", `{"input":{}}`, `{"tools":{}}`,
		`{"tools":[{"type":"namespace","name":"n","tools":[{"type":"function"}]}]}`} {
		if _, eErr := BuildNativeRequest("m", []byte(body)); eErr == nil || eErr.Class != errclass.ClassTranslation {
			t.Fatalf("invalid request accepted: %s (%v)", body, eErr)
		}
	}
	out, eErr := BuildNativeRequest("m", []byte(`{"model":"public","input":"hello","metadata":{"number":9007199254740993},"tools":[{"type":"file_search","vector_store_ids":["v"]}],"tool_choice":"auto"}`))
	if eErr != nil {
		t.Fatal(eErr)
	}
	var got struct {
		Input []map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(out, &got); err != nil || len(got.Input) != 1 || !bytes.Contains(out, []byte("9007199254740993")) ||
		!bytes.Contains(out, []byte("vector_store_ids")) || !bytes.Contains(out, []byte(`"tool_choice":"auto"`)) {
		t.Fatalf("plain native request fields lost: %s", out)
	}
}

func TestNativeRequestMuseSearchControls(t *testing.T) {
	for _, model := range []string{"muse-spark-1.3-contributor", "grok-4.7", "gpt-6.1-sol"} {
		for _, kind := range []string{"web_search", "web_search_preview", "web_search_preview_2025_03_11", "file_search"} {
			for _, additional := range []bool{false, true} {
				t.Run(model+"/"+kind+"/additional="+strconv.FormatBool(additional), func(t *testing.T) {
					tool := map[string]any{"type": kind, "search_content_types": []any{"text", "image"},
						"external_web_access": false, "filters": map[string]any{"allowed_domains": []any{"example.test"}}}
					src := map[string]any{"input": []any{}, "tools": []any{tool}}
					if additional {
						src["tools"] = []any{}
						src["input"] = []any{map[string]any{"type": "additional_tools", "tools": []any{tool}}}
					}
					out, eErr := BuildNativeRequest(model, encodeValue(t, src))
					if eErr != nil {
						t.Fatal(eErr)
					}
					got := decodeObject(t, out)["tools"].([]any)[0].(map[string]any)
					expected := make(map[string]any, len(tool))
					for key, value := range tool {
						expected[key] = value
					}
					if strings.HasPrefix(model, "muse-spark") && strings.HasPrefix(kind, "web_search") {
						delete(expected, "search_content_types")
					}
					if !reflect.DeepEqual(got, expected) {
						t.Fatalf("search controls = %+v, want %+v", got, expected)
					}
				})
			}
		}
	}
}
