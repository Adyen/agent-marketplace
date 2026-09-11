package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMappedNativeRestoreIsExactAndSymlinkSafe(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "native.json")
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("handwritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}
	const name = ".agent-marketplace-native/machine-mcp/.claude.json"
	header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: 2}
	mappings := []map[string]string{{name: destination}}
	if err := restoreMappedFile(strings.NewReader("{}"), header, mappings); err == nil {
		t.Fatal("restore followed a symlink")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "handwritten" {
		t.Fatalf("external bytes changed: %s, %v", got, err)
	}
	if link, err := os.Readlink(destination); err != nil || link != outside {
		t.Fatalf("symlink changed: %s, %v", link, err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := restoreMappedFile(strings.NewReader("{}"), header, nil); err == nil {
		t.Fatal("unmapped native entry accepted")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("unmapped restore wrote: %v", err)
	}
	if err := restoreMappedFile(strings.NewReader("{}"), header, mappings); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "{}" {
		t.Fatalf("safe restore failed: %s, %v", got, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("restore residue: %v, %v", entries, err)
	}
}
