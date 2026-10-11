package backup

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: backup-recovery/restore-staged-payload-replacement-refuses-publication
func TestRestoreStagedPayloadReplacementPreservesForeignGeneration(t *testing.T) {
	for _, lane := range []string{"ordinary", "mapped", "codex-set"} {
		t.Run(lane, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			file := filepath.Join(root, "native.json")
			if err := os.WriteFile(file, []byte("inert archived restore payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			restore := func() (string, error) { return Restore(hcc.TargetFactory, root) }
			switch lane {
			case "ordinary":
				if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetFactory: {Root: root, RelPaths: []string{"native.json"}}}); err != nil {
					t.Fatal(err)
				}
			case "mapped":
				mappings := map[string]string{".agent-marketplace-native/machine-mcp/.claude.json": file}
				if err := Create(map[hcc.Target]ManagedPaths{hcc.TargetClaude: {Root: filepath.Join(root, "absent-ordinary-root"), MappedFiles: mappings}}); err != nil {
					t.Fatal(err)
				}
				restore = func() (string, error) { return Restore(hcc.TargetClaude, root, mappings) }
			case "codex-set":
				partitions := []CodexPartition{{ID: "machine-agents", Root: root, RelPaths: []string{"native.json"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
				if _, err := CreateCodexSet(partitions); err != nil {
					t.Fatal(err)
				}
				restore = func() (string, error) { return RestoreCodexSet(partitions) }
			}
			if err := os.WriteFile(file, []byte("inert current native user bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			retained := filepath.Join(t.TempDir(), "original-restore-stage")
			changed := false
			replacement := ""
			var foreign, originalStage os.FileInfo
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(codexRestoreObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), onPublication: func(path string) {
				if changed {
					return
				}
				changed = true
				replacement = path
				originalStage, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("foreign inert restore stage replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				foreign, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
			}}))
			restored, err := restore()
			if raw, readErr := os.ReadFile(file); readErr != nil || string(raw) != "inert current native user bytes" {
				t.Fatalf("restore promoted foreign staged bytes: reached=%t timestamp=%q err=%v raw=%q read=%v", changed, restored, err, raw, readErr)
			}
			if !changed || restored != "" || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("restore stage replacement did not refuse: %q %v", restored, err)
			}
			if after, err := os.Stat(file); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("refusal changed current native generation: %v", err)
			}
			if raw, err := os.ReadFile(replacement); err != nil || string(raw) != "foreign inert restore stage replacement" {
				t.Fatalf("cleanup changed foreign stage: %q %v", raw, err)
			}
			if after, err := os.Stat(replacement); err != nil || !os.SameFile(foreign, after) || foreign.Mode() != after.Mode() || !foreign.ModTime().Equal(after.ModTime()) {
				t.Fatalf("cleanup changed foreign stage metadata: %v", err)
			}
			if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert archived restore payload" {
				t.Fatalf("original attempt stage changed: %q %v", raw, err)
			}
			if info, err := os.Stat(retained); err != nil || !os.SameFile(originalStage, info) || originalStage.Mode() != info.Mode() || !originalStage.ModTime().Equal(info.ModTime()) {
				t.Fatalf("original staged generation metadata changed: %v", err)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 2 {
				t.Fatalf("refusal stage/output membership changed: %v %v", entries, err)
			}
			restored, err = restore()
			if err != nil || restored == "" {
				t.Fatalf("healthy archive restore failed: %q %v", restored, err)
			}
			if raw, err := os.ReadFile(file); err != nil || string(raw) != "inert archived restore payload" {
				t.Fatalf("healthy restore did not publish actual archive: %q %v", raw, err)
			}
			if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("healthy archived mode changed: %v", err)
			}
			if raw, err := os.ReadFile(replacement); err != nil || string(raw) != "foreign inert restore stage replacement" {
				t.Fatalf("healthy restore deleted foreign stage: %q %v", raw, err)
			}
		})
	}
}
