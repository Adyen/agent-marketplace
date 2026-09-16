package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadFSWindsurfHookInventoryOptions(t *testing.T) {
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"}]}}`)}}
	current, err := LoadFSWithOptions(src, "windsurf", IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Entries) != 2 {
		t.Fatalf("current entries = %d, want 2", len(current.Entries))
	}
	if _, ok := Resources(current)[skill.ResourceKey(ir.KindHook, "hooks-2")]; !ok {
		t.Fatalf("current entries = %#v, want hooks-2 provider", current.Entries)
	}
	legacy, err := LoadFSWithOptions(src, "windsurf", IngestOptions{LegacyWindsurfHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Entries) != 1 || legacy.Entries[0].Name != "hooks" {
		t.Fatalf("legacy entries = %#v, want one hooks provider", legacy.Entries)
	}
}

func TestDiscoverWindsurfOptionsFlowThroughSubBundles(t *testing.T) {
	root := t.TempDir()
	hooksPath := filepath.Join(root, "child", ".windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := DiscoverWithOptions(root, "windsurf", IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Entries) != 2 {
		t.Fatalf("current discovered entries = %d, want 2", len(current.Entries))
	}
	legacy, err := DiscoverWithOptions(root, "windsurf", IngestOptions{LegacyWindsurfHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Entries) != 1 || legacy.Entries[0].Name != "hooks" {
		t.Fatalf("legacy discovered entries = %#v, want one hooks provider", legacy.Entries)
	}
}
