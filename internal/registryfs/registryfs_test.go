package registryfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTreeRejectsRemoteSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	err := ValidateTree(root, true)
	if err == nil || !strings.Contains(err.Error(), "remote registries do not allow symlinks") {
		t.Fatalf("ValidateTree error = %v, want actionable remote symlink rejection", err)
	}
}

func TestValidateTreeAllowsContainedLocalSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	if err := ValidateTree(root, false); err != nil {
		t.Fatalf("ValidateTree rejected contained local symlink: %v", err)
	}
}

func TestValidateTreeRejectsEscapingLocalSymlinks(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "sentinel"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	err := ValidateTree(root, false)
	if err == nil || !strings.Contains(err.Error(), "resolves outside the registry root") {
		t.Fatalf("ValidateTree error = %v, want containment rejection", err)
	}
}
