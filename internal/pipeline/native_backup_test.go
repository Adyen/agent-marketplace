package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func expectedMachineRootAggregates(target hcc.Target) []string {
	switch target {
	case hcc.TargetClaude:
		return []string{"hooks/hooks.json"}
	case hcc.TargetGemini:
		return []string{"settings.json"}
	default:
		info, _ := harness.TargetInfoFor(target)
		return info.AggregateFilesForScope(true)
	}
}

func TestClaudeNativeMCPBackupCoverage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(home, ".claude.json")
	original := []byte(`{"theme":"dark","mcpServers":{"manual":{"command":"manual"}}}`)
	if err := os.WriteFile(native, original, 0o600); err != nil {
		t.Fatal(err)
	}
	resources := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "probe"}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetClaude}}}
	managed := CollectManagedPaths(resources)
	if err := backup.Create(managed); err != nil {
		t.Fatal(err)
	}
	backups, err := backup.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups[hcc.TargetClaude]) != 1 {
		t.Fatalf("native file was not archived: %v", backups)
	}
	dir, err := backup.Dir()
	if err != nil {
		t.Fatal(err)
	}
	archiveInfo, err := os.Stat(filepath.Join(dir, "claude", backups[hcc.TargetClaude][0]+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if archiveInfo.Mode().Perm() != 0o600 {
		t.Fatalf("native settings backup permissions = %o", archiveInfo.Mode().Perm())
	}
	if err := os.WriteFile(native, []byte(`{"changed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(hcc.TargetClaude, root, managed[hcc.TargetClaude].MappedFiles); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(native)
	if err != nil || string(got) != string(original) {
		t.Fatalf("restore = %s, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-marketplace-native")); !os.IsNotExist(err) {
		t.Fatalf("restored into wrong root: %v", err)
	}
}
