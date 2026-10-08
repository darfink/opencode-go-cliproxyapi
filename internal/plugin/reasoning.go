package plugin

import (
	"encoding/json"
	"slices"
	"strings"

	"opencode-go-cliproxyapi/internal/catalog"
	"opencode-go-cliproxyapi/internal/errclass"
	"opencode-go-cliproxyapi/internal/thinking"
)

// prepareModelReasoning removes synthetic budget controls before translation.
// This avoids inflating max_tokens for models with only a toggle or fixed mode.
func prepareModelReasoning(policy catalog.ReasoningPolicy, format string, body []byte) ([]byte, string, *errclass.Error) {
	if policy == "" {
		return body, "", nil
	}
	var src map[string]json.RawMessage
	if err := json.Unmarshal(body, &src); err != nil || src == nil {
		return nil, "", errclass.Translation("malformed reasoning request JSON")
	}
	var kind string
	if policy != catalog.ReasoningFixed {
		var effort string
		var err error
		if format == "openai-response" || format == "claude" {
			field := "reasoning"
			if format == "claude" {
				field = "output_config"
			}
			var reasoning struct {
				Effort string `json:"effort"`
			}
			if raw := src[field]; len(raw) > 0 {
				err = json.Unmarshal(raw, &reasoning)
			}
			effort = reasoning.Effort
		} else if raw := src["reasoning_effort"]; len(raw) > 0 {
			err = json.Unmarshal(raw, &effort)
		}
		if err != nil {
			return nil, "", errclass.Translation("malformed reasoning control")
		}
		effort = strings.ToLower(strings.TrimSpace(effort))
		// Native Messages callers can explicitly select enabled or adaptive.
		// In that format, a disabled thinking mode takes precedence over effort.
		if raw := src["thinking"]; len(raw) > 0 && (effort == "" || format == "claude") {
			var control struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &control); err != nil {
				return nil, "", errclass.Translation("malformed thinking toggle")
			}
			switch control.Type {
			case "", "enabled", "disabled":
				kind = control.Type
			case "adaptive":
				kind = "enabled"
				if policy == catalog.ReasoningAdaptiveToggle {
					kind = "adaptive"
				}
			default:
				return nil, "", &errclass.Error{Class: errclass.ClassUnsupported, Message: "unsupported thinking toggle type"}
			}
		}
		if effort != "" && kind == "" {
			if !slices.Contains(thinking.CanonicalLevels, effort) && effort != "auto" {
				return nil, "", &errclass.Error{Class: errclass.ClassUnsupported, Message: "unsupported thinking toggle effort"}
			}
			kind = "enabled"
			if policy == catalog.ReasoningAdaptiveToggle {
				kind = "adaptive"
			}
			if effort == "none" {
				kind = "disabled"
			}
		}
	}
	if eErr := removeModelReasoning(src); eErr != nil {
		return nil, "", eErr
	}
	result, _ := json.Marshal(src)
	return result, kind, nil
}

// applyModelReasoning uses the real provider toggle, never a fictitious budget.
// Raw messages preserve unrelated fields and integers across this final rewrite.
func applyModelReasoning(policy catalog.ReasoningPolicy, kind string, body []byte) ([]byte, *errclass.Error) {
	if policy == "" {
		return body, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil || out == nil {
		return nil, errclass.Translation("malformed translated reasoning request JSON")
	}
	if eErr := removeModelReasoning(out); eErr != nil {
		return nil, eErr
	}
	if kind != "" && policy != catalog.ReasoningFixed {
		out["thinking"], _ = json.Marshal(map[string]string{"type": kind})
	}
	result, _ := json.Marshal(out)
	return result, nil
}

func removeModelReasoning(body map[string]json.RawMessage) *errclass.Error {
	delete(body, "reasoning_effort")
	delete(body, "thinking")
	for _, field := range []string{"reasoning", "output_config"} {
		raw, ok := body[field]
		if !ok || string(raw) == "null" {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return errclass.Translation("malformed reasoning control")
		}
		delete(object, "effort")
		if len(object) == 0 {
			delete(body, field)
		} else {
			body[field], _ = json.Marshal(object)
		}
	}
	return nil
}
