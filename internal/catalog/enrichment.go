package catalog

import (
	"slices"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/config"
)

// ReasoningPolicy distinguishes real provider controls from accepted no-op efforts.
// The empty policy preserves named efforts and budget-based translation.
type ReasoningPolicy string

const (
	ReasoningFixed          ReasoningPolicy = "fixed"
	ReasoningToggle         ReasoningPolicy = "toggle"
	ReasoningAdaptiveToggle ReasoningPolicy = "adaptive-toggle"
)

// OpenCode Go omits thinking metadata from /models. Named efforts and toggles
// follow https://models.opencode.ai/api.json (2026-10-08), not HTTP acceptance.
// Messages budget tiers retain the route-specific successful budget conversions.
// Keep versioned IDs: a new variant or a different endpoint needs its own audit.
type modelEnrichment struct {
	route            Route
	reasoningEfforts []string
	reasoningPolicy  ReasoningPolicy
	contextWindow    int64
	inputLimit       int64
	outputLimit      int64
	inputModes       []string
	outputModes      []string
	hostedWebSearch  config.HostedWebSearchPolicy
}

// Built-in enrichment supplies missing metadata, not a static model catalog.
// An unlisted model remains discoverable and keeps its provider metadata.
// Limits follow the opencode-go provider in https://models.opencode.ai/api.json
// (2026-10-12). Only text/image inputs supported by our adapters are advertised;
// registry audio/video/PDF declarations are not proof of transport compatibility.
var modelEnrichments = map[string]modelEnrichment{
	"muse-spark-1.3-contributor": {
		route:            RouteResponses,
		reasoningEfforts: []string{"minimal", "low", "medium", "high", "xhigh"},
		// https://dev.meta.ai/docs/models lists a 1M window for this exact model.
		contextWindow:   1_048_576,
		outputLimit:     131_072,
		inputModes:      []string{"text", "image"},
		outputModes:     []string{"text"},
		hostedWebSearch: config.HostedWebSearchEnabled,
	},
	"grok-4.7":            {route: RouteResponses, reasoningEfforts: []string{"low", "medium", "high", "xhigh"}, contextWindow: 500_000, outputLimit: 500_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"deepseek-v4-pro":     {route: RouteChatCompletions, reasoningEfforts: []string{"high", "max"}, contextWindow: 1_000_000, outputLimit: 384_000, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"deepseek-v4.1-flash": {route: RouteChatCompletions, reasoningEfforts: []string{"low", "high", "max"}, contextWindow: 1_000_000, outputLimit: 384_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	// This retired model has no current declaration. Do not invent extra tiers.
	"glm-5.1":       {route: RouteChatCompletions, reasoningEfforts: []string{"high"}},
	"glm-5.3":       {route: RouteChatCompletions, reasoningEfforts: []string{"low", "high", "max"}, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"glm-5.3-flash": {route: RouteChatCompletions, reasoningEfforts: []string{"low", "high", "max"}, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"hy3":           {route: RouteChatCompletions, reasoningEfforts: []string{"none", "low", "high"}, contextWindow: 256_000, inputLimit: 192_000, outputLimit: 128_000, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"hy4-preview":   {route: RouteChatCompletions, reasoningEfforts: []string{"none", "high"}, contextWindow: 1_024_000, outputLimit: 64_000, inputModes: []string{"text"}, outputModes: []string{"text"}},
	// Codex catalogs need a nonempty effort list. A singleton is a fixed default,
	// not an adjustable tier; requests omit reasoning controls for these models.
	"kimi-k2.6":                {route: RouteChatCompletions, reasoningEfforts: []string{"high"}, reasoningPolicy: ReasoningFixed, contextWindow: 262_144, outputLimit: 65_536, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"kimi-k2.7-code":           {route: RouteChatCompletions, reasoningEfforts: []string{"high"}, reasoningPolicy: ReasoningFixed, contextWindow: 262_144, outputLimit: 262_144, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"kimi-k3":                  {route: RouteChatCompletions, reasoningEfforts: []string{"max"}, contextWindow: 1_048_576, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"longcat-2.0":              {route: RouteChatCompletions, reasoningEfforts: []string{"none", "high"}, reasoningPolicy: ReasoningToggle, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"longcat-2.5-preview-free": {route: RouteChatCompletions, reasoningEfforts: []string{"none", "high"}, reasoningPolicy: ReasoningToggle, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"mimo-v2.6-flash":          {route: RouteChatCompletions, reasoningEfforts: []string{"high"}, reasoningPolicy: ReasoningFixed, contextWindow: 1_048_576, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"mimo-v2.6-pro":            {route: RouteChatCompletions, reasoningEfforts: []string{"high"}, reasoningPolicy: ReasoningFixed, contextWindow: 1_048_576, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"minimax-m2.5":             {route: RouteMessages, reasoningEfforts: []string{"high"}, reasoningPolicy: ReasoningFixed},
	"minimax-m3":               {route: RouteMessages, reasoningEfforts: []string{"none", "high"}, reasoningPolicy: ReasoningAdaptiveToggle, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	// The 128000-token max conversion requires max_tokens=129024, which this
	// endpoint rejects. Smaller reasoning budgets, including xhigh, succeed.
	"qwen3.6-plus":  {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh"}, contextWindow: 1_000_000, outputLimit: 65_536, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"qwen3.7-max":   {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, contextWindow: 1_000_000, outputLimit: 65_536, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"qwen3.8-flash": {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"qwen3.8-max":   {route: RouteMessages, reasoningEfforts: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	// Remaining Go registry entries receive limits and text/image metadata only.
	// Reasoning controls need a separate route-specific audit, not a boolean flag.
	"deepseek-v4-flash":            {route: RouteChatCompletions, contextWindow: 1_000_000, outputLimit: 384_000, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"deepseek-v4-flash-vision-exp": {route: RouteChatCompletions, contextWindow: 1_000_000, outputLimit: 384_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"glm-5.2":                      {route: RouteChatCompletions, contextWindow: 1_000_000, outputLimit: 131_072, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"grok-4.5":                     {route: RouteResponses, contextWindow: 500_000, outputLimit: 500_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"grok-4.6":                     {route: RouteResponses, contextWindow: 500_000, outputLimit: 500_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"muse-spark-1.2-contributor":   {route: RouteResponses, contextWindow: 1_048_576, outputLimit: 131_072, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"gpt-5.6-luna":                 {route: RouteResponses, contextWindow: 1_050_000, inputLimit: 922_000, outputLimit: 128_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"gpt-6-luna":                   {route: RouteResponses, contextWindow: 1_050_000, inputLimit: 922_000, outputLimit: 128_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"claude-haiku-5-5":             {route: RouteMessages, contextWindow: 1_000_000, outputLimit: 128_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"mimo-v2.5":                    {route: RouteChatCompletions, contextWindow: 1_000_000, outputLimit: 128_000, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"mimo-v2.5-pro":                {route: RouteChatCompletions, contextWindow: 1_048_576, outputLimit: 128_000, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"minimax-m2.7":                 {route: RouteMessages, contextWindow: 204_800, outputLimit: 131_072, inputModes: []string{"text"}, outputModes: []string{"text"}},
	"space-bunny":                  {route: RouteChatCompletions, contextWindow: 1_048_576, inputLimit: 524_288, outputLimit: 524_288, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"space-bunny-free":             {route: RouteChatCompletions, contextWindow: 1_048_576, inputLimit: 524_288, outputLimit: 524_288, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"step-5-preview-free":          {route: RouteChatCompletions, contextWindow: 1_000_000, inputLimit: 1_000_000, outputLimit: 65_536, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
	"qwen3.7-plus":                 {route: RouteMessages, contextWindow: 1_000_000, outputLimit: 65_536, inputModes: []string{"text", "image"}, outputModes: []string{"text"}},
}

// enrichModel resolves metadata and request policy once for the shared snapshot.
// Configured fields override provider values; built-ins only fill missing data
// on the audited endpoint and never add models that discovery did not return.
func (m *Manager) enrichModel(e rawModel, rec *ModelRecord) {
	// Provider metadata beats built-in enrichment, even a partial thinking object.
	rec.Thinking = normalizeThinking(e.Thinking)
	rec.ContextLimit = e.ContextLength
	rec.InputLimit = e.MaxInputTokens
	rec.HostedWebSearch = config.HostedWebSearchEnabled
	if enrichment, ok := modelEnrichments[e.ID]; ok && rec.Protocol == enrichment.route && rec.EndpointPath == enrichment.route.EndpointPath() {
		if e.Thinking == nil && len(enrichment.reasoningEfforts) > 0 {
			// An explicit effort declaration also replaces the built-in wire policy.
			if m.cfg.ModelEnrichments[e.ID].ReasoningEfforts == nil {
				rec.ReasoningPolicy = enrichment.reasoningPolicy
			}
			rec.Thinking = &pluginapi.ThinkingSupport{
				Levels:      slices.Clone(enrichment.reasoningEfforts),
				ZeroAllowed: slices.Contains(enrichment.reasoningEfforts, "none"),
			}
		}
		if rec.ContextLimit == 0 {
			rec.ContextLimit = enrichment.contextWindow
		}
		if rec.InputLimit == 0 {
			rec.InputLimit = enrichment.inputLimit
		}
		if rec.OutputLimit == 0 {
			rec.OutputLimit = enrichment.outputLimit
		}
		if len(rec.InputModes) == 0 {
			rec.InputModes = slices.Clone(enrichment.inputModes)
		}
		if len(rec.OutputModes) == 0 {
			rec.OutputModes = slices.Clone(enrichment.outputModes)
		}
		if enrichment.hostedWebSearch != "" {
			rec.HostedWebSearch = enrichment.hostedWebSearch
		}
	}
	enrichment := m.cfg.ModelEnrichments[e.ID]
	if enrichment.ContextWindow != nil {
		rec.ContextLimit = *enrichment.ContextWindow
	}
	if enrichment.InputTokenLimit != nil {
		rec.InputLimit = *enrichment.InputTokenLimit
	}
	if enrichment.MaxOutputTokens != nil {
		rec.OutputLimit = *enrichment.MaxOutputTokens
	}
	if enrichment.InputModalities != nil {
		rec.InputModes = slices.Clone(enrichment.InputModalities)
	}
	if enrichment.OutputModalities != nil {
		rec.OutputModes = slices.Clone(enrichment.OutputModalities)
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

// ClientContextLimit bounds the context advertised to clients that ignore the
// separate input limit, including CLIProxyAPI's synthesized Codex catalog.
// ContextLimit retains the provider's combined input/output window internally.
func (r ModelRecord) ClientContextLimit() int64 {
	if r.InputLimit > 0 && (r.ContextLimit <= 0 || r.InputLimit < r.ContextLimit) {
		return r.InputLimit
	}
	return r.ContextLimit
}
