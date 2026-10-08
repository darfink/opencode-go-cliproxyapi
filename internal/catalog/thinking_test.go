package catalog

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"opencode-go-cliproxyapi/internal/config"
)

func TestVerifiedModelThinking(t *testing.T) {
	for _, tc := range []struct {
		id     string
		levels string
		zero   bool
	}{
		{"muse-spark-1.3-contributor", "minimal,low,medium,high,xhigh,max", false},
		{"grok-4.7", "minimal,low,medium,high,xhigh", false},
		{"deepseek-v4-pro", "none,minimal,low,medium,high,xhigh,max,ultra", true},
		{"deepseek-v4.1-flash", "none,minimal,low,medium,high,xhigh,max,ultra", true},
		{"glm-5.1", "low,medium,high,xhigh,max", false},
		{"glm-5.3", "low,medium,high,xhigh,max", false},
		{"glm-5.3-flash", "none,minimal,low,medium,high,xhigh,max", true},
		{"hy3", "none,minimal,low,medium,high,xhigh,max", true},
		{"hy4-preview", "none,minimal,low,medium,high,xhigh,max", true},
		{"kimi-k2.6", "none,minimal,low,medium,high,xhigh,max", true},
		{"kimi-k2.7-code", "minimal,low,medium,high,xhigh,max,ultra", false},
		{"kimi-k3", "none,minimal,low,medium,high,xhigh,max", true},
		{"longcat-2.0", "none,minimal,low,medium,high,xhigh,max,ultra", true},
		{"longcat-2.5-preview-free", "none,minimal,low,medium,high,xhigh,max,ultra", true},
		{"mimo-v2.6-flash", "none,low,medium,high", true},
		{"mimo-v2.6-pro", "none,low,medium,high", true},
		{"minimax-m2.5", "none,minimal,low,medium,high,xhigh,max", true},
		{"minimax-m3", "none,minimal,low,medium,high,xhigh,max", true},
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

func TestUpstreamThinkingOverridesVerifiedFallback(t *testing.T) {
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

func TestVerifiedThinkingOnlyOnAuditedRoutes(t *testing.T) {
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
			if got := findModel(t, m.Models(), "muse-spark-1.3-contributor").Thinking; got != nil {
				t.Fatalf("unaudited override inherited thinking: %+v", got)
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
