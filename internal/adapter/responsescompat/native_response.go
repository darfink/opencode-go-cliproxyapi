package responsescompat

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/errclass"
)

// NativeTranslator restores SDK tool identities without resynthesizing native
// Responses output. Native reasoning carriers, annotations, usage, IDs, sequence
// numbers, output indexes, and unknown fields retain their upstream values.
type NativeTranslator struct {
	model       string
	original    []byte
	chatRequest []byte
	framer      *shared.SSEFramer
	items       map[string]map[string]json.RawMessage
	done        bool
}

func NewNative(model string, original []byte) *NativeTranslator {
	chat := translator.TranslateRequest(translator.FormatOpenAIResponse, translator.FormatOpenAI, model, original, true)
	return &NativeTranslator{model: model, original: original, chatRequest: chat, framer: shared.NewSSEFramer(true),
		items: make(map[string]map[string]json.RawMessage)}
}

// restoreItem delegates namespace lookup and custom-input unwrapping to the
// SDK's reverse tool index. Only tool fields are copied back into the native item.
func (t *NativeTranslator) restoreItem(raw json.RawMessage) (json.RawMessage, *errclass.Error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil || item == nil {
		return nil, errclass.Translation("malformed Responses output item")
	}
	if rawString(item["type"]) != "function_call" {
		return raw, nil
	}
	call := shared.CCToolCall{ID: rawString(item["call_id"]), Type: "function"}
	call.Function.Name = rawString(item["name"])
	call.Function.Arguments = rawString(item["arguments"])
	body := marshalRaw(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"tool_calls": []shared.CCToolCall{call},
	}}}})
	var param any
	restored := translator.TranslateNonStream(context.Background(), translator.FormatOpenAI, translator.FormatOpenAIResponse,
		t.model, t.original, t.chatRequest, body, &param)
	var result struct {
		Output []map[string]json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(restored, &result); err != nil || len(result.Output) != 1 {
		return nil, errclass.Translation("SDK could not restore a native tool call")
	}
	identity := result.Output[0]
	for _, field := range []string{"type", "name", "namespace", "arguments", "input"} {
		delete(item, field)
		if value, ok := identity[field]; ok {
			item[field] = value
		}
	}
	if rawString(identity["type"]) == "custom_tool_call" {
		// Preserve upstream's alternate code/cmd/command wrappers. The SDK
		// resolves tool identity, but only unwraps the standard input property.
		item["input"] = marshalRaw(shared.UnwrapCustomToolInput(call.Function.Arguments))
	}
	return marshalRaw(item), nil
}

func (t *NativeTranslator) restoreResponse(raw []byte) ([]byte, *errclass.Error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil || response == nil {
		return nil, errclass.Translation("malformed Responses response JSON")
	}
	if !shared.HasContent(response["output"]) {
		return raw, nil
	}
	var output []json.RawMessage
	if err := json.Unmarshal(response["output"], &output); err != nil {
		return nil, errclass.Translation("Responses output must be an array")
	}
	changed := false
	for i, item := range output {
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(item, &head)
		if head.Type != "function_call" {
			continue
		}
		var eErr *errclass.Error
		output[i], eErr = t.restoreItem(item)
		if eErr != nil {
			return nil, eErr
		}
		changed = true
	}
	if !changed {
		return raw, nil
	}
	response["output"] = marshalRaw(output)
	return marshalRaw(response), nil
}

func (t *NativeTranslator) ConvertNonStream(status int, body []byte) ([]byte, *errclass.Error) {
	if status >= http.StatusBadRequest {
		return nil, shared.UpstreamStatusError(status, body)
	}
	return t.restoreResponse(body)
}

// Feed repairs tool events in place. Custom tools use a function wrapper on
// the wire; suppress its JSON deltas and emit the unwrapped input at .done,
// matching the SDK's own custom-tool streaming policy.
func (t *NativeTranslator) Feed(chunk []byte) (events [][]byte, done bool, eErr *errclass.Error) {
	if t.done {
		return nil, true, nil
	}
	t.framer.Push(chunk)
	for {
		event, data, raw, ok := t.framer.Next()
		if !ok {
			return events, t.done, nil
		}
		if data == "[DONE]" {
			t.done = true
			return append(events, raw), true, nil
		}
		if data == "" {
			events = append(events, raw)
			continue
		}
		var frame map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &frame); err != nil || frame == nil {
			return nil, false, errclass.Translation("malformed Responses SSE JSON")
		}
		kind := rawString(frame["type"])
		if kind == "" {
			kind = event
		}
		changed := false
		switch kind {
		case "error", "response.failed":
			return nil, false, shared.UpstreamStatusError(http.StatusBadGateway, []byte(data))
		case "response.output_item.added", "response.output_item.done":
			var item map[string]json.RawMessage
			if err := json.Unmarshal(frame["item"], &item); err != nil {
				return nil, false, errclass.Translation("malformed Responses tool event")
			}
			if rawString(item["type"]) == "function_call" {
				frame["item"], eErr = t.restoreItem(frame["item"])
				if eErr != nil {
					return nil, false, eErr
				}
				var restored map[string]json.RawMessage
				_ = json.Unmarshal(frame["item"], &restored)
				if rawString(restored["type"]) == "custom_tool_call" {
					t.items[rawString(item["id"])] = item
				}
				changed = true
			}
		case "response.function_call_arguments.delta":
			if _, custom := t.items[rawString(frame["item_id"])]; custom {
				continue
			}
		case "response.function_call_arguments.done":
			if item, custom := t.items[rawString(frame["item_id"])]; custom {
				item["arguments"] = frame["arguments"]
				var restored json.RawMessage
				restored, eErr = t.restoreItem(marshalRaw(item))
				if eErr != nil {
					return nil, false, eErr
				}
				var output map[string]json.RawMessage
				_ = json.Unmarshal(restored, &output)
				kind = "response.custom_tool_call_input.done"
				frame["type"], frame["input"] = marshalRaw(kind), output["input"]
				delete(frame, "arguments")
				changed = true
			}
		case "response.completed", "response.incomplete":
			frame["response"], eErr = t.restoreResponse(frame["response"])
			if eErr != nil {
				return nil, false, eErr
			}
			changed = true
			t.done = true
		}
		if changed {
			if event == "" {
				event = kind
			} else if kind == "response.custom_tool_call_input.done" {
				event = kind
			}
			raw = []byte("event: " + event + "\ndata: " + string(marshalRaw(frame)) + "\n\n")
		}
		events = append(events, raw)
		if t.done {
			return events, true, nil
		}
	}
}
