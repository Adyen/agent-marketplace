package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEditV4ResourceSelectionClearRootExclude(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	data := "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\nresources:\n  local:\n    exclude: [skill/alpha, skill/sibling]\n"
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionClearExclude)
	if !handled || err != nil {
		t.Fatalf("clear = handled %v err %v", handled, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "skill/alpha") || !strings.Contains(string(got), "skill/sibling") {
		t.Fatalf("clear removed wrong exclusions: %s", got)
	}
}
