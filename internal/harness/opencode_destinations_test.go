package harness

import (
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: opencode-native-configuration/native-jsonc-and-custom-destinations-remain-effective
func TestOpenCodeDestinations(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if err := os.WriteFile(filepath.Join(repo, "opencode.jsonc"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := resolveDestinations(hcc.Target("opencode"), false, repo, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 || d[0].Root != filepath.Join(repo, ".opencode") || d[1].Root != repo || d[1].AggregateFile != "opencode.jsonc" {
		t.Fatalf("repo destinations: %#v", d)
	}
	d, err = resolveDestinations(hcc.Target("opencode"), true, repo, home)
	if err != nil || len(d) != 2 || d[0].Root != filepath.Join(home, "config", "opencode") || d[1].Root != d[0].Root || d[1].AggregateFile != "opencode.json" {
		t.Fatalf("machine destinations: %#v %v", d, err)
	}
	customDir, customFile := filepath.Join(home, "custom"), filepath.Join(home, "native.jsonc")
	t.Setenv("OPENCODE_CONFIG_DIR", customDir)
	t.Setenv("OPENCODE_CONFIG", customFile)
	d, err = resolveDestinations(hcc.Target("opencode"), true, repo, home)
	if err != nil || d[0].Root != customDir || d[1].Root != home || d[1].AggregateFile != "native.jsonc" {
		t.Fatalf("custom destinations: %#v %v", d, err)
	}
}

// spec: opencode-native-configuration/ambiguous-destination-resolution-preserves-existing-output
func TestOpenCodeRelativeOverridesRefused(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", "relative")
	t.Setenv("OPENCODE_CONFIG", "")
	if _, err := resolveDestinations(hcc.Target("opencode"), true, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("relative directory override accepted")
	}
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("OPENCODE_CONFIG", "relative.json")
	if _, err := resolveDestinations(hcc.Target("opencode"), true, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("relative file override accepted")
	}
}
