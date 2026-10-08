package responsescompat

import (
	"encoding/json"
	"testing"

	"opencode-go-cliproxyapi/internal/catalog"
)

const codexToolsRequest = `{
	"model":"opencode-go/test","input":[{"role":"user","content":"test tools"},
		{"type":"additional_tools","tools":[{"type":"namespace","name":"editor","tools":[
			{"type":"custom","name":"patch","description":"Apply a patch"}]}]}],
	"reasoning":{"effort":"high"},"stream":true,
	"tools":[{"type":"namespace","name":"functions","tools":[
		{"type":"function","name":"exec","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}]},
		{"type":"web_search"}]
}`

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func encodeValue(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func toolNames(t *testing.T, route catalog.Route, request []byte) []string {
	t.Helper()
	tools := decodeObject(t, request)["tools"].([]any)
	names := make([]string, len(tools))
	for i, raw := range tools {
		tool := raw.(map[string]any)
		if route == catalog.RouteChatCompletions {
			tool = tool["function"].(map[string]any)
		}
		names[i] = tool["name"].(string)
	}
	return names
}

func assertToolIdentity(t *testing.T, output []any) {
	t.Helper()
	if len(output) != 2 {
		t.Fatalf("output = %+v", output)
	}
	function := output[0].(map[string]any)
	custom := output[1].(map[string]any)
	if function["type"] != "function_call" || function["namespace"] != "functions" || function["name"] != "exec" ||
		function["call_id"] != "call_f" || function["arguments"] != `{"command":"pwd"}` {
		t.Fatalf("function identity lost: %+v", function)
	}
	if custom["type"] != "custom_tool_call" || custom["namespace"] != "editor" || custom["name"] != "patch" ||
		custom["call_id"] != "call_c" || custom["input"] != "*** Begin Patch\n*** End Patch" {
		t.Fatalf("custom identity lost: %+v", custom)
	}
}
