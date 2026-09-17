package resolver

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestResolveCatalogViewKeepsAdditiveHookProviders(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:first.git", Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
		{URL: "git@example.com:second.git", Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: ir.NewHookWithMatcher("PreToolUse-command", "PreToolUse", "Bash", "echo first", "command", nil, ir.SourceRef{})}},
		cfg.Registries[1].CatalogKey: {{Kind: ir.KindHook, Name: "PreToolUse-command", Resource: ir.NewHookWithMatcher("PreToolUse-command", "PreToolUse", "Bash", "echo second", "command", nil, ir.SourceRef{})}},
	}
	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 || len(resolved) != 2 {
		t.Fatalf("Resolve = %d resources, %v errors; want two resources and no errors", len(resolved), errs)
	}

	selectedView := ResolveCatalogView(cfg, catalogs, resolved[:1])
	if len(selectedView) != 2 {
		t.Fatalf("selected catalog view has %d rows, want 2: %#v", len(selectedView), selectedView)
	}
	if !selectedView[0].Selected || selectedView[0].RegistryURL != "git@example.com:first.git" {
		t.Fatalf("selected first row = %+v, want selected first provider", selectedView[0])
	}
	if selectedView[1].Selected || selectedView[1].RegistryURL != "git@example.com:second.git" {
		t.Fatalf("unselected second row = %+v, want available second provider", selectedView[1])
	}

	availableView := ResolveCatalogView(cfg, catalogs, nil)
	if len(availableView) != 2 {
		t.Fatalf("available catalog view has %d rows, want 2: %#v", len(availableView), availableView)
	}
	for _, row := range availableView {
		if row.Selected {
			t.Fatalf("available row marked selected: %+v", row)
		}
	}
}
