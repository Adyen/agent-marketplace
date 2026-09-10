package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4ChildExclusionThenRootIncludeResolvesResource(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), []byte(`sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {exclude: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: fragment.yaml
sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {include: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	setCatalogKeys(cfg)
	key := cfg.Registries[0].CatalogKey
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{key: {{Kind: ir.KindSkill, Name: "review"}}}, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors = %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Name != "review" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestV4RequiredOnlyDoesNotSelectCatalogResources(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.test/agents.git, ref: main}}
resources: {shared: {required: true}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	setCatalogKeys(cfg)
	key := cfg.Registries[0].CatalogKey
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{key: {{Kind: ir.KindSkill, Name: "review"}}}, nil)
	if len(errs) != 0 {
		t.Fatalf("resolve errors = %v", errs)
	}
	if len(resolved) != 0 {
		t.Fatalf("required-only unexpectedly selected resources: %#v", resolved)
	}
}

func TestV4AllSelectionIsAdditiveAndHonorsExclusions(t *testing.T) {
	tests := []struct {
		name      string
		group     string
		wantNames []string
	}{
		{
			name:      "all plus include",
			group:     "include: [skill/keep]\n    all: true",
			wantNames: []string{"blocked", "keep", "other", "platform"},
		},
		{
			name:      "all plus tags",
			group:     "tags: [platform]\n    all: true",
			wantNames: []string{"blocked", "keep", "other", "platform"},
		},
		{
			name:      "all plus exclusion",
			group:     "all: true\n    exclude: [skill/blocked]",
			wantNames: []string{"keep", "other", "platform"},
		},
		{
			name:      "required protects all selection",
			group:     "all: true\n    required: true\n    exclude: [skill/blocked]",
			wantNames: []string{"blocked", "keep", "other", "platform"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			data := []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/agents.git, ref: main}}\nresources:\n  shared:\n    " + tt.group + "\n")
			if err := os.WriteFile(root, data, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadConfigFromPath(root)
			if err != nil {
				t.Fatal(err)
			}
			setCatalogKeys(cfg)
			key := cfg.Registries[0].CatalogKey
			entries := []catalog.Entry{
				{Kind: ir.KindSkill, Name: "blocked"},
				{Kind: ir.KindSkill, Name: "keep"},
				{Kind: ir.KindSkill, Name: "other"},
				{Kind: ir.KindSkill, Name: "platform", Tags: []string{"platform"}},
			}
			resolved, errs := Resolve(cfg, map[string][]catalog.Entry{key: entries}, nil)
			if len(errs) != 0 {
				t.Fatalf("resolve errors = %v", errs)
			}
			if len(resolved) != len(tt.wantNames) {
				t.Fatalf("resolved names = %#v, want %#v", resolvedNames(resolved), tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if resolved[i].Name != want {
					t.Fatalf("resolved names = %#v, want %#v", resolvedNames(resolved), tt.wantNames)
				}
			}
		})
	}
}
