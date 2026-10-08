package config

import (
	"reflect"
	"testing"
)

func TestLoadModelEnrichmentFields(t *testing.T) {
	cfg, err := Load([]byte(withKey + `
model-enrichments:
  custom-model:
    reasoning-efforts: [none, minimal, low, medium, high, xhigh, max, ultra, auto]
    context-window: 1048576
    hosted-web-search: disabled
  another-model: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ModelEnrichments["custom-model"]
	if got.ContextWindow == nil || *got.ContextWindow != 1_048_576 || got.HostedWebSearch == nil || *got.HostedWebSearch != HostedWebSearchDisabled ||
		!reflect.DeepEqual(got.ReasoningEfforts, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "auto"}) {
		t.Fatalf("enrichment = %+v", got)
	}
	if got := cfg.ModelEnrichments["another-model"]; got.ContextWindow != nil || got.HostedWebSearch != nil || got.ReasoningEfforts != nil {
		t.Fatalf("omitted fields did not inherit: %+v", got)
	}
}

func TestInvalidModelEnrichments(t *testing.T) {
	for _, fragment := range []string{
		`"": {}`,
		`" grok-4.7 ": {}`,
		`grok-4.7: {context-window: 0}`,
		`grok-4.7: {context-window: -1}`,
		`grok-4.7: {context-window: many}`,
		`grok-4.7: {hosted-web-search: ""}`,
		`grok-4.7: {hosted-web-search: preserve}`,
		`grok-4.7: {hosted-web-search: true}`,
		`grok-4.7: {reasoning-efforts: []}`,
		`grok-4.7: {reasoning-efforts: [auto]}`,
		`grok-4.7: {reasoning-efforts: [high, high]}`,
		`grok-4.7: {reasoning-efforts: [bogus]}`,
		`grok-4.7: {reasoning-efforts: [High]}`,
		`grok-4.7: {reasoning-efforts: high}`,
		`grok-4.7: invalid`,
	} {
		t.Run(fragment, func(t *testing.T) {
			if _, err := Load([]byte(withKey + "\nmodel-enrichments:\n  " + fragment + "\n")); err == nil {
				t.Fatal("invalid model enrichment accepted")
			}
		})
	}
}
