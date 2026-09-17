package resolver

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestResolveAdditiveHooksFromFactoryCatalogs(t *testing.T) {
	first, err := catalog.LoadFS(fstest.MapFS{
		".factory/hooks/hooks.json": {Data: []byte(`{
			"PreToolUse": [
				{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}
			]
		}`)},
	}, catalog.HarnessFactory)
	if err != nil {
		t.Fatalf("load first catalog: %v", err)
	}
	second, err := catalog.LoadFS(fstest.MapFS{
		".factory/hooks/hooks.json": {Data: []byte(`{
			"PreToolUse": [
				{"matcher":"Bash","hooks":[{"type":"command","command":"echo second"}]}
			]
		}`)},
	}, catalog.HarnessFactory)
	if err != nil {
		t.Fatalf("load second catalog: %v", err)
	}

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:first.git", Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
		{URL: "git@example.com:second.git", Ref: "main", Source: config.RepoCommitted, Hooks: ks("PreToolUse-command")},
	}}
	setCatalogKeys(cfg)
	catalogs := map[string][]catalog.Entry{
		cfg.Registries[0].CatalogKey: first.Entries,
		cfg.Registries[1].CatalogKey: second.Entries,
	}

	resolved, errs := Resolve(cfg, catalogs, nil)
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v, want none", errs)
	}
	if len(resolved) != 2 {
		t.Fatalf("Resolve returned %d resources, want 2: %#v", len(resolved), resolved)
	}
	want := map[string]string{
		"git@example.com:first.git":  "echo first",
		"git@example.com:second.git": "echo second",
	}
	for _, artifact := range resolved {
		if artifact.Name != "PreToolUse-command" {
			t.Errorf("resolved name = %q, want PreToolUse-command", artifact.Name)
		}
		hook, ok := artifact.Resource.(*ir.Hook)
		if !ok {
			t.Fatalf("resolved resource = %T, want *ir.Hook", artifact.Resource)
		}
		if got, wantCommand := hook.Command, want[artifact.RegistryURL]; got != wantCommand {
			t.Errorf("%s command = %q, want %q", artifact.RegistryURL, got, wantCommand)
		}
		delete(want, artifact.RegistryURL)
	}
	if len(want) != 0 {
		t.Errorf("missing registry contributions: %v", want)
	}
}
