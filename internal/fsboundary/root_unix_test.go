//go:build unix

package fsboundary

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryRootAdmissionAndStrictPaths(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "native"))
	mustSymlink(t, "native", filepath.Join(path, "alias"))
	opened, err := root.OpenRoot("alias")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	created, err := opened.EnsurePath("skills/probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "native", "skills", "probe")); err != nil {
		t.Fatalf("contained root did not receive directories: %v", err)
	}
	if _, err := root.OpenPath("alias/skills"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("strict path followed a root-shaped link: %v", err)
	}
	outside := t.TempDir()
	mustSymlink(t, outside, filepath.Join(path, "escape"))
	if directory, err := root.EnsureRoot("escape"); !errors.Is(err, ErrUnsafePath) {
		if directory != nil {
			_ = directory.Close()
		}
		t.Fatalf("escaping root accepted: %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("escaping root wrote output: %v %v", entries, err)
	}
	mustSymlink(t, "native/../native", filepath.Join(path, "ambiguous"))
	if directory, err := root.OpenRoot("ambiguous"); !errors.Is(err, ErrUnsafePath) {
		if directory != nil {
			_ = directory.Close()
		}
		t.Fatalf("ambiguous link text accepted: %v", err)
	}
}

func TestCapturedDirectoryCreationCannotRedirectToReplacement(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "native"))
	captured, err := root.OpenRoot("native")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = captured.Close() }()
	original := filepath.Join(path, "captured-original")
	if err := os.Rename(filepath.Join(path, "native"), original); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Join(path, "native"))
	created, err := captured.EnsurePath("skills/probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(original, "skills", "probe")); err != nil {
		t.Fatalf("creation detached from captured object: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(path, "native")); err != nil || len(entries) != 0 {
		t.Fatalf("creation redirected into replacement: %v %v", entries, err)
	}
}
