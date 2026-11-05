package modelpolicy

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var devinCapturedUIDs = []struct {
	uid, identity, effort string
}{
	{"adaptive", "devin/adaptive", ""},
	{"swe-2-high", "cognition/swe-2", "high"},
	{"swe-2-medium", "cognition/swe-2", "medium"},
	{"swe-2-max", "cognition/swe-2", "max"},
	{"claude-fable-5-1-medium", "anthropic/claude-fable-5.1", "medium"},
	{"claude-fable-5-1-low", "anthropic/claude-fable-5.1", "low"},
	{"claude-fable-5-1-high", "anthropic/claude-fable-5.1", "high"},
	{"claude-fable-5-1-xhigh", "anthropic/claude-fable-5.1", "xhigh"},
	{"claude-fable-5-1-max", "anthropic/claude-fable-5.1", "max"},
	{"claude-opus-5-5-medium", "anthropic/claude-opus-5-5", "medium"},
	{"claude-opus-5-5-low", "anthropic/claude-opus-5-5", "low"},
	{"claude-opus-5-5-high", "anthropic/claude-opus-5-5", "high"},
	{"claude-opus-5-5-xhigh", "anthropic/claude-opus-5-5", "xhigh"},
	{"claude-opus-5-5-max", "anthropic/claude-opus-5-5", "max"},
	{"claude-opus-5-5-low-fast", "anthropic/claude-opus-5-5-fast", "low"},
	{"claude-opus-5-5-medium-fast", "anthropic/claude-opus-5-5-fast", "medium"},
	{"claude-opus-5-5-high-fast", "anthropic/claude-opus-5-5-fast", "high"},
	{"claude-opus-5-5-xhigh-fast", "anthropic/claude-opus-5-5-fast", "xhigh"},
	{"claude-opus-5-5-max-fast", "anthropic/claude-opus-5-5-fast", "max"},
	{"gpt-6-astra-medium", "openai/gpt-6-astra", "medium"},
	{"gpt-6-astra-low", "openai/gpt-6-astra", "low"},
	{"gpt-6-astra-high", "openai/gpt-6-astra", "high"},
	{"gpt-6-astra-xhigh", "openai/gpt-6-astra", "xhigh"},
	{"gpt-6-astra-max", "openai/gpt-6-astra", "max"},
	{"gpt-6-sol-medium", "openai/gpt-6-sol", "medium"},
	{"gpt-6-sol-none", "openai/gpt-6-sol", "none"},
	{"gpt-6-sol-low", "openai/gpt-6-sol", "low"},
	{"gpt-6-sol-high", "openai/gpt-6-sol", "high"},
	{"gpt-6-sol-xhigh", "openai/gpt-6-sol", "xhigh"},
	{"gpt-6-sol-max", "openai/gpt-6-sol", "max"},
	{"gpt-6-sol-none-priority", "openai/gpt-6-sol-fast", "none"},
	{"gpt-6-sol-low-priority", "openai/gpt-6-sol-fast", "low"},
	{"gpt-6-sol-medium-priority", "openai/gpt-6-sol-fast", "medium"},
	{"gpt-6-sol-high-priority", "openai/gpt-6-sol-fast", "high"},
	{"gpt-6-sol-xhigh-priority", "openai/gpt-6-sol-fast", "xhigh"},
	{"gpt-6-sol-max-priority", "openai/gpt-6-sol-fast", "max"},
	{"gpt-6-luna-medium", "openai/gpt-6-luna", "medium"},
	{"gpt-6-luna-none", "openai/gpt-6-luna", "none"},
	{"gpt-6-luna-low", "openai/gpt-6-luna", "low"},
	{"gpt-6-luna-high", "openai/gpt-6-luna", "high"},
	{"gpt-6-luna-xhigh", "openai/gpt-6-luna", "xhigh"},
	{"gpt-6-luna-max", "openai/gpt-6-luna", "max"},
	{"gpt-6-luna-none-priority", "openai/gpt-6-luna-fast", "none"},
	{"gpt-6-luna-low-priority", "openai/gpt-6-luna-fast", "low"},
	{"gpt-6-luna-medium-priority", "openai/gpt-6-luna-fast", "medium"},
	{"gpt-6-luna-high-priority", "openai/gpt-6-luna-fast", "high"},
	{"gpt-6-luna-xhigh-priority", "openai/gpt-6-luna-fast", "xhigh"},
	{"gpt-6-luna-max-priority", "openai/gpt-6-luna-fast", "max"},
	{"kimi-k3-high", "moonshot/kimi-k3", "high"},
	{"kimi-k3-low", "moonshot/kimi-k3", "low"},
	{"kimi-k3-max", "moonshot/kimi-k3", "max"},
	{"glm-5-3-low", "zai/glm-5.3", "low"},
	{"glm-5-3-high", "zai/glm-5.3", "high"},
	{"glm-5-3-max", "zai/glm-5.3", "max"},
	{"claude-sonnet-5-medium", "anthropic/claude-sonnet-5", "medium"},
	{"claude-sonnet-5-low", "anthropic/claude-sonnet-5", "low"},
	{"claude-sonnet-5-high", "anthropic/claude-sonnet-5", "high"},
	{"gemini-3-8-flash-medium", "google/gemini-3.8-flash", "medium"},
	{"gemini-3-8-flash-low", "google/gemini-3.8-flash", "low"},
	{"gemini-3-8-flash-high", "google/gemini-3.8-flash", "high"},
	{"claude-opus-4-8-medium", "anthropic/claude-opus-4-8", "medium"},
	{"claude-opus-4-8-low", "anthropic/claude-opus-4-8", "low"},
	{"claude-opus-4-8-high", "anthropic/claude-opus-4-8", "high"},
	{"claude-opus-4-8-xhigh", "anthropic/claude-opus-4-8", "xhigh"},
	{"claude-opus-5-medium", "anthropic/claude-opus-5", "medium"},
	{"claude-opus-5-low", "anthropic/claude-opus-5", "low"},
	{"claude-opus-5-high", "anthropic/claude-opus-5", "high"},
	{"claude-opus-5-xhigh", "anthropic/claude-opus-5", "xhigh"},
	{"claude-opus-5-max", "anthropic/claude-opus-5", "max"},
	{"gpt-5-6-sol-low", "openai/gpt-5.6-sol", "low"},
	{"gpt-5-6-sol-medium", "openai/gpt-5.6-sol", "medium"},
	{"gpt-5-6-sol-high", "openai/gpt-5.6-sol", "high"},
	{"gpt-5-6-sol-xhigh", "openai/gpt-5.6-sol", "xhigh"},
	{"gpt-5-6-sol-max", "openai/gpt-5.6-sol", "max"},
	{"gpt-5-6-terra-low", "openai/gpt-5.6-terra", "low"},
	{"gpt-5-6-terra-medium", "openai/gpt-5.6-terra", "medium"},
	{"gpt-5-6-terra-high", "openai/gpt-5.6-terra", "high"},
	{"gpt-5-6-terra-xhigh", "openai/gpt-5.6-terra", "xhigh"},
	{"gpt-5-6-terra-max", "openai/gpt-5.6-terra", "max"},
	{"gpt-5-6-luna-low", "openai/gpt-5.6-luna", "low"},
	{"gpt-5-6-luna-medium", "openai/gpt-5.6-luna", "medium"},
	{"gpt-5-6-luna-high", "openai/gpt-5.6-luna", "high"},
	{"gpt-5-6-luna-xhigh", "openai/gpt-5.6-luna", "xhigh"},
	{"gpt-5-6-luna-max", "openai/gpt-5.6-luna", "max"},
	{"glm-5-3-flash-low", "zai/glm-5.3-flash", "low"},
	{"glm-5-3-flash-high", "zai/glm-5.3-flash", "high"},
	{"glm-5-3-flash-max", "zai/glm-5.3-flash", "max"},
	{"swe-1-7", "cognition/swe-1.7", "max"},
	{"swe-1-7-medium", "cognition/swe-1.7", "medium"},
	{"fusion-gpt-5-6-sol-high-sidekick-swe-2-medium", "devin/fusion-gpt-5-6-sol-high-sidekick-swe-2-medium", ""},
	{"fusion-claude-fable-5-1-medium-sidekick-gpt-5-6-luna-high", "devin/fusion-claude-fable-5-1-medium-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-gpt-5-6-sol-high-sidekick-gpt-5-6-luna-high", "devin/fusion-gpt-5-6-sol-high-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-claude-opus-5-high-sidekick-gpt-5-6-luna-high", "devin/fusion-claude-opus-5-high-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-gpt-5-6-sol-medium-sidekick-gpt-5-6-luna-high", "devin/fusion-gpt-5-6-sol-medium-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-claude-opus-5-medium-sidekick-gpt-5-6-luna-high", "devin/fusion-claude-opus-5-medium-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-gpt-6-astra-medium-sidekick-gpt-5-6-luna-high", "devin/fusion-gpt-6-astra-medium-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-gpt-5-6-sol-low-sidekick-gpt-5-6-luna-high", "devin/fusion-gpt-5-6-sol-low-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-claude-opus-5-low-sidekick-gpt-5-6-luna-high", "devin/fusion-claude-opus-5-low-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-claude-fable-5-1-low-sidekick-gpt-5-6-luna-high", "devin/fusion-claude-fable-5-1-low-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-gpt-6-astra-low-sidekick-gpt-5-6-luna-high", "devin/fusion-gpt-6-astra-low-sidekick-gpt-5-6-luna-high", ""},
	{"fusion-claude-fable-5-1-medium-sidekick-gpt-5-6-sol-high", "devin/fusion-claude-fable-5-1-medium-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-claude-fable-5-1-high-sidekick-gpt-5-6-sol-high", "devin/fusion-claude-fable-5-1-high-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-claude-opus-5-high-sidekick-gpt-5-6-sol-high", "devin/fusion-claude-opus-5-high-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-gpt-6-astra-high-sidekick-gpt-5-6-sol-high", "devin/fusion-gpt-6-astra-high-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-claude-opus-5-medium-sidekick-gpt-5-6-sol-high", "devin/fusion-claude-opus-5-medium-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-gpt-6-astra-medium-sidekick-gpt-5-6-sol-high", "devin/fusion-gpt-6-astra-medium-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-claude-fable-5-1-low-sidekick-gpt-5-6-sol-high", "devin/fusion-claude-fable-5-1-low-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-gpt-6-astra-low-sidekick-gpt-5-6-sol-high", "devin/fusion-gpt-6-astra-low-sidekick-gpt-5-6-sol-high", ""},
	{"fusion-claude-fable-5-1-medium-sidekick-swe-2-high", "devin/fusion-claude-fable-5-1-medium-sidekick-swe-2-high", ""},
	{"fusion-claude-fable-5-1-high-sidekick-swe-2-high", "devin/fusion-claude-fable-5-1-high-sidekick-swe-2-high", ""},
	{"fusion-claude-opus-5-high-sidekick-swe-2-high", "devin/fusion-claude-opus-5-high-sidekick-swe-2-high", ""},
	{"fusion-gpt-6-astra-high-sidekick-swe-2-high", "devin/fusion-gpt-6-astra-high-sidekick-swe-2-high", ""},
	{"fusion-gpt-5-6-sol-medium-sidekick-swe-2-high", "devin/fusion-gpt-5-6-sol-medium-sidekick-swe-2-high", ""},
	{"fusion-claude-opus-5-medium-sidekick-swe-2-high", "devin/fusion-claude-opus-5-medium-sidekick-swe-2-high", ""},
	{"fusion-gpt-6-astra-medium-sidekick-swe-2-high", "devin/fusion-gpt-6-astra-medium-sidekick-swe-2-high", ""},
	{"fusion-gpt-5-6-sol-low-sidekick-swe-2-high", "devin/fusion-gpt-5-6-sol-low-sidekick-swe-2-high", ""},
	{"fusion-claude-opus-5-low-sidekick-swe-2-high", "devin/fusion-claude-opus-5-low-sidekick-swe-2-high", ""},
	{"fusion-claude-fable-5-1-low-sidekick-swe-2-high", "devin/fusion-claude-fable-5-1-low-sidekick-swe-2-high", ""},
	{"fusion-gpt-6-astra-low-sidekick-swe-2-high", "devin/fusion-gpt-6-astra-low-sidekick-swe-2-high", ""},
}

var devinCapturedSelectors = []struct {
	spelling, identity string
}{
	{"Adaptive", "devin/adaptive"},
	{"swe-1.7", "cognition/swe-1.7"},
	{"swe-2", "cognition/swe-2"},
	{"swe", "cognition/swe"},
	{"fable", "anthropic/fable"},
	{"claude-fable-5-1", "anthropic/claude-fable-5.1"},
	{"claude-fable-5.1", "anthropic/claude-fable-5.1"},
	{"claude-opus-5-5", "anthropic/claude-opus-5-5"},
	{"claude-opus-5.5", "anthropic/claude-opus-5-5"},
	{"claude-opus-4.8", "anthropic/claude-opus-4-8"},
	{"claude-opus-5", "anthropic/claude-opus-5"},
	{"claude-sonnet-5", "anthropic/claude-sonnet-5"},
	{"claude", "anthropic/sonnet"},
	{"sonnet", "anthropic/sonnet"},
	{"opus", "anthropic/opus"},
	{"haiku", "anthropic/haiku"},
	{"gpt-6-astra", "openai/gpt-6-astra"},
	{"gpt-6-sol", "openai/gpt-6-sol"},
	{"gpt-6-luna", "openai/gpt-6-luna"},
	{"gpt-5.6-sol", "openai/gpt-5.6-sol"},
	{"gpt-5.6-terra", "openai/gpt-5.6-terra"},
	{"gpt-5.6-luna", "openai/gpt-5.6-luna"},
	{"gpt", "openai/gpt"},
	{"codex", "openai/codex"},
	{"kimi-k3", "moonshot/kimi-k3"},
	{"glm-5-3", "zai/glm-5.3"},
	{"glm-5.3", "zai/glm-5.3"},
	{"glm-5-3-flash", "zai/glm-5.3-flash"},
	{"glm-5.3-flash", "zai/glm-5.3-flash"},
	{"gemini-3-8-flash", "google/gemini-3.8-flash"},
	{"gemini-3.8-flash", "google/gemini-3.8-flash"},
	{"gemini", "google/gemini"},
	{"fusion", "devin/fusion"},
}

func TestDevinCatalogCapturedInventory(t *testing.T) {
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	c := r.catalogs[target.TargetDevin]
	for _, tc := range devinCapturedUIDs {
		selected, ok := c.native[Encoding{Model: tc.uid}]
		if !ok {
			t.Errorf("captured Devin model_uid %q missing from catalog", tc.uid)
			continue
		}
		if selected.Identity != tc.identity || selected.Effort != tc.effort {
			t.Errorf("Devin uid %q decodes to %+v, want identity %q effort %q", tc.uid, selected, tc.identity, tc.effort)
		}
	}
	for _, tc := range devinCapturedSelectors {
		selected, ok := c.native[Encoding{Model: tc.spelling}]
		if !ok || selected.Identity != tc.identity || selected.Effort != "" {
			t.Errorf("Devin selector %q decodes to %+v (ok=%v), want %q", tc.spelling, selected, ok, tc.identity)
		}
	}
}
