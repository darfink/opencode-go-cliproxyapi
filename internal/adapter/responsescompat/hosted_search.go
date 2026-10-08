package responsescompat

import (
	"encoding/json"
	"strings"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/errclass"
)

// FilterHostedWebSearch enforces a resolved model policy on a Responses body.
// Only declarations change: client functions, history, and usage stay intact.
// Run after protocol translation so the policy also covers non-Responses clients.
func FilterHostedWebSearch(body []byte) ([]byte, *errclass.Error) {
	var src map[string]json.RawMessage
	if err := json.Unmarshal(body, &src); err != nil || src == nil {
		return nil, errclass.Translation("malformed openai-response request JSON")
	}
	var choice struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(src["tool_choice"], &choice)
	if isWebSearchTool(choice.Type) || isWebSearchTool(rawString(src["tool_choice"])) {
		return nil, &errclass.Error{Class: errclass.ClassUnsupported,
			Message: "hosted web search is disabled by model enrichment"}
	}
	removed, remaining := 0, 0
	filter := func(raw json.RawMessage) (json.RawMessage, *errclass.Error) {
		if !shared.HasContent(raw) {
			return raw, nil
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, errclass.Translation("tools must be an array")
		}
		out := make([]json.RawMessage, 0, len(tools))
		for _, tool := range tools {
			var def struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(tool, &def); err != nil {
				return nil, errclass.Translation("malformed tool definition")
			}
			if isWebSearchTool(def.Type) {
				removed++
				continue
			}
			out = append(out, tool)
		}
		remaining += len(out)
		return marshalRaw(out), nil
	}
	var eErr *errclass.Error
	if raw, present := src["tools"]; present {
		if src["tools"], eErr = filter(raw); eErr != nil {
			return nil, eErr
		}
	}
	// additional_tools can introduce new declarations on follow-up turns.
	if shared.HasContent(src["input"]) {
		var input []json.RawMessage
		if json.Unmarshal(src["input"], &input) == nil {
			for i, raw := range input {
				var item map[string]json.RawMessage
				_ = json.Unmarshal(raw, &item)
				if rawString(item["type"]) == "additional_tools" {
					if item["tools"], eErr = filter(item["tools"]); eErr != nil {
						return nil, eErr
					}
					input[i] = marshalRaw(item)
				}
			}
			src["input"] = marshalRaw(input)
		}
	}
	if removed == 0 {
		return body, nil
	}
	if remaining == 0 && rawString(src["tool_choice"]) == "required" {
		return nil, &errclass.Error{Class: errclass.ClassUnsupported,
			Message: "tool_choice requires a tool but hosted web search is disabled by model enrichment"}
	}
	return marshalRaw(src), nil
}

func isWebSearchTool(kind string) bool {
	return kind == "web_search" || strings.HasPrefix(kind, "web_search_preview") || strings.HasPrefix(kind, "web_search_20")
}
