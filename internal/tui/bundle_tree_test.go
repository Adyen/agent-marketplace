package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestBundleTreeInitialSelectionAndDeterministicExpansion(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "z-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"SKILL.md", "notes.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "z-dir", "later.txt"), []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a-dir", "first.txt"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree := newBundleTree(root, "")
	if got := tree.selectedPath(); got != "SKILL.md" {
		t.Fatalf("initial selection = %q, want SKILL.md", got)
	}
	var got []string
	for _, entry := range tree.entries() {
		got = append(got, entry.RelPath)
	}
	want := []string{"a-dir", "z-dir", "SKILL.md", "notes.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root entries = %v, want %v", got, want)
	}

	tree.cursor = 0
	tree.expand()
	if got := tree.entries()[0].RelPath; got != "a-dir" {
		t.Fatal("expanded tree lost directory cursor")
	}
	var expanded []string
	for _, entry := range tree.entries() {
		expanded = append(expanded, entry.RelPath)
	}
	want = []string{"a-dir", "a-dir/first.txt", "z-dir", "SKILL.md", "notes.md"}
	if !reflect.DeepEqual(expanded, want) {
		t.Fatalf("expanded entries = %v, want %v", expanded, want)
	}
}

func TestBundleTreeRestoresNestedSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts", "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "scripts", "checks", "verify.sh")
	if err := os.WriteFile(selected, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	tree := newBundleTree(root, "")
	if !tree.restoreSelection("scripts/checks/verify.sh") {
		t.Fatal("nested selection was not restored")
	}
	if got := tree.selectedPath(); got != "scripts/checks/verify.sh" {
		t.Fatalf("selected path = %q", got)
	}
	entry, ok := tree.selectedEntry()
	if !ok || entry.RelPath != "scripts/checks/verify.sh" || entry.IsDir {
		t.Fatalf("selected entry = %+v, ok=%v", entry, ok)
	}
}

func TestBundleTreeExpansionFailureIsVisible(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "scripts")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	tree := newBundleTree(root, "")
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	tree.cursor = 0
	tree.expand()
	entries := tree.entries()
	if len(entries) != 1 || entries[0].Err == nil {
		t.Fatalf("expansion error was not retained: %+v", entries)
	}
}

func TestBundleTreeSkipsSymlinksAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	tree := newBundleTree(root, "")
	if len(tree.entries()) != 1 || tree.entries()[0].RelPath != "safe.txt" {
		t.Fatalf("entries = %+v, symlink was not skipped", tree.entries())
	}
	if _, err := safeBundlePath(root, "../outside.txt"); err == nil {
		t.Fatal("traversal path was accepted")
	}
	if _, err := safeBundlePath(root, "a/../../outside.txt"); err == nil {
		t.Fatal("normalized traversal path was accepted")
	}
}

func TestBundleTreeFileBackedResourceHasOneEntry(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "command.md")
	if err := os.WriteFile(file, []byte("# command"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindCommand, Path: file}}}
	tree, err := bundleTreeForRow(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.entries()) != 1 || tree.entries()[0].RelPath != "command.md" {
		t.Fatalf("entries = %+v", tree.entries())
	}
	if tree.selectedPath() != "command.md" {
		t.Fatalf("selected path = %q", tree.selectedPath())
	}
}

// spec: tui-navigation/authoritative-remote-inspection-paths-remain-unchanged
func TestBundleTreeUsesAuthoritativeResourceSourcePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(t.TempDir(), "removed-cache", "skill", "SKILL.md")
	r := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{
		Kind:     ir.KindSkill,
		Name:     "skill",
		Path:     stalePath,
		Resource: ir.NewSkill("skill", "", "", nil, nil, ir.SourceRef{File: filepath.Join(root, "SKILL.md")}),
	}}}
	tree, err := bundleTreeForRow(r)
	if err != nil {
		t.Fatal(err)
	}
	if tree.selectedPath() != "SKILL.md" {
		t.Fatalf("selected path = %q, want SKILL.md", tree.selectedPath())
	}
}

func TestRenderBundleFileCapsAndRendersSelectedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "selected.md"), []byte("# Selected\n\ncontent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("# Other"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := renderBundleFile(true, root, "selected.md", 40)
	if !contains(out, "Selected") || contains(out, "Other") {
		t.Fatalf("selected preview = %q", out)
	}
}

func BenchmarkBundlePreviewRendering(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill\n\n"+string(make([]byte, 4096))), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderBundleFile(true, root, "SKILL.md", 80)
	}
}

func BenchmarkBundleTreeInitializationAndExpansion(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 24; i++ {
		dir := filepath.Join(root, "dir", string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 12; j++ {
			if err := os.WriteFile(filepath.Join(dir, "file-"+string(rune('a'+j))+".md"), []byte("# content"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree := newBundleTree(root, "")
		tree.cursor = 0
		tree.expand()
	}
}
