package catalog

import (
	"slices"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// OpenCode Go currently omits thinking metadata from /models. These exact
// model/route pairs accepted the listed efforts in live probes on 2026-10-07.
// Acceptance does not imply that every effort produces distinct compute.
// Keep versioned IDs: a new variant or a different endpoint needs its own audit.
var verifiedThinking = map[string]struct {
	route  Route
	levels []string
}{
	"muse-spark-1.3-contributor": {RouteResponses, []string{"minimal", "low", "medium", "high", "xhigh", "max"}},
	"grok-4.7":                   {RouteResponses, []string{"minimal", "low", "medium", "high", "xhigh"}},
	"deepseek-v4-pro":            {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"deepseek-v4.1-flash":        {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"glm-5.1":                    {RouteChatCompletions, []string{"low", "medium", "high", "xhigh", "max"}},
	"glm-5.3":                    {RouteChatCompletions, []string{"low", "medium", "high", "xhigh", "max"}},
	"glm-5.3-flash":              {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"hy3":                        {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"hy4-preview":                {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"kimi-k2.6":                  {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"kimi-k2.7-code":             {RouteChatCompletions, []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"kimi-k3":                    {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"longcat-2.0":                {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"longcat-2.5-preview-free":   {RouteChatCompletions, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"mimo-v2.6-flash":            {RouteChatCompletions, []string{"none", "low", "medium", "high"}},
	"mimo-v2.6-pro":              {RouteChatCompletions, []string{"none", "low", "medium", "high"}},
	"minimax-m2.5":               {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"minimax-m3":                 {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	// The 128000-token max conversion requires max_tokens=129024, which this
	// endpoint rejects. Smaller reasoning budgets, including xhigh, succeed.
	"qwen3.6-plus":  {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh"}},
	"qwen3.7-max":   {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"qwen3.8-flash": {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"qwen3.8-max":   {RouteMessages, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
}

func modelThinking(e rawModel, route Route, endpoint string) *pluginapi.ThinkingSupport {
	// Explicit upstream metadata remains authoritative, even a partial object.
	if e.Thinking != nil {
		return normalizeThinking(e.Thinking)
	}
	verified, ok := verifiedThinking[e.ID]
	if !ok || route != verified.route || endpoint != verified.route.EndpointPath() {
		return nil
	}
	return &pluginapi.ThinkingSupport{
		Levels:      slices.Clone(verified.levels),
		ZeroAllowed: slices.Contains(verified.levels, "none"),
	}
}
