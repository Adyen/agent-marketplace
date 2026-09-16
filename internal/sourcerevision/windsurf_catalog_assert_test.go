package sourcerevision

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func assertWindsurfCatalog(t *testing.T, cat *catalog.Catalog, version string) {
	t.Helper()
	if cat == nil {
		t.Fatal("Build returned nil catalog")
	}
	want := map[string]string{"hooks": "echo second"}
	if version == "v3" {
		want["hooks-2"] = "echo first"
	}
	if len(cat.Entries) != len(want) {
		t.Fatalf("%s catalog entries = %d, want %d", version, len(cat.Entries), len(want))
	}
	for _, entry := range cat.Entries {
		hook, ok := entry.Resource.(*ir.Hook)
		if !ok {
			t.Fatalf("catalog entry %q resource = %T, want hook", entry.Name, entry.Resource)
		}
		command, ok := want[entry.Name]
		if !ok || hook.Command != command {
			t.Fatalf("%s catalog hook %q command=%q, want %q", version, entry.Name, hook.Command, command)
		}
		delete(want, entry.Name)
	}
	if len(want) != 0 {
		t.Fatalf("%s catalog missing hooks %v", version, want)
	}
}
