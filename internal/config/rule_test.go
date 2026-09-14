package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

func TestRuleSelectorsDecodeAndMergeAcrossConfigLayers(t *testing.T) {
	var global Config
	if err := yaml.Unmarshal([]byte(`
apiVersion: agent-marketplace/v3
harnesses: [factory, cursor]
registries:
  - url: rules.git
    ref: main
    rules:
      include: [secure-coding]
      exclude: [legacy-style]
rules:
  include: [team-style]
  exclude: [old-style]
`), &global); err != nil {
		t.Fatalf("decode global config: %v", err)
	}

	var repo Config
	if err := yaml.Unmarshal([]byte(`
apiVersion: agent-marketplace/v3
registries:
  - url: repo-rules.git
    ref: main
    rules: [repo-style]
rules:
  include: [repo-default]
`), &repo); err != nil {
		t.Fatalf("decode repo config: %v", err)
	}

	merged := Merge(&global, &repo)
	if len(merged.Registries) != 2 {
		t.Fatalf("merged registry count = %d, want 2", len(merged.Registries))
	}
	if got := merged.Registries[0].SelectorForKind(ir.KindRule); len(got.Include) != 1 || got.Include[0] != "secure-coding" {
		t.Fatalf("entry rule include = %#v, want secure-coding", got.Include)
	}
	if got := merged.Registries[1].SelectorForKind(ir.KindRule); len(got.Include) != 1 || got.Include[0] != "repo-style" {
		t.Fatalf("legacy entry rule include = %#v, want repo-style", got.Include)
	}
}

func TestRuleSelectorsDecodeInLocalConfig(t *testing.T) {
	var local LocalConfig
	if err := yaml.Unmarshal([]byte(`
rules:
  include: [local-style]
  exclude: [disabled-style]
`), &local); err != nil {
		t.Fatalf("decode local config: %v", err)
	}
	if got := local.SelectorForKind(ir.KindRule); len(got.Include) != 1 || got.Include[0] != "local-style" {
		t.Fatalf("local rule include = %#v, want local-style", got.Include)
	}
	if got := local.SelectorForKind(ir.KindRule); len(got.Exclude) != 1 || got.Exclude[0] != "disabled-style" {
		t.Fatalf("local rule exclude = %#v, want disabled-style", got.Exclude)
	}
}

func TestConfigAsLocalPreservesRuleSelector(t *testing.T) {
	want := KindSelector{Include: []string{"local-style"}, Exclude: []string{"disabled-style"}}
	local := configAsLocal(&Config{Rules: want})
	if local == nil || !reflect.DeepEqual(local.Rules, want) {
		t.Fatalf("configAsLocal rules = %#v, want %#v", local, want)
	}
}

func TestRemoveResourceFromConfigRemovesTopLevelLocalRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	if err := os.WriteFile(path, []byte("rules:\n  include: [secure-coding, other]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveResourceFromConfig(path, ir.KindRule, "secure-coding"); err != nil {
		t.Fatalf("RemoveResourceFromConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var local LocalConfig
	if err := yaml.Unmarshal(data, &local); err != nil {
		t.Fatalf("decode edited local config: %v", err)
	}
	if got := local.Rules.Include; len(got) != 1 || got[0] != "other" {
		t.Fatalf("local rule include = %#v, want [other]", got)
	}
}
