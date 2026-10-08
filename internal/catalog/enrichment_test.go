package catalog

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/config"
)

func TestModelEnrichmentReasoningEfforts(t *testing.T) {
	for _, tc := range []struct {
		id     string
		levels string
		zero   bool
	}{
		{"muse-spark-1.3-contributor", "minimal,low,medium,high,xhigh", false},
		{"grok-4.7", "low,medium,high,xhigh", false},
		{"deepseek-v4-pro", "high,max", false},
		{"deepseek-v4.1-flash", "low,high,max", false},
		{"glm-5.1", "high", false},
		{"glm-5.3", "low,high,max", false},
		{"glm-5.3-flash", "low,high,max", false},
		{"hy3", "none,low,high", true},
		{"hy4-preview", "none,high", true},
		{"kimi-k2.6", "high", false},
		{"kimi-k2.7-code", "high", false},
		{"kimi-k3", "max", false},
		{"longcat-2.0", "none,high", true},
		{"longcat-2.5-preview-free", "none,high", true},
		{"mimo-v2.6-flash", "high", false},
		{"mimo-v2.6-pro", "high", false},
		{"minimax-m2.5", "high", false},
		{"minimax-m3", "none,high", true},
		{"qwen3.6-plus", "none,minimal,low,medium,high,xhigh", true},
		{"qwen3.7-max", "none,minimal,low,medium,high,xhigh,max", true},
		{"qwen3.8-flash", "none,minimal,low,medium,high,xhigh,max", true},
		{"qwen3.8-max", "none,minimal,low,medium,high,xhigh,max", true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			fc := &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
				Body: []byte(fmt.Sprintf(`{"data":[{"id":%q}]}`, tc.id))}}
			m := newManager(testCfg(), fc)
			mustRefresh(t, m)
			got := findModel(t, m.Models(), tc.id).Thinking
			if got == nil || strings.Join(got.Levels, ",") != tc.levels || got.ZeroAllowed != tc.zero || got.DynamicAllowed {
				t.Fatalf("thinking = %+v, want %s zero=%v", got, tc.levels, tc.zero)
			}
			// Every refresh must own its levels, not mutate the fallback table.
			got.Levels[0] = "mutated"
			mustRefresh(t, m)
			if levels := findModel(t, m.Models(), tc.id).Thinking.Levels; strings.Join(levels, ",") != tc.levels {
				t.Fatalf("fallback mutated across snapshots: %v", levels)
			}
		})
	}
}

func TestProviderThinkingOverridesBuiltinEnrichment(t *testing.T) {
	for _, raw := range []string{
		`{"levels":["low","max"],"min":1000,"max":64000,"zero_allowed":true,"dynamic_allowed":true}`,
		`{"min":1024}`,
		`{}`,
	} {
		t.Run(raw, func(t *testing.T) {
			fc := &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
				Body: []byte(`{"data":[{"id":"muse-spark-1.3-contributor","thinking":` + raw + `},
					{"id":"gpt-test","thinking":` + raw + `}]}`)}}
			m := newManager(testCfg(), fc)
			mustRefresh(t, m)
			models := m.Models()
			got := findModel(t, models, "muse-spark-1.3-contributor").Thinking
			want := findModel(t, models, "gpt-test").Thinking
			if got == nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("declared metadata replaced: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestBuiltinEnrichmentOnlyOnAuditedRoutes(t *testing.T) {
	for _, override := range []config.RouteOverride{
		{Protocol: "chat-completions", Endpoint: "/v1/chat/completions"},
		{Protocol: "responses", Endpoint: "/v1/custom-responses"},
	} {
		t.Run(override.Protocol+override.Endpoint, func(t *testing.T) {
			cfg := testCfg()
			cfg.RouteOverrides = map[string]config.RouteOverride{"muse-spark-1.3-contributor": override}
			m := newManager(cfg, &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
				Body: []byte(`{"data":[{"id":"muse-spark-1.3-contributor"}]}`)}})
			mustRefresh(t, m)
			if got := findModel(t, m.Models(), "muse-spark-1.3-contributor"); got.Thinking != nil || got.ContextLimit != 0 {
				t.Fatalf("unaudited override inherited enrichment: %+v", got)
			}
		})
	}
}

func TestUnknownVariantsKeepAbsentThinking(t *testing.T) {
	m := newManager(testCfg(), &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
		Body: []byte(`{"data":[{"id":"muse-spark-future"},{"id":"glm-future"},{"id":"minimax-future"}]}`)}})
	mustRefresh(t, m)
	for _, model := range m.Models() {
		if model.Thinking != nil {
			t.Fatalf("unknown or budget-based model inherited thinking: %+v", model)
		}
	}
}

func TestBuiltinReasoningPolicyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, metadata       string
		configured, override bool
		want                 ReasoningPolicy
	}{
		{"builtin", "", false, false, ReasoningAdaptiveToggle},
		{"provider", `,"thinking":{"levels":["low","high"]}`, false, false, ""},
		{"partial provider", `,"thinking":{"min":1024}`, false, false, ""},
		{"configured", "", true, false, ""},
		{"other endpoint", "", false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testCfg()
			if tc.configured {
				cfg.ModelEnrichments = map[string]config.ModelEnrichment{"minimax-m3": {ReasoningEfforts: []string{"low", "high"}}}
			}
			if tc.override {
				cfg.RouteOverrides = map[string]config.RouteOverride{"minimax-m3": {Protocol: "messages", Endpoint: "/v1/custom"}}
			}
			m := newManager(cfg, &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
				Body: []byte(`{"data":[{"id":"minimax-m3"` + tc.metadata + `}]}`)}})
			mustRefresh(t, m)
			if got := findModel(t, m.Models(), "minimax-m3").ReasoningPolicy; got != tc.want {
				t.Fatalf("policy = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfiguredEnrichmentOverridesProviderMetadata(t *testing.T) {
	cfg, err := config.Load([]byte(`api-keys: [{value: test-key}]
model-enrichments:
  muse-spark-1.3-contributor:
    reasoning-efforts: [high, ultra]
    context-window: 524288
    hosted-web-search: disabled
`))
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[
  {"id":"muse-spark-1.3-contributor","context_length":131072,"thinking":{"levels":["low"],"min":1024,"max":64000,"zero_allowed":true,"dynamic_allowed":true}},
  {"id":"grok-4.7","context_length":4096,"thinking":{"levels":["low"]}}]}`)}})
	mustRefresh(t, m)
	got := findModel(t, m.Models(), "muse-spark-1.3-contributor")
	if got.ContextLimit != 524_288 || got.HostedWebSearch != config.HostedWebSearchDisabled || got.Thinking.Min != 1024 || got.Thinking.Max != 64000 ||
		got.Thinking.ZeroAllowed || got.Thinking.DynamicAllowed || !reflect.DeepEqual(got.Thinking.Levels, []string{"high", "ultra"}) {
		t.Fatalf("configured enrichment = %+v, thinking %+v", got, got.Thinking)
	}
	other := findModel(t, m.Models(), "grok-4.7")
	if other.ContextLimit != 4096 || other.HostedWebSearch != config.HostedWebSearchEnabled || !reflect.DeepEqual(other.Thinking.Levels, []string{"low"}) {
		t.Fatalf("unrelated model changed: %+v", other)
	}
	got.Thinking.Levels[0] = "mutated"
	mustRefresh(t, m)
	if !reflect.DeepEqual(findModel(t, m.Models(), "muse-spark-1.3-contributor").Thinking.Levels, []string{"high", "ultra"}) {
		t.Fatal("snapshot mutated configured enrichment")
	}
}

func TestPartialEnrichmentInheritsAndDiscoveryRemainsDynamic(t *testing.T) {
	cfg, err := config.Load([]byte(`api-keys: [{value: test-key}]
model-enrichments:
  muse-spark-1.3-contributor:
    hosted-web-search: disabled
  gpt-future:
    reasoning-efforts: [none, max, auto]
    context-window: 131072
  gpt-not-discovered:
    context-window: 4096
`))
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"muse-spark-1.3-contributor"},{"id":"gpt-future"}]}`)}})
	mustRefresh(t, m)
	if len(m.Models()) != 2 {
		t.Fatal("enrichment added an undiscovered model")
	}
	muse := findModel(t, m.Models(), "muse-spark-1.3-contributor")
	if muse.ContextLimit != 1_048_576 || muse.HostedWebSearch != config.HostedWebSearchDisabled ||
		strings.Join(muse.Thinking.Levels, ",") != "minimal,low,medium,high,xhigh" {
		t.Fatalf("partial enrichment lost built-in fields: %+v", muse)
	}
	future := findModel(t, m.Models(), "gpt-future")
	if future.ContextLimit != 131_072 || future.HostedWebSearch != config.HostedWebSearchEnabled || !future.Thinking.ZeroAllowed || !future.Thinking.DynamicAllowed {
		t.Fatalf("unknown model enrichment = %+v", future)
	}
}
