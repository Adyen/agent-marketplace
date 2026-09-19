package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4DirectorySourceKindControlsLocalIdentityAndDeduplication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.git")
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: path, Ref: "first", V4Kind: config.V4SourceDirectory, Skills: config.KindSelector{Include: []string{"review"}}},
		{URL: path, Ref: "second", V4Kind: config.V4SourceDirectory, Skills: config.KindSelector{Include: []string{"review"}}},
	}}
	setCatalogKeys(cfg)
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindSkill, Name: "review"}},
	}, nil)
	if len(errs) != 0 {
		t.Fatalf("local identity resolution errors = %v", errs)
	}
	if len(resolved) != 1 || !resolved[0].IsLocal() || resolved[0].V4Kind != config.V4SourceDirectory {
		t.Fatalf("local identity/deduplication = %#v", resolved)
	}
}

func TestV4DirectorySourceWithGitSuffixResolvesAsLocal(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "registry.git")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
sources:
  local: {directory: registry.git}
resources:
  local: [skill/review]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	setCatalogKeys(cfg)
	entry := cfg.Registries[0]
	if !entry.IsLocal() {
		t.Fatalf("directory source classified as Git: %#v", entry)
	}
	resolved, errs := Resolve(cfg, map[string][]catalog.Entry{entry.CatalogKey: {{Kind: ir.KindSkill, Name: "review"}}}, nil)
	if len(errs) != 0 || len(resolved) != 1 || resolved[0].Name != "review" {
		t.Fatalf("local resolution = %#v, errors=%v", resolved, errs)
	}
}
