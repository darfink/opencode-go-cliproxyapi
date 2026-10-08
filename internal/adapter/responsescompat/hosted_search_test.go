package responsescompat

import (
	"bytes"
	"reflect"
	"testing"

	"opencode-go-cliproxyapi/internal/errclass"
)

func TestHostedSearchFilterPreservesNativeFieldsAndAdditionalTools(t *testing.T) {
	for _, additional := range []bool{false, true} {
		t.Run(map[bool]string{false: "top-level", true: "additional"}[additional], func(t *testing.T) {
			body := []byte(`{"input":[{"type":"web_search_call","id":"ws","action":{"type":"search","query":"q"}}],"metadata":{"number":9007199254740993},"tools":[{"type":"web_search_2025_08_26"},{"type":"function","name":"web_search","parameters":{"type":"object"}},{"type":"file_search","vector_store_ids":["v"]}],"tool_choice":{"type":"function","name":"web_search"}}`)
			src := decodeObject(t, body)
			if additional {
				// Keep the integer in raw JSON; map round trips would round it.
				body = []byte(`{"input":[{"type":"web_search_call","id":"ws","action":{"type":"search","query":"q"}},{"type":"additional_tools","tools":[{"type":"web_search_2025_08_26"},{"type":"function","name":"web_search","parameters":{"type":"object"}},{"type":"file_search","vector_store_ids":["v"]}]}],"metadata":{"number":9007199254740993},"tool_choice":{"type":"function","name":"web_search"}}`)
			}
			out, eErr := FilterHostedWebSearch(body)
			if eErr != nil {
				t.Fatal(eErr)
			}
			got := decodeObject(t, out)
			tools := got["tools"]
			if additional {
				if _, exists := got["tools"]; exists {
					t.Fatal("filter introduced top-level tools")
				}
				tools = got["input"].([]any)[1].(map[string]any)["tools"]
			}
			if !reflect.DeepEqual(tools, src["tools"].([]any)[1:]) ||
				!reflect.DeepEqual(got["input"].([]any)[0], src["input"].([]any)[0]) ||
				!reflect.DeepEqual(got["tool_choice"], src["tool_choice"]) || !bytes.Contains(out, []byte("9007199254740993")) {
				t.Fatalf("native fields changed: %s", out)
			}
		})
	}
}

func TestHostedSearchFilterValidationAndNoop(t *testing.T) {
	for _, body := range []string{"null", "{broken", `{"tools":{}}`, `{"tools":[1]}`} {
		if _, eErr := FilterHostedWebSearch([]byte(body)); eErr == nil || eErr.Class != errclass.ClassTranslation {
			t.Fatalf("malformed body accepted: %s (%v)", body, eErr)
		}
	}
	for _, body := range []string{`{ "input": "hi" }`, `{"tools":[],"tool_choice":"required"}`, `{"tools":[{"type":"function","name":"web_search"}]}`} {
		out, eErr := FilterHostedWebSearch([]byte(body))
		if eErr != nil || string(out) != body {
			t.Fatalf("no-op changed body: %s (%v)", out, eErr)
		}
	}
}
