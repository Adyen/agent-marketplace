package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestUnselectedCodexDanglingRootDoesNotBlockIndependentBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	root := filepath.Join(home, ".agents")
	if err := os.Symlink("missing-dotfiles", root); err != nil {
		t.Fatal(err)
	}
	partitions, err := CollectCodexBackupPartitions(machineScopeSkill(hcc.TargetFactory))
	if err != nil || len(partitions) != 0 {
		t.Fatalf("unused absent Codex root blocked independent target: %+v %v", partitions, err)
	}
	if _, err := CollectCodexBackupPartitions(machineScopeSkill(hcc.TargetCodex)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selected unsafe Codex root lost refusal: %v", err)
	}
	if text, err := os.Readlink(root); err != nil || text != "missing-dotfiles" {
		t.Fatalf("backup probe changed root link: %q %v", text, err)
	}
	if _, err := os.Lstat(filepath.Join(home, "missing-dotfiles")); !os.IsNotExist(err) {
		t.Fatalf("backup probe created dangling target: %v", err)
	}
}
