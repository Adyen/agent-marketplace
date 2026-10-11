package fsboundary

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetPrefixReadRejectsPostReadRewrite(t *testing.T) {
	rootPath := t.TempDir()
	filePath := filepath.Join(rootPath, "data.txt")
	if err := os.WriteFile(filePath, []byte("original inert text"), 0o640); err != nil {
		t.Fatal(err)
	}
	root, err := OpenDir(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	source, err := CaptureDestination(root, "data.txt", LinkPolicyRepository)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	var prefix [4]byte
	err = source.ReadTargetWith(func(reader io.Reader) error {
		if _, err := io.ReadFull(reader, prefix[:]); err != nil {
			return err
		}
		if string(prefix[:]) != "orig" {
			t.Fatalf("prefix positive control read %q", prefix)
		}
		return os.WriteFile(filePath, []byte("rewritten inert text"), 0o640)
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("post-read changed prefix was accepted: %v", err)
	}
}

func TestTargetPrefixReadRejectsAliasRetargetDuringCallback(t *testing.T) {
	rootPath := t.TempDir()
	for _, name := range []string{"original.txt", "replacement.txt"} {
		if err := os.WriteFile(filepath.Join(rootPath, name), []byte("inert body"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(rootPath, "alias.txt")
	if err := os.Symlink("original.txt", alias); err != nil {
		t.Fatal(err)
	}
	root, err := OpenDir(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	source, err := CaptureDestination(root, "alias.txt", LinkPolicyRepository)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	err = source.ReadTargetWith(func(reader io.Reader) error {
		var prefix [5]byte
		if _, err := io.ReadFull(reader, prefix[:]); err != nil {
			return err
		}
		if string(prefix[:]) != "inert" {
			t.Fatalf("contained prefix alias control failed: %q", prefix)
		}
		if err := os.Rename(alias, filepath.Join(rootPath, "retained-alias")); err != nil {
			return err
		}
		return os.Symlink("replacement.txt", alias)
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("retargeted alias prefix accepted: %v", err)
	}
}
