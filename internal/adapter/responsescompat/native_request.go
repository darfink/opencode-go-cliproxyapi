package responsescompat

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/errclass"
)

type nativeChatRequest struct {
	Tools []struct {
		Function map[string]json.RawMessage `json:"function"`
	} `json:"tools"`
	ToolChoice json.RawMessage `json:"tool_choice"`
	Messages   []struct {
		ToolCalls []shared.CCToolCall `json:"tool_calls"`
	} `json:"messages"`
}

// BuildNativeRequest normalizes tools and removes incompatible encrypted history.
// A full Responses -> Chat -> Responses round trip would also lose requested
// reasoning controls, media, and provider-specific fields these endpoints support.
func BuildNativeRequest(model string, body []byte) ([]byte, *errclass.Error) {
	rewritten, eErr := shared.RewriteModelID(model, body, "openai-response")
	if eErr != nil {
		return nil, eErr
	}
	var src map[string]json.RawMessage
	_ = json.Unmarshal(rewritten, &src)
	var input []json.RawMessage
	if shared.HasContent(src["input"]) {
		var text string
		if json.Unmarshal(src["input"], &text) == nil {
			// Grok's Responses endpoint requires an array, unlike OpenAI's API.
			input = []json.RawMessage{marshalRaw(map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": text},
			}})}
		} else if err := json.Unmarshal(src["input"], &input); err != nil {
			return nil, errclass.Translation("input must be a string or an array")
		}
	}
	var hosted []json.RawMessage
	collectHosted := func(raw json.RawMessage) *errclass.Error {
		if !shared.HasContent(raw) {
			return nil
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err != nil {
			return errclass.Translation("tools must be an array")
		}
		for _, tool := range tools {
			var def struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(tool, &def); err != nil {
				return errclass.Translation("malformed tool definition")
			}
			switch def.Type {
			case "", "function", "custom", "namespace":
				if _, eErr := filterTools(marshalRaw([]json.RawMessage{tool}), true); eErr != nil {
					return eErr
				}
			default:
				// Native Responses endpoints can execute hosted tools. Leave
				// those definitions intact instead of applying the Chat policy.
				if strings.HasPrefix(strings.ToLower(model), "muse-spark") &&
					(def.Type == "web_search" || strings.HasPrefix(def.Type, "web_search_preview")) {
					var search map[string]json.RawMessage
					_ = json.Unmarshal(tool, &search)
					// Muse rejects Codex's text/image selector even on preview
					// search tools. Keep hosted search and its other controls.
					if _, exists := search["search_content_types"]; exists {
						delete(search, "search_content_types")
						tool = marshalRaw(search)
					}
				}
				hosted = append(hosted, tool)
			}
		}
		return nil
	}
	if eErr := collectHosted(src["tools"]); eErr != nil {
		return nil, eErr
	}
	for _, raw := range input {
		var item struct {
			Type  string          `json:"type"`
			Tools json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, errclass.Translation("malformed input item")
		}
		if item.Type == "additional_tools" {
			if eErr := collectHosted(item.Tools); eErr != nil {
				return nil, eErr
			}
		}
	}
	var stream bool
	_ = json.Unmarshal(src["stream"], &stream)
	chatBody := translator.TranslateRequest(translator.FormatOpenAIResponse, translator.FormatOpenAI, model, body, stream)
	var chat nativeChatRequest
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		return nil, errclass.Translation("SDK produced an invalid tool request")
	}
	tools := make([]json.RawMessage, 0, len(chat.Tools)+len(hosted))
	for _, tool := range chat.Tools {
		tool.Function["type"] = json.RawMessage(`"function"`)
		tools = append(tools, marshalRaw(tool.Function))
	}
	tools = append(tools, hosted...)
	if len(tools) > 0 || src["tools"] != nil {
		src["tools"] = marshalRaw(tools)
	}
	// The SDK's Chat tool index resolves namespaces, custom tools, long names,
	// and collisions consistently for declarations, forced choice, and history.
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(chat.ToolChoice, &choice) == nil && choice.Type == "function" {
		src["tool_choice"] = marshalRaw(map[string]any{"type": "function", "name": choice.Function.Name})
	}
	calls := make(map[string]shared.CCToolCall)
	for _, message := range chat.Messages {
		for _, call := range message.ToolCalls {
			calls[call.ID] = call
		}
	}
	if input != nil {
		out := make([]json.RawMessage, 0, len(input))
		for _, raw := range input {
			var item map[string]json.RawMessage
			_ = json.Unmarshal(raw, &item)
			kind := rawString(item["type"])
			switch kind {
			case "additional_tools":
				continue
			case "compaction", "reasoning":
				// Non-GPT endpoints reject encrypted history, including blobs
				// created by another credential in a pooled account session.
				continue
			case "function_call", "custom_tool_call":
				id := rawString(item["call_id"])
				if id == "" {
					id = rawString(item["id"])
				}
				call, ok := calls[id]
				if !ok {
					return nil, errclass.Translation("SDK could not resolve a tool call in history")
				}
				item["type"] = json.RawMessage(`"function_call"`)
				item["name"] = marshalRaw(call.Function.Name)
				item["arguments"] = marshalRaw(shared.DefaultArgs(call.Function.Arguments))
				item["call_id"] = marshalRaw(call.ID)
				delete(item, "namespace")
				delete(item, "input")
				raw = marshalRaw(item)
			case "custom_tool_call_output":
				item["type"] = json.RawMessage(`"function_call_output"`)
				raw = marshalRaw(item)
			}
			out = append(out, raw)
		}
		src["input"] = marshalRaw(out)
	}
	return marshalRaw(src), nil
}

func marshalRaw(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
