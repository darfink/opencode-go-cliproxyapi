package catalog

import (
	"slices"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/config"
)

// OpenCode Go currently omits thinking metadata from /models. These exact
// model/route pairs accepted the listed efforts in live probes on 2026-10-07.
// Acceptance does not imply that every effort produces distinct compute.
// Keep versioned IDs: a new variant or a different endpoint needs its own audit.
type modelEnrichment struct {
	route            Route
	reasoningEfforts []string
	contextWindow    int64
	hostedWebSearch  config.HostedWebSearchPolicy
}

// Built-in enrichment supplies missing metadata, not a static model catalog.
// An unlisted model remains discoverable and keeps its provider metadata.
var modelEnrichments = map[string]modelEnrichment{
	"muse-spark-1.3-contributor": {
		route:            RouteResponses,
		reasoningEfforts: []string{"minimal", "low", "medium", "high", "xhigh", "max"},
		// https://dev.meta.ai/docs/models lists a 1M window for this exact model.
		contextWindow:   1_048_576,
		hostedWebSearch: config.HostedWebSearchEnabled,
	},
	"grok-4.7":                 {route: RouteResponses, reasoningEfforts: []string{"minimal", "low", "medium", "high", "xhigh"}},
	"deepseek-v4-pro":          {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"deepseek-v4.1-flash":      {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"glm-5.1":                  {route: RouteChatCompletions, reasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}},
	"glm-5.3":                  {route: RouteChatCompletions, reasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}},
	"glm-5.3-flash":            {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"hy3":                      {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"hy4-preview":              {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"kimi-k2.6":                {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"kimi-k2.7-code":           {route: RouteChatCompletions, reasoningEfforts: []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"kimi-k3":                  {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"longcat-2.0":              {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"longcat-2.5-preview-free": {route: RouteChatCompletions, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}},
	"mimo-v2.6-flash":          {route: RouteChatCompletions, reasoningEfforts: []string{"none", "low", "medium", "high"}},
	"mimo-v2.6-pro":            {route: RouteChatCompletions, reasoningEfforts: []string{"none", "low", "medium", "high"}},
	"minimax-m2.5":             {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"minimax-m3":               {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	// The 128000-token max conversion requires max_tokens=129024, which this
	// endpoint rejects. Smaller reasoning budgets, including xhigh, succeed.
	"qwen3.6-plus":  {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh"}},
	"qwen3.7-max":   {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"qwen3.8-flash": {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	"qwen3.8-max":   {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
}

// enrichModel resolves metadata and request policy once for the shared snapshot.
// Configured fields override provider values; built-ins only fill missing data
// on the audited endpoint and never add models that discovery did not return.
func (m *Manager) enrichModel(e rawModel, rec *ModelRecord) {
	// Provider metadata beats built-in enrichment, even a partial thinking object.
	rec.Thinking = normalizeThinking(e.Thinking)
	rec.ContextLimit = e.ContextLength
	rec.HostedWebSearch = config.HostedWebSearchEnabled
	if enrichment, ok := modelEnrichments[e.ID]; ok && rec.Protocol == enrichment.route && rec.EndpointPath == enrichment.route.EndpointPath() {
		if e.Thinking == nil && len(enrichment.reasoningEfforts) > 0 {
			rec.Thinking = &pluginapi.ThinkingSupport{
				Levels:      slices.Clone(enrichment.reasoningEfforts),
				ZeroAllowed: slices.Contains(enrichment.reasoningEfforts, "none"),
			}
		}
		if rec.ContextLimit == 0 {
			rec.ContextLimit = enrichment.contextWindow
		}
		if enrichment.hostedWebSearch != "" {
			rec.HostedWebSearch = enrichment.hostedWebSearch
		}
	}
	enrichment := m.cfg.ModelEnrichments[e.ID]
	if enrichment.ContextWindow != nil {
		rec.ContextLimit = *enrichment.ContextWindow
	}
	if enrichment.HostedWebSearch != nil {
		rec.HostedWebSearch = *enrichment.HostedWebSearch
	}
	if enrichment.ReasoningEfforts != nil {
		if rec.Thinking == nil {
			rec.Thinking = &pluginapi.ThinkingSupport{}
		}
		rec.Thinking.Levels = slices.Clone(enrichment.ReasoningEfforts)
		rec.Thinking.ZeroAllowed = slices.Contains(enrichment.ReasoningEfforts, "none")
		rec.Thinking.DynamicAllowed = slices.Contains(enrichment.ReasoningEfforts, "auto")
	}
}
