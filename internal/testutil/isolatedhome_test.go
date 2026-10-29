package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsolatedHomeNeutralizesClaudeConfigurationDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "external-account"))
	home, cleanup, err := IsolatedHome()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	})
	if os.Getenv("HOME") != home || os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		t.Fatal("isolated HOME retained an external Claude machine root")
	}
}

// Cleanup must never modify files outside the isolated home. A symlink inside
// the home pointing at an external file must not have its target chmod'ed.
func TestIsolatedHomeCleanupLeavesSymlinkTargetsUntouched(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	external := filepath.Join(t.TempDir(), "external.txt")
	if err := os.WriteFile(external, []byte("do not touch"), 0o400); err != nil {
		t.Fatalf("write external file: %v", err)
	}

	home, cleanup, err := IsolatedHome()
	if err != nil {
		t.Fatalf("IsolatedHome: %v", err)
	}

	link := filepath.Join(home, "link")
	if err := os.Symlink(external, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// A read-only file inside the home must also keep its own mode; only
	// directories need permission restoration for removal.
	inner := filepath.Join(home, "inner.txt")
	if err := os.WriteFile(inner, []byte("mine"), 0o400); err != nil {
		t.Fatalf("write inner file: %v", err)
	}

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("isolated home not removed: %v", err)
	}

	info, err := os.Stat(external)
	if err != nil {
		t.Fatalf("external file missing after cleanup: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Fatalf("external file mode changed by cleanup: got %o, want %o", got, 0o400)
	}
	content, err := os.ReadFile(external)
	if err != nil {
		t.Fatalf("read external file: %v", err)
	}
	if string(content) != "do not touch" {
		t.Fatalf("external file bytes changed: %q", content)
	}
}
