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
    input-token-limit: 922000
    max-output-tokens: 128000
    input-modalities: [text, image]
    output-modalities: [text]
    hosted-web-search: disabled
  another-model: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ModelEnrichments["custom-model"]
	if got.ContextWindow == nil || *got.ContextWindow != 1_048_576 || got.HostedWebSearch == nil || *got.HostedWebSearch != HostedWebSearchDisabled ||
		got.InputTokenLimit == nil || *got.InputTokenLimit != 922_000 || got.MaxOutputTokens == nil || *got.MaxOutputTokens != 128_000 ||
		!reflect.DeepEqual(got.InputModalities, []string{"text", "image"}) || !reflect.DeepEqual(got.OutputModalities, []string{"text"}) ||
		!reflect.DeepEqual(got.ReasoningEfforts, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "auto"}) {
		t.Fatalf("enrichment = %+v", got)
	}
	if got := cfg.ModelEnrichments["another-model"]; got.ContextWindow != nil || got.HostedWebSearch != nil || got.ReasoningEfforts != nil ||
		got.InputTokenLimit != nil || got.MaxOutputTokens != nil || got.InputModalities != nil || got.OutputModalities != nil {
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
		`grok-4.7: {input-token-limit: 0}`,
		`grok-4.7: {input-token-limit: -1}`,
		`grok-4.7: {input-token-limit: many}`,
		`grok-4.7: {max-output-tokens: 0}`,
		`grok-4.7: {max-output-tokens: -1}`,
		`grok-4.7: {max-output-tokens: many}`,
		`grok-4.7: {input-modalities: []}`,
		`grok-4.7: {input-modalities: [text, text]}`,
		`grok-4.7: {input-modalities: [Image]}`,
		`grok-4.7: {input-modalities: [bogus]}`,
		`grok-4.7: {input-modalities: text}`,
		`grok-4.7: {output-modalities: []}`,
		`grok-4.7: {output-modalities: [text, text]}`,
		`grok-4.7: {output-modalities: [bogus]}`,
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
