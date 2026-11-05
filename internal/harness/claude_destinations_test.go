package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: claude-native-configuration/unset-or-empty-variable-keeps-the-default-roots
func TestClaudeConfigurationDestinations(t *testing.T) {
	home, repo, external := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for _, custom := range []string{"", filepath.Join(home, ".claude-alt"), external} {
		t.Run(custom, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", custom)
			root, aggregateRoot := filepath.Join(home, ".claude"), home
			if custom != "" {
				root, aggregateRoot = custom, custom
			}
			machine, err := ResolveDestinations(hcc.TargetClaude, true, repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(machine) != 2 || machine[0].Root != root || machine[1].Root != aggregateRoot || machine[1].AggregateFile != ".claude.json" {
				t.Fatalf("machine destinations = %+v", machine)
			}
			if got := MakeInstallRoots()[hcc.TargetClaude].Machine; got != root {
				t.Fatalf("install root = %s, want %s", got, root)
			}
			if got, ok := MachineRoot(hcc.TargetClaude); !ok || got != root {
				t.Fatalf("machine root = %s, %v, want %s", got, ok, root)
			}
			project, err := ResolveDestinations(hcc.TargetClaude, false, repo)
			if err != nil || project[0].Root != filepath.Join(repo, ".claude") || project[1].Root != repo || project[1].AggregateFile != ".mcp.json" {
				t.Fatalf("project destinations = %+v, %v", project, err)
			}
			mapped, err := NativeAggregateBackupFiles(hcc.TargetClaude)
			if err != nil || mapped[".agent-marketplace-native/machine-mcp/.claude.json"] != filepath.Join(aggregateRoot, ".claude.json") {
				t.Fatalf("backup mappings = %v, %v", mapped, err)
			}
		})
	}
	t.Run("relative", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "relative")
		if _, err := ResolveDestinations(hcc.TargetClaude, true, repo); err == nil || !strings.Contains(err.Error(), "CLAUDE_CONFIG_DIR must be absolute") {
			t.Fatalf("relative root: %v", err)
		}
		if _, err := ResolveDestinations(hcc.TargetClaude, false, repo); err != nil {
			t.Fatalf("relative machine root affects repository: %v", err)
		}
	})
}

// spec: claude-native-configuration/harness-detection-recognizes-the-configuration-directory
func TestClaudeConfigurationDetection(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "account")
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	for _, path := range []string{filepath.Join(home, ".claude"), custom} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := DetectInstalled(home)
	if err != nil || !slices.Contains(got, "claude") {
		t.Fatalf("custom root not detected: %v, %v", got, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "missing"))
	got, err = DetectInstalled(home)
	if err != nil || slices.Contains(got, "claude") {
		t.Fatalf("default root detected despite override: %v, %v", got, err)
	}
}
