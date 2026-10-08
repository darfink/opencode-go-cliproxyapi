// Package responsescompat uses the host SDK's Responses tool identity mapping
// for non-native Go routes. Original declarations must survive reverse
// translation so flattened namespaces and custom tools remain callable by Codex.
package responsescompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	_ "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator/builtin"

	"opencode-go-cliproxyapi/internal/adapter/shared"
	"opencode-go-cliproxyapi/internal/catalog"
	"opencode-go-cliproxyapi/internal/errclass"
	"opencode-go-cliproxyapi/internal/thinking"
)

// BuildRequest validates unsupported inputs before SDK translation, whose API
// cannot report conversion errors. Only hosted search declarations are omitted:
// Go's Chat/Messages endpoints cannot execute those OpenAI-hosted tools.
func BuildRequest(route catalog.Route, model string, body []byte, ts *pluginapi.ThinkingSupport) ([]byte, *errclass.Error) {
	var src map[string]json.RawMessage
	if err := json.Unmarshal(body, &src); err != nil || src == nil {
		return nil, errclass.Translation("malformed openai-response request JSON")
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if shared.HasContent(src["reasoning"]) {
		if err := json.Unmarshal(src["reasoning"], &reasoning); err != nil {
			return nil, errclass.Translation("malformed reasoning control")
		}
	}
	// Named routes pass efforts through; Messages must map them to token budgets.
	if route == catalog.RouteMessages && reasoning.Effort != "" {
		if eErr := thinking.ValidateEffort(reasoning.Effort, ts); eErr != nil {
			return nil, eErr
		}
	}
	if raw, ok := src["tools"]; ok {
		filtered, eErr := filterTools(raw, true)
		if eErr != nil {
			return nil, eErr
		}
		src["tools"] = filtered
	}
	if choice := src["tool_choice"]; shared.HasContent(choice) {
		var kind string
		if json.Unmarshal(choice, &kind) != nil {
			var tool struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(choice, &tool); err != nil {
				return nil, errclass.Translation("malformed tool_choice")
			}
			kind = tool.Type
		}
		switch kind {
		case "auto", "none", "required", "function", "custom":
		default:
			return nil, unsupported("tool_choice", kind)
		}
	}
	if shared.HasContent(src["input"]) {
		var text string
		if json.Unmarshal(src["input"], &text) != nil {
			var items []map[string]json.RawMessage
			if err := json.Unmarshal(src["input"], &items); err != nil {
				return nil, errclass.Translation("input must be a string or an array")
			}
			for _, item := range items {
				var kind, role string
				_ = json.Unmarshal(item["type"], &kind)
				_ = json.Unmarshal(item["role"], &role)
				switch kind {
				case "", "message":
					switch role {
					case "user", "assistant", "system", "developer":
					default:
						return nil, shared.ValidateRole(role, "Responses compatibility")
					}
					// The SDK's Chat translator downgrades developer to user.
					// Keep operator instructions authoritative on both Go routes.
					if role == "developer" {
						item["role"] = json.RawMessage(`"system"`)
					}
					parts, eErr := shared.DecodeStringOrParts(item["content"], "Responses compatibility")
					if eErr != nil {
						return nil, eErr
					}
					for _, part := range parts {
						if part.ImageURL != "" && (role == "system" || role == "developer") {
							return nil, shared.SystemImageRejected()
						}
					}
				case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "reasoning":
				case "additional_tools":
					filtered, eErr := filterTools(item["tools"], true)
					if eErr != nil {
						return nil, eErr
					}
					item["tools"] = filtered
				default:
					return nil, shared.UnsupportedInputItemType(kind)
				}
			}
			src["input"], _ = json.Marshal(items)
		}
	}
	filtered, _ := json.Marshal(src)
	var stream bool
	_ = json.Unmarshal(src["stream"], &stream)
	// Use the Chat tool index for both routes: the SDK's direct Responses-to-
	// Claude converter truncates long names without disambiguating collisions.
	translated := translator.TranslateRequest(translator.FormatOpenAIResponse, translator.FormatOpenAI, model, filtered, stream)
	if route == catalog.RouteMessages {
		translated = translator.TranslateRequest(translator.FormatOpenAI, translator.FormatClaude, model, translated, stream)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(translated, &out); err != nil || out == nil {
		return nil, errclass.Translation("SDK produced an invalid request")
	}
	// SDK defaults target official Claude models. Go routes instead keep this
	// plugin's capability-aware budgets and sampling policy, including off/auto.
	for _, field := range []string{"temperature", "top_p"} {
		if raw, ok := src[field]; ok {
			out[field] = raw
		}
	}
	if route == catalog.RouteMessages {
		delete(out, "thinking")
		delete(out, "output_config")
		var maxTokens int64
		_ = json.Unmarshal(src["max_output_tokens"], &maxTokens)
		maxTokens = shared.ClaudeMaxTokens(maxTokens)
		if reasoning.Effort != "" {
			budget, ok := thinking.BudgetFromEffort(reasoning.Effort, ts)
			if !ok {
				return nil, &errclass.Error{Class: errclass.ClassUnsupported,
					Message: fmt.Sprintf("reasoning_effort %q has no supported Messages token budget", reasoning.Effort)}
			}
			if budget > 0 {
				out["thinking"], _ = json.Marshal(shared.ClaudeThinking{Type: "enabled", BudgetTokens: budget})
				delete(out, "temperature")
				delete(out, "top_p")
				if maxTokens <= budget {
					maxTokens = budget + 1024
				}
			}
		}
		out["max_tokens"], _ = json.Marshal(maxTokens)
	} else {
		if raw, ok := src["parallel_tool_calls"]; ok {
			out["parallel_tool_calls"] = raw
		}
		if reasoning.Effort != "" {
			out["reasoning_effort"], _ = json.Marshal(strings.ToLower(strings.TrimSpace(reasoning.Effort)))
		}
		if stream {
			out["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		}
	}
	result, _ := json.Marshal(out)
	return result, nil
}

func unsupported(field, kind string) *errclass.Error {
	return &errclass.Error{Class: errclass.ClassUnsupported, Message: fmt.Sprintf("unsupported %s type %q on this Go route", field, kind)}
}

func filterTools(raw json.RawMessage, namespaces bool) (json.RawMessage, *errclass.Error) {
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, errclass.Translation("tools must be an array")
	}
	out := make([]map[string]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		var kind, name string
		_ = json.Unmarshal(tool["type"], &kind)
		_ = json.Unmarshal(tool["name"], &name)
		switch kind {
		case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
			continue
		case "namespace":
			if !namespaces {
				return nil, unsupported("nested tool", kind)
			}
			children, eErr := filterTools(tool["tools"], false)
			if eErr != nil {
				return nil, eErr
			}
			tool["tools"] = children
		case "", "function", "custom":
		default:
			return nil, unsupported("tool", kind)
		}
		if strings.TrimSpace(name) == "" {
			return nil, errclass.Translation("tool name must not be empty")
		}
		out = append(out, tool)
	}
	result, _ := json.Marshal(out)
	return result, nil
}
