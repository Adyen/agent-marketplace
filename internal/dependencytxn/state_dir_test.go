package dependencytxn

import (
	"path/filepath"
	"testing"
)

func TestStateDirPrefersAbsoluteXDG(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "state-root")
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	canonical, err := canonicalizeStatePath(xdg)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(canonical, "agent-marketplace", "dependencies") {
		t.Fatalf("StateDir = %q", got)
	}
}

func TestStateDirRejectsRelativeXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative/state")
	t.Setenv("HOME", t.TempDir())
	if _, err := StateDir(); err == nil {
		t.Fatal("expected relative XDG_STATE_HOME refusal")
	}
}

func TestStateDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonical, ".local", "state", "agent-marketplace", "dependencies")
	if got != want {
		t.Fatalf("StateDir = %q, want %q", got, want)
	}
}

func TestStateDirRejectsMissingHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := StateDir(); err == nil {
		t.Fatal("expected missing HOME refusal")
	}
}
