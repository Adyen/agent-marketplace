package pipeline

import (
	"archive/tar"
	"compress/gzip"
	"io"
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

func TestClaudeConfigurationBackupAcrossLayouts(t *testing.T) {
	for _, fromCustom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default-to-custom", true: "custom-to-default"}[fromCustom], func(t *testing.T) {
			home, custom := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			sourceRoot, sourceFile := filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")
			destRoot, destFile := custom, filepath.Join(custom, ".claude.json")
			if fromCustom {
				t.Setenv("CLAUDE_CONFIG_DIR", custom)
				sourceRoot, destRoot = destRoot, sourceRoot
				sourceFile, destFile = destFile, sourceFile
			}
			if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			original := []byte(`{"numStartups":7,"mcpServers":{"manual":{"command":"true"}}}`)
			if err := os.WriteFile(sourceFile, original, 0o600); err != nil {
				t.Fatal(err)
			}
			resources := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "probe"}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetClaude}}}
			managed := CollectManagedPaths(resources)
			if err := backup.Create(managed); err != nil {
				t.Fatal(err)
			}
			archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace/backups/claude/*.tar.gz"))
			if err != nil || len(archives) != 1 {
				t.Fatalf("archives: %v, %v", archives, err)
			}
			file, err := os.Open(archives[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			gz, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = gz.Close() }()
			reader := tar.NewReader(gz)
			count := 0
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if header.Name != ".agent-marketplace-native/machine-mcp/.claude.json" {
					t.Fatalf("unexpected archive member %q", header.Name)
				}
				count++
			}
			if count != 1 {
				t.Fatalf("configuration archived %d times", count)
			}
			t.Setenv("CLAUDE_CONFIG_DIR", custom)
			if fromCustom {
				t.Setenv("CLAUDE_CONFIG_DIR", "")
			}
			if err := os.MkdirAll(destRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			mappings, err := harness.NativeAggregateBackupFiles(hcc.TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.Restore(hcc.TargetClaude, destRoot, mappings); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(destFile)
			if err != nil || string(got) != string(original) {
				t.Fatalf("cross-layout restore: %s, %v", got, err)
			}
		})
	}
}

func expectedMachineRootAggregates(target hcc.Target) []string {
	switch target {
	case hcc.TargetClaude:
		return []string{".claude-plugin/plugin.json", "hooks/hooks.json"}
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
