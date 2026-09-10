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
