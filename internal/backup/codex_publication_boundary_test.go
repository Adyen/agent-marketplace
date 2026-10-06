package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spec: backup-recovery/codex-set-publication-preserves-aliased-partial-manifest
func TestCreateCodexSetPreservesAliasedPartialManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	foreign := filepath.Join(t.TempDir(), "foreign-manifest")
	if err := os.WriteFile(foreign, []byte("inert foreign metadata"), 0o640); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := -2; i <= 120; i++ {
		name := now.Add(time.Duration(i)*time.Second).Format("2006-01-02-150405") + ".set.json.partial"
		if err := os.Symlink(foreign, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(home, ".agents")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "skills", "demo")
	if err := os.WriteFile(managed, []byte("inert archived generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(operation) < len("2006-01-02-150405") {
		t.Fatalf("invalid operation: %q", operation)
	}
	base := operation[:len("2006-01-02-150405")]
	if link, err := os.Readlink(filepath.Join(dir, base+".set.json.partial")); err != nil || link != foreign {
		t.Fatalf("operation did not exercise a seeded partial alias: %q %v", link, err)
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), operation+"-") && strings.HasSuffix(entry.Name(), ".partial") {
				t.Fatalf("attempt partial residue: %s", entry.Name())
			}
		}
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "inert foreign metadata" {
		t.Fatalf("partial manifest writer changed foreign bytes: %q %v", data, err)
	}
	for i := -2; i <= 120; i++ {
		name := now.Add(time.Duration(i)*time.Second).Format("2006-01-02-150405") + ".set.json.partial"
		if link, err := os.Readlink(filepath.Join(dir, name)); err != nil || link != foreign {
			t.Fatalf("pre-existing partial alias changed: %q %v", link, err)
		}
	}
	if err := os.WriteFile(managed, []byte("inert current generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(partitions); err != nil || restored != operation {
		t.Fatalf("valid set failed to restore: %q %v", restored, err)
	}
	if data, err := os.ReadFile(managed); err != nil || string(data) != "inert archived generation" {
		t.Fatalf("set omitted actual source payload: %q %v", data, err)
	}
}
