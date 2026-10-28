package modelpolicy

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func BenchmarkNativeReasoning(b *testing.B) {
	for _, tc := range []struct {
		name, model, effort string
	}{
		{"supported", "nemotron-3.5-super", "high"},
		{"unsupported", "nemotron-3.5-super", "low"},
		{"qualified", "nvidia/nemotron-3.5-super", "high"},
		{"unknown", "future-native-model", "future"},
		{"other-catalog", "gpt-6.1-terra", "high"},
		{"opaque-selector", "sonnet", "high"},
		{"family-supported", "anthropic/sonnet", "high"},
		{"family-unsupported", "anthropic/sonnet", "max"},
		{"family-omitted", "anthropic/sonnet", ""},
		{"omitted", "nemotron-3.5-super", ""},
	} {
		b.Run(tc.name, func(b *testing.B) {
			if err := ValidateCatalogs(); err != nil {
				b.Fatal(err)
			}
			req := Request{Source: target.TargetFactory, Target: target.TargetFactory, Model: tc.model, Effort: tc.effort}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Resolve(req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
