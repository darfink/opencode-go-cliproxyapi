package catalog

import (
	"encoding/json"
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
			if got := findModel(t, m.Models(), "muse-spark-1.3-contributor"); got.Thinking != nil || got.ContextLimit != 0 ||
				got.InputLimit != 0 || got.OutputLimit != 0 || len(got.InputModes) != 0 || len(got.OutputModes) != 0 {
				t.Fatalf("unaudited override inherited enrichment: %+v", got)
			}
		})
	}
}

func TestBuiltinModelCapabilities(t *testing.T) {
	// Offline expectations from OpenCode's Go registry, not the live discovery
	// endpoint. Discovery can omit fields or models without changing these tests.
	cases := []struct {
		id                     string
		context, input, output int64
		image                  bool
	}{
		{"deepseek-v4-pro", 1_000_000, 0, 384_000, false},
		{"deepseek-v4.1-flash", 1_000_000, 0, 384_000, true},
		{"deepseek-v4-flash", 1_000_000, 0, 384_000, false},
		{"deepseek-v4-flash-vision-exp", 1_000_000, 0, 384_000, true},
		{"glm-5.2", 1_000_000, 0, 131_072, false},
		{"glm-5.3", 1_000_000, 0, 131_072, false},
		{"glm-5.3-flash", 1_000_000, 0, 131_072, true},
		{"grok-4.5", 500_000, 0, 500_000, true},
		{"grok-4.6", 500_000, 0, 500_000, true},
		{"grok-4.7", 500_000, 0, 500_000, true},
		{"gpt-5.6-luna", 1_050_000, 922_000, 128_000, true},
		{"gpt-6-luna", 1_050_000, 922_000, 128_000, true},
		{"claude-haiku-5-5", 1_000_000, 0, 128_000, true},
		{"hy3", 256_000, 192_000, 128_000, false},
		{"hy4-preview", 1_024_000, 0, 64_000, false},
		{"kimi-k2.6", 262_144, 0, 65_536, true},
		{"kimi-k2.7-code", 262_144, 0, 262_144, true},
		{"kimi-k3", 1_048_576, 0, 131_072, true},
		{"longcat-2.0", 1_000_000, 0, 131_072, false},
		{"longcat-2.5-preview-free", 1_000_000, 0, 131_072, true},
		{"mimo-v2.5", 1_000_000, 0, 128_000, true},
		{"mimo-v2.5-pro", 1_048_576, 0, 128_000, false},
		{"mimo-v2.6-pro", 1_048_576, 0, 131_072, true},
		{"mimo-v2.6-flash", 1_048_576, 0, 131_072, true},
		{"minimax-m2.7", 204_800, 0, 131_072, false},
		{"minimax-m3", 1_000_000, 0, 131_072, true},
		{"muse-spark-1.2-contributor", 1_048_576, 0, 131_072, true},
		{"muse-spark-1.3-contributor", 1_048_576, 0, 131_072, true},
		{"qwen3.6-plus", 1_000_000, 0, 65_536, true},
		{"qwen3.7-plus", 1_000_000, 0, 65_536, true},
		{"qwen3.7-max", 1_000_000, 0, 65_536, false},
		{"qwen3.8-flash", 1_000_000, 0, 131_072, true},
		{"qwen3.8-max", 1_000_000, 0, 131_072, true},
		{"space-bunny", 1_048_576, 524_288, 524_288, true},
		{"space-bunny-free", 1_048_576, 524_288, 524_288, true},
		{"step-5-preview-free", 1_000_000, 1_000_000, 65_536, true},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"data": []map[string]string{{"id": tc.id}}})
			if err != nil {
				t.Fatal(err)
			}
			m := newManager(testCfg(), &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200, Body: body}})
			mustRefresh(t, m)
			got := findModel(t, m.Models(), tc.id)
			modes := []string{"text"}
			if tc.image {
				modes = append(modes, "image")
			}
			if got.ContextLimit != tc.context || got.InputLimit != tc.input || got.OutputLimit != tc.output ||
				!reflect.DeepEqual(got.InputModes, modes) || !reflect.DeepEqual(got.OutputModes, []string{"text"}) {
				t.Fatalf("capabilities = %+v", got)
			}
			got.InputModes[0], got.OutputModes[0] = "mutated", "mutated"
			mustRefresh(t, m)
			if fresh := findModel(t, m.Models(), tc.id); !reflect.DeepEqual(fresh.InputModes, modes) || !reflect.DeepEqual(fresh.OutputModes, []string{"text"}) {
				t.Fatal("snapshot mutated built-in modalities")
			}
		})
	}
}

func TestCapabilityEnrichmentPrecedence(t *testing.T) {
	for _, spelling := range []string{"modes", "modalities"} {
		t.Run(spelling, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"data":[{"id":"mimo-v2.6-pro","context_length":8192,"max_input_tokens":4096,"max_output_tokens":2048,"input_%s":["text"],"output_%s":["audio"]}]}`, spelling, spelling))
			cfg := testCfg()
			fc := &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200, Body: body}}
			m := newManager(cfg, fc)
			mustRefresh(t, m)
			got := findModel(t, m.Models(), "mimo-v2.6-pro")
			if got.ContextLimit != 8192 || got.InputLimit != 4096 || got.OutputLimit != 2048 ||
				!reflect.DeepEqual(got.InputModes, []string{"text"}) || !reflect.DeepEqual(got.OutputModes, []string{"audio"}) {
				t.Fatalf("provider metadata replaced: %+v", got)
			}
			cfg, err := config.Load([]byte(`api-keys: [{value: test-key}]
model-enrichments:
  mimo-v2.6-pro:
    context-window: 131072
    input-token-limit: 65536
    max-output-tokens: 16384
    input-modalities: [text, image]
    output-modalities: [text]
`))
			if err != nil {
				t.Fatal(err)
			}
			configured := newManager(cfg, fc)
			configured.SeedFrom(m)
			got = findModel(t, configured.Models(), "mimo-v2.6-pro")
			if got.ContextLimit != 131_072 || got.InputLimit != 65_536 || got.OutputLimit != 16_384 ||
				!reflect.DeepEqual(got.InputModes, []string{"text", "image"}) || !reflect.DeepEqual(got.OutputModes, []string{"text"}) {
				t.Fatalf("configured metadata ignored during reconfigure: %+v", got)
			}
			got.InputModes[0] = "mutated"
			mustRefresh(t, configured)
			if findModel(t, configured.Models(), "mimo-v2.6-pro").InputModes[0] != "text" {
				t.Fatal("snapshot mutated configured modalities")
			}
		})
	}
}

func TestUnverifiedModelsKeepAbsentCapabilities(t *testing.T) {
	m := newManager(testCfg(), &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
		Body: []byte(`{"data":[{"id":"glm-5.1"},{"id":"minimax-m2.5"},{"id":"deepseek-flash"},{"id":"omen-alpha"},{"id":"mimo-future"}]}`)}})
	mustRefresh(t, m)
	for _, model := range m.Models() {
		if model.ContextLimit != 0 || model.InputLimit != 0 || model.OutputLimit != 0 || len(model.InputModes) != 0 || len(model.OutputModes) != 0 {
			t.Fatalf("unverified model inherited capabilities: %+v", model)
		}
	}
	if len(m.Models()) != 4 || len(m.Unsupported()) != 1 || m.Unsupported()[0].UpstreamID != "omen-alpha" {
		t.Fatal("unverified route was invented")
	}
}

func TestPartialCapabilityOverrideKeepsOtherMetadata(t *testing.T) {
	cfg, err := config.Load([]byte(`api-keys: [{value: test-key}]
model-enrichments:
  mimo-v2.6-pro:
    input-modalities: [text]
  hy3:
    max-output-tokens: 4096
`))
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, &fakeClient{resp: pluginapi.HTTPResponse{StatusCode: 200,
		Body: []byte(`{"data":[{"id":"mimo-v2.6-pro","max_output_tokens":8192},{"id":"hy3","context_length":128000}]}`)}})
	mustRefresh(t, m)
	mimo := findModel(t, m.Models(), "mimo-v2.6-pro")
	if mimo.ContextLimit != 1_048_576 || mimo.OutputLimit != 8192 || !reflect.DeepEqual(mimo.InputModes, []string{"text"}) ||
		!reflect.DeepEqual(mimo.OutputModes, []string{"text"}) || mimo.ReasoningPolicy != ReasoningFixed {
		t.Fatalf("partial modality override lost other fields: %+v", mimo)
	}
	hy := findModel(t, m.Models(), "hy3")
	if hy.ContextLimit != 128_000 || hy.InputLimit != 192_000 || hy.OutputLimit != 4096 || hy.ClientContextLimit() != 128_000 ||
		!reflect.DeepEqual(hy.InputModes, []string{"text"}) {
		t.Fatalf("partial output override lost other fields: %+v", hy)
	}
}

func TestClientContextLimit(t *testing.T) {
	for _, tc := range []struct{ context, input, want int64 }{
		{0, 0, 0}, {1_000_000, 0, 1_000_000}, {0, 192_000, 192_000},
		{256_000, 192_000, 192_000}, {128_000, 192_000, 128_000},
		{1_050_000, 922_000, 922_000}, {1_048_576, 524_288, 524_288},
	} {
		if got := (ModelRecord{ContextLimit: tc.context, InputLimit: tc.input}).ClientContextLimit(); got != tc.want {
			t.Fatalf("context=%d input=%d: got %d, want %d", tc.context, tc.input, got, tc.want)
		}
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
