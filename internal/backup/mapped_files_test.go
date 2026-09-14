package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
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

func TestMappedDirectoryCapturesExistingRegularFilesAndRestoresByPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "harness")
	sourceDir := filepath.Join(home, ".devin", "rules")
	restoreDir := filepath.Join(home, "restored-rules")
	for _, dir := range []string{root, sourceDir, restoreDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "managed.md"), []byte("before sync\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "handwritten.md"), []byte("handwritten\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourceDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	const prefix = ".agent-marketplace-native/devin-rules/"
	if err := Create(map[hcc.Target]ManagedPaths{
		hcc.TargetDevin: {
			Root: root,
			MappedDirectories: map[string]string{
				prefix: sourceDir,
			},
		},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.Remove(filepath.Join(sourceDir, "managed.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetDevin, root, map[string]string{prefix: restoreDir}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for name, want := range map[string]string{
		"managed.md":     "before sync\n",
		"handwritten.md": "handwritten\n",
	} {
		got, err := os.ReadFile(filepath.Join(restoreDir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested directory was archived: %v", err)
	}
}

func TestMappedDirectoryCapturesAndRestoresDevinRuleSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "harness")
	sourceDir := filepath.Join(home, ".devin", "rules")
	restoreDir := filepath.Join(home, "restored-rules")
	cacheRule := filepath.Join(home, ".cache", "agent-marketplace", "registry", "rules", "managed.md")
	for _, dir := range []string{root, sourceDir, restoreDir, filepath.Dir(cacheRule)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(cacheRule, []byte("managed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rulePath := filepath.Join(sourceDir, "managed.md")
	if err := os.Symlink(cacheRule, rulePath); err != nil {
		t.Fatal(err)
	}

	const prefix = ".agent-marketplace-native/devin-rules/"
	if err := Create(map[hcc.Target]ManagedPaths{
		hcc.TargetDevin: {
			Root: root,
			MappedDirectories: map[string]string{
				prefix: sourceDir,
			},
		},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Removing the source selection prunes the materialized Rule symlink.
	if err := os.Remove(rulePath); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetDevin, root, map[string]string{prefix: restoreDir}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored := filepath.Join(restoreDir, "managed.md")
	if got, err := os.Readlink(restored); err != nil || got != cacheRule {
		t.Fatalf("restored symlink target = %q, %v; want %q", got, err, cacheRule)
	}
	if got, err := os.ReadFile(restored); err != nil || string(got) != "managed\n" {
		t.Fatalf("restored symlink content = %q, %v", got, err)
	}
}

func TestMappedDirectoryCollectionRejectsSymlinkAncestor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".devin")); err != nil {
		t.Fatal(err)
	}
	_, err := existingMappedDirectories(map[string]string{
		".agent-marketplace-native/devin-rules/": filepath.Join(home, ".devin", "rules"),
	})
	if err == nil {
		t.Fatal("collection accepted a symlink ancestor")
	}
}

func TestMappedDirectoryRejectsUnapprovedArchivePrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	directory := filepath.Join(home, "rules")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := existingMappedDirectories(map[string]string{
		".agent-marketplace-native/other-rules/": directory,
	})
	if err == nil {
		t.Fatal("collection accepted an unapproved mapped directory prefix")
	}
}

func TestMappedDirectoryRestoreRejectsUnsafeSuffixes(t *testing.T) {
	root := t.TempDir()
	const prefix = ".agent-marketplace-native/devin-rules/"
	mappings := []map[string]string{{prefix: root}}
	for _, name := range []string{
		prefix + "../escape.md",
		prefix + "nested/escape.md",
		prefix,
	} {
		header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}
		if err := restoreMappedFile(strings.NewReader("x"), header, mappings); err == nil {
			t.Errorf("restoreMappedFile(%q) accepted unsafe suffix", name)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("unsafe restore wrote files: %v, %v", entries, err)
	}
}

func TestMappedDirectoryRestoreRejectsSymlinkRootAndDestination(t *testing.T) {
	const prefix = ".agent-marketplace-native/devin-rules/"
	header := &tar.Header{Name: prefix + "rule.md", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}
	outside := t.TempDir()

	symlinkRoot := filepath.Join(t.TempDir(), "rules")
	if err := os.Symlink(outside, symlinkRoot); err != nil {
		t.Fatal(err)
	}
	if err := restoreMappedFile(strings.NewReader("x"), header, []map[string]string{{prefix: symlinkRoot}}); err == nil {
		t.Fatal("restore accepted a symlink destination directory")
	}

	root := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.md")
	if err := os.WriteFile(outsideFile, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "rule.md")); err != nil {
		t.Fatal(err)
	}
	if err := restoreMappedFile(strings.NewReader("x"), header, []map[string]string{{prefix: root}}); err == nil {
		t.Fatal("restore accepted a symlink destination file")
	}
	if got, err := os.ReadFile(outsideFile); err != nil || string(got) != "safe" {
		t.Fatalf("outside destination changed: %q, %v", got, err)
	}
}
