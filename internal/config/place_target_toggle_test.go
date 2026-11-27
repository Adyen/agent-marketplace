package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderPlaceTargetToggle(t *testing.T) {
	t.Run("missing list starts from baseline", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\n# retained\nsources: {}\n")
		got, changed, err := RenderPlaceTargetToggle("config.yaml", input, []string{"factory"}, "invented-app", true)
		if err != nil || !changed || !strings.Contains(string(got), "harnesses:\n  - factory\n  - invented-app") ||
			!strings.Contains(string(got), "# retained") {
			t.Fatalf("render enable = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("targets spelling permits explicit empty", func(t *testing.T) {
		got, changed, err := RenderPlaceTargetToggle("config.yaml", []byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app]\n"), nil, "invented-app", false)
		if err != nil || !changed || !strings.Contains(string(got), "targets: []") || strings.Contains(string(got), "harnesses:") {
			t.Fatalf("render final targets removal = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("legacy last target removal is refused", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\nharnesses: [invented-app]\n")
		got, changed, err := RenderPlaceTargetToggle("config.yaml", input, nil, "invented-app", false)
		if err == nil || changed || got != nil || !strings.Contains(err.Error(), "rename the key to targets:") {
			t.Fatalf("render legacy final removal = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("no-op preserves bytes", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\ntargets: [invented-app] # keep\n")
		got, changed, err := RenderPlaceTargetToggle("config.yaml", input, nil, "invented-app", true)
		if err != nil || changed || string(got) != string(input) {
			t.Fatalf("render no-op = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("unrelated duplicates and comments remain", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\ntargets:\n  # first duplicate\n  - factory\n  # second duplicate\n  - factory\n  - invented-app\n")
		got, changed, err := RenderPlaceTargetToggle("config.yaml", input, nil, "invented-app", false)
		if err != nil || !changed || strings.Count(string(got), "factory") != 2 ||
			strings.Index(string(got), "# first duplicate") > strings.Index(string(got), "# second duplicate") {
			t.Fatalf("render duplicate preservation = %q, changed=%v, err=%v", got, changed, err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(got, &doc); err != nil {
			t.Fatal(err)
		}
		_, sequence := findTopLevelSequence(doc.Content[0], "targets")
		if sequence == nil || len(sequence.Content) != 2 || sequence.Content[0].Value != "factory" ||
			sequence.Content[1].Value != "factory" {
			t.Fatalf("preserved target sequence = %#v", sequence)
		}
	})
	t.Run("both aliases are refused", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\ntargets: [factory]\nharnesses: [cursor]\n")
		if got, changed, err := RenderPlaceTargetToggle("config.yaml", input, nil, "invented-app", true); err == nil ||
			changed || got != nil || !strings.Contains(err.Error(), "targets and harnesses cannot both be declared") {
			t.Fatalf("render dual aliases = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("empty legacy list inherits baseline for enable and disable", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\nharnesses: [] # retained\n")
		enabled, changed, err := RenderPlaceTargetToggle("config.yaml", input, []string{"factory"}, "invented-app", true)
		if err != nil || !changed || !strings.Contains(string(enabled), "factory") ||
			!strings.Contains(string(enabled), "invented-app") || !strings.Contains(string(enabled), "# retained") {
			t.Fatalf("enable over empty legacy list = %q, changed=%v, err=%v", enabled, changed, err)
		}
		disabled, changed, err := RenderPlaceTargetToggle("config.yaml", input, []string{"factory", "invented-app"}, "invented-app", false)
		if err != nil || !changed || !strings.Contains(string(disabled), "harnesses:") ||
			!strings.Contains(string(disabled), "factory") || strings.Contains(string(disabled), "invented-app") ||
			!strings.Contains(string(disabled), "# retained") {
			t.Fatalf("disable inherited target from empty legacy list = %q, changed=%v, err=%v", disabled, changed, err)
		}
	})
	t.Run("empty targets list ignores baseline", func(t *testing.T) {
		input := []byte("apiVersion: agent-marketplace/v4\ntargets: []\n")
		got, changed, err := RenderPlaceTargetToggle("config.yaml", input, []string{"factory"}, "invented-app", true)
		if err != nil || !changed || strings.Contains(string(got), "factory") || !strings.Contains(string(got), "invented-app") {
			t.Fatalf("enable over explicit empty targets = %q, changed=%v, err=%v", got, changed, err)
		}
	})
	t.Run("compiled IDs are refused", func(t *testing.T) {
		if _, _, err := RenderPlaceTargetToggle("config.yaml", []byte("apiVersion: agent-marketplace/v4\n"), nil, "factory", true); err == nil {
			t.Fatal("compiled target was accepted by place target renderer")
		}
	})
}
