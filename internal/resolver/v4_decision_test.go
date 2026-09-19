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

func TestV4ContainingFilePrecedenceWinsCrossSourceCollision(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), []byte(`sources:
  child: {git: https://example.test/a.git, ref: main}
resources:
  child: {include: [skill/shared]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: fragment.yaml
sources:
  root: {git: https://example.test/z.git, ref: main}
resources:
  root: {include: [skill/shared]}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	setCatalogKeys(cfg)
	catalogs := make(map[string][]catalog.Entry, len(cfg.Registries))
	for _, entry := range cfg.Registries {
		catalogs[entry.CatalogKey] = []catalog.Entry{{Kind: ir.KindSkill, Name: "shared"}}
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 1 || !IsNameCollision(errs[0]) {
		t.Fatalf("resolve errors = %v, want one collision warning", errs)
	}
	if len(resolved) != 1 || resolved[0].RegistryURL != "https://example.test/z.git" {
		t.Fatalf("resolved = %#v, want containing-file source z.git", resolved)
	}
}

func TestV4ExclusionIsScopedToItsSourceIdentityAcrossLayers(t *testing.T) {
	for _, tt := range []struct {
		name       string
		localURL   string
		wantReview bool
	}{
		{name: "different source is preserved", localURL: "https://example.test/local.git", wantReview: true},
		{name: "same source is excluded", localURL: "https://example.test/repo.git", wantReview: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			repoPath := filepath.Join(dir, "config.yaml")
			localPath := filepath.Join(dir, "config.local.yaml")
			if err := os.WriteFile(repoPath, []byte(`apiVersion: agent-marketplace/v4
sources:
  repo: {git: https://example.test/repo.git, ref: main}
resources:
  repo: {include: [skill/review]}
`), 0o600); err != nil {
				t.Fatal(err)
			}
			localData := []byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {git: " + tt.localURL + ", ref: main}\nresources:\n  local: {exclude: [skill/review]}\n")
			if err := os.WriteFile(localPath, localData, 0o600); err != nil {
				t.Fatal(err)
			}
			repo, err := config.LoadRootConfigFromPath(repoPath)
			if err != nil {
				t.Fatal(err)
			}
			local, err := config.LoadRootConfigFromPath(localPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.MergeLayers([]config.Layer{
				{Source: config.RepoCommitted, Config: repo},
				{Source: config.RepoPersonal, Config: local},
			})
			setCatalogKeys(cfg)
			catalogs := make(map[string][]catalog.Entry, len(cfg.Registries))
			for _, entry := range cfg.Registries {
				catalogs[entry.CatalogKey] = nil
				if entry.URL == "https://example.test/repo.git" {
					catalogs[entry.CatalogKey] = []catalog.Entry{{Kind: ir.KindSkill, Name: "review"}}
				}
			}

			resolved, errs := Resolve(cfg, catalogs, nil)
			if len(errs) != 0 {
				t.Fatalf("resolve errors = %v", errs)
			}
			if got := len(resolved) == 1 && resolved[0].Name == "review"; got != tt.wantReview {
				t.Fatalf("resolved = %#v, want review present=%t", resolved, tt.wantReview)
			}
		})
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
