package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: opencode-native-configuration/machine-backup-uses-explicit-native-inventory
func TestOpenCodeCustomBackupInventory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, configuration := filepath.Join(home, "native"), filepath.Join(home, "custom.jsonc")
	t.Setenv("OPENCODE_CONFIG_DIR", root)
	t.Setenv("OPENCODE_CONFIG", configuration)
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{filepath.Join(root, "commands", "check.md"): "Check", filepath.Join(root, "notes.txt"): "Unrelated", configuration: "{ // handwritten\n \"mcp\": {}\n}"} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	managed := CollectManagedPaths([]resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "check"}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetOpenCode}}})
	paths, ok := managed[hcc.TargetOpenCode]
	if !ok || len(paths.MappedFiles) != 1 {
		t.Fatalf("custom inventory: %#v", paths)
	}
	for _, source := range paths.MappedFiles {
		if source != configuration {
			t.Fatalf("wrong native source: %s", source)
		}
	}
	original, err := os.ReadFile(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Create(managed); err != nil {
		t.Fatal(err)
	}
	archives, _ := backup.ListBackups()
	if len(archives[hcc.TargetOpenCode]) != 1 {
		t.Fatal("OpenCode archive missing")
	}
	dir, _ := backup.Dir()
	info, err := os.Stat(filepath.Join(dir, "opencode", archives[hcc.TargetOpenCode][0]+".tar.gz"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("archive is not private")
	}
	if err := os.Remove(configuration); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(home, "restored")
	if err := os.MkdirAll(restored, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(hcc.TargetOpenCode, restored, paths.MappedFiles); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(configuration)
	if err != nil || string(actual) != string(original) {
		t.Fatal("native aggregate bytes were not restored")
	}
	if _, err := os.Stat(filepath.Join(restored, "commands", "check.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(restored, "notes.txt")); !os.IsNotExist(err) {
		t.Fatal("backup traversed unrelated native content")
	}
}
