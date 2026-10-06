package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/backup-source-hardlink-refuses-before-payload-import
func TestCreateRefusesHardlinkedSourcePayload(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		name := "native-tree"
		if mapped {
			name = "mapped-native"
		}
		t.Run(name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			file := filepath.Join(root, "skills", "demo", "SKILL.md")
			if mapped {
				file = filepath.Join(t.TempDir(), "native.json")
			}
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("inert private snapshot payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(t.TempDir(), "foreign-payload")
			if err := os.Link(file, foreign); err != nil {
				t.Fatal(err)
			}
			paths := ManagedPaths{Root: root, RelPaths: []string{"skills"}}
			if mapped {
				paths = ManagedPaths{Root: filepath.Join(root, "absent-harness-root"), MappedFiles: map[string]string{".agent-marketplace-native/machine-mcp/.claude.json": file}}
			}
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: paths}); !errors.Is(err, fsboundary.ErrUnsafeFile) {
				t.Fatalf("unsafe source payload archived: %v", err)
			}
			if after, err := os.ReadFile(foreign); err != nil || string(after) != "inert private snapshot payload" {
				t.Fatalf("foreign source changed: %q %v", after, err)
			}
			archiveDir := filepath.Join(home, ".agent-marketplace", "backups", "factory")
			if entries, err := os.ReadDir(archiveDir); err != nil || len(entries) != 0 {
				t.Fatalf("unsafe archive residue: %v %v", entries, err)
			}
			if err := os.Remove(foreign); err != nil {
				t.Fatal(err)
			}
			if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: paths}); err != nil {
				t.Fatalf("regular source snapshot refused: %v", err)
			}
			if err := os.WriteFile(file, []byte("inert later generation"), 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(hcc.TargetFactory, root, paths.MappedFiles); err != nil {
				t.Fatal(err)
			}
			if after, err := os.ReadFile(file); err != nil || string(after) != "inert private snapshot payload" {
				t.Fatalf("regular source archive missed actual payload: %q %v", after, err)
			}
		})
	}
}
