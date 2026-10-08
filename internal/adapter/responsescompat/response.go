package responsescompat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/catalog"
	"opencode-go-cliproxyapi/internal/errclass"
)

// Translator retains both request forms for the SDK's reverse tool index.
// Instances are request-scoped; streaming state is never shared across accounts.
type Translator struct {
	route              catalog.Route
	model              string
	original, upstream []byte
	param              any
	claudeParam        any
	chatRequest        []byte
	framer             *shared.SSEFramer
	done, finished     bool
}

func New(route catalog.Route, model string, original, upstream []byte) *Translator {
	chatRequest := upstream
	if route == catalog.RouteMessages {
		chatRequest = translator.TranslateRequest(translator.FormatOpenAIResponse, translator.FormatOpenAI, model, original, true)
	}
	return &Translator{route: route, model: model, original: original, upstream: upstream, chatRequest: chatRequest, framer: shared.NewSSEFramer(false)}
}

// ConvertNonStream accepts the Go endpoint's JSON responses, rather than the
// SSE aggregate expected by the SDK's official Claude non-stream translator.
func (t *Translator) ConvertNonStream(status int, body []byte) ([]byte, *errclass.Error) {
	if status >= http.StatusBadRequest {
		return nil, shared.UpstreamStatusError(status, body)
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil || response == nil {
		return nil, errclass.Translation("malformed upstream response JSON")
	}
	if t.route == catalog.RouteMessages {
		var eErr *errclass.Error
		body, eErr = claudeJSONToSSE(response)
		if eErr != nil {
			return nil, eErr
		}
		body = translator.TranslateNonStream(context.Background(), translator.FormatClaude, translator.FormatOpenAI,
			t.model, t.chatRequest, t.upstream, body, &t.claudeParam)
	} else {
		var choices []json.RawMessage
		if err := json.Unmarshal(response["choices"], &choices); err != nil || len(choices) == 0 {
			return nil, errclass.Translation("Chat Completions response carries no choices")
		}
	}
	// This SDK converter echoes its request argument as Responses metadata.
	// Supply the client form so tools and forced choices do not leak wire names.
	return translator.TranslateNonStream(context.Background(), translator.FormatOpenAI, translator.FormatOpenAIResponse,
		t.model, t.original, t.original, body, &t.param), nil
}

// claudeJSONToSSE adapts transport framing only. The SDK still owns Responses
// output ordering, namespace/custom identity restoration, reasoning, and usage.
func claudeJSONToSSE(response map[string]json.RawMessage) ([]byte, *errclass.Error) {
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(response["content"], &blocks); err != nil || blocks == nil {
		return nil, errclass.Translation("Messages response carries no content blocks")
	}
	var out bytes.Buffer
	appendEvent := func(event any) {
		raw, _ := json.Marshal(event)
		out.WriteString("data: ")
		out.Write(raw)
		out.WriteString("\n\n")
	}
	appendEvent(map[string]any{"type": "message_start", "message": response})
	for i, block := range blocks {
		var kind string
		_ = json.Unmarshal(block["type"], &kind)
		appendEvent(map[string]any{"type": "content_block_start", "index": i, "content_block": block})
		delta := map[string]any{}
		switch kind {
		case "text":
			delta["type"], delta["text"] = "text_delta", block["text"]
		case "tool_use":
			delta["type"], delta["partial_json"] = "input_json_delta", shared.DefaultArgs(string(block["input"]))
		case "thinking":
			delta["type"], delta["thinking"] = "thinking_delta", block["thinking"]
		case "redacted_thinking":
			// Chat Completions cannot carry encrypted Anthropic reasoning.
		default:
			return nil, unsupported("Messages output block", kind)
		}
		if len(delta) > 0 {
			appendEvent(map[string]any{"type": "content_block_delta", "index": i, "delta": delta})
		}
		appendEvent(map[string]any{"type": "content_block_stop", "index": i})
	}
	appendEvent(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": response["stop_reason"]}, "usage": response["usage"]})
	appendEvent(map[string]any{"type": "message_stop"})
	return out.Bytes(), nil
}

// Feed reassembles arbitrary network chunks into SDK data frames. Completion
// waits for the real terminal marker so late usage-only chunks are retained.
func (t *Translator) Feed(chunk []byte) (events [][]byte, done bool, eErr *errclass.Error) {
	if t.done {
		return nil, true, nil
	}
	t.framer.Push(chunk)
	for {
		event, data, _, ok := t.framer.Next()
		if !ok {
			return events, t.done, nil
		}
		if data == "" {
			continue
		}
		terminal := data == "[DONE]"
		if !terminal {
			var frame map[string]json.RawMessage
			if err := json.Unmarshal([]byte(data), &frame); err != nil || frame == nil {
				return nil, false, errclass.Translation("malformed upstream SSE JSON")
			}
			if shared.HasContent(frame["error"]) || event == "error" {
				return nil, false, shared.UpstreamStatusError(http.StatusBadGateway, []byte(data))
			}
			if t.route == catalog.RouteMessages {
				var kind string
				_ = json.Unmarshal(frame["type"], &kind)
				if kind == "" && event != "" {
					kind = event
					frame["type"], _ = json.Marshal(kind)
					raw, _ := json.Marshal(frame)
					data = string(raw)
				}
				terminal = kind == "message_stop"
				if kind == "message_delta" {
					var delta struct {
						StopReason string `json:"stop_reason"`
					}
					_ = json.Unmarshal(frame["delta"], &delta)
					t.finished = t.finished || delta.StopReason != ""
				}
			} else {
				var choices []struct {
					FinishReason string `json:"finish_reason"`
				}
				_ = json.Unmarshal(frame["choices"], &choices)
				for _, choice := range choices {
					t.finished = t.finished || choice.FinishReason != ""
				}
			}
		}
		events = append(events, t.translate([]byte("data: "+data), terminal)...)
		if terminal {
			t.done = true
			return events, true, nil
		}
	}
}

func (t *Translator) translate(frame []byte, terminal bool) [][]byte {
	frames := [][]byte{frame}
	if t.route == catalog.RouteMessages {
		frames = translator.TranslateStream(context.Background(), translator.FormatClaude, translator.FormatOpenAI,
			t.model, t.chatRequest, t.upstream, frame, &t.claudeParam)
		if terminal {
			frames = append(frames, []byte("[DONE]"))
		}
	}
	var events [][]byte
	for _, chunk := range frames {
		converted := translator.TranslateStream(context.Background(), translator.FormatOpenAI, translator.FormatOpenAIResponse,
			t.model, t.original, t.chatRequest, chunk, &t.param)
		for _, event := range converted {
			// SDK events omit the frame separator; the host normally adds it,
			// but plugin emissions must contain complete wire-ready SSE frames.
			events = append(events, append(bytes.TrimRight(event, "\r\n"), '\n', '\n'))
		}
	}
	return events
}

// Flush handles providers that close after finish_reason without a terminal
// marker. Do not invent a completed response for a truncated generation.
func (t *Translator) Flush() [][]byte {
	if t.done {
		return nil
	}
	if !t.finished {
		return nil
	}
	t.done = true
	if t.route == catalog.RouteMessages {
		return t.translate([]byte(`data: {"type":"message_stop"}`), true)
	}
	return t.translate([]byte("data: [DONE]"), true)
}
