//go:build unix

package fsboundary

import (
	"errors"
	"fmt"
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

func TestContainedDirectoryRootResolvesIntermediateAliases(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "scripts"))
	mustMkdir(t, filepath.Join(path, "scripts", "dir"))
	mustMkdir(t, filepath.Join(path, "scripts", "dir", "real"))
	mustMkdir(t, filepath.Join(path, "scripts", "dir", "real", "child"))
	mustMkdir(t, filepath.Join(path, "scripts", "dir", "real", "child", "deep"))
	mustSymlink(t, "real", filepath.Join(path, "scripts", "dir", "contained"))
	mustSymlink(t, "contained/child", filepath.Join(path, "scripts", "dir", "alias-child"))
	mustSymlink(t, "scripts/dir/alias-child", filepath.Join(path, "references"))
	for _, rel := range []string{"scripts/dir/alias-child/deep", "scripts/dir/contained/child/deep", "references/deep"} {
		t.Run(rel, func(t *testing.T) {
			opened, err := root.OpenContainedRoot(rel)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = opened.Close() }()
			want := filepath.Join(path, "scripts", "dir", "real", "child", "deep")
			if opened.Path() != want {
				t.Fatalf("resolved path = %s, want %s", opened.Path(), want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		open func(string) (*Dir, error)
		rel  string
	}{
		{"OpenRoot", root.OpenRoot, "scripts/dir/contained/child"},
		{"EnsureRoot", root.EnsureRoot, "scripts/dir/contained/new-child"},
		{"OpenPath", root.OpenPath, "scripts/dir/contained/child"},
		{"EnsurePath", root.EnsurePath, "scripts/dir/contained/new-child"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened, err := tc.open(tc.rel)
			if opened != nil {
				_ = opened.Close()
			}
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("native ancestor alias accepted: %v", err)
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(path, "scripts", "dir", "real", "new-child")); !os.IsNotExist(err) {
		t.Fatalf("native refusal created directory: %v", err)
	}
}

func TestContainedDirectoryRootRefusesUnsafeIntermediateAliases(t *testing.T) {
	for _, kind := range []string{"external", "dangling", "cycle", "external-return", "parent-return", "hop-limit"} {
		t.Run(kind, func(t *testing.T) {
			root, path := tempRoot(t)
			outside := t.TempDir()
			mustMkdir(t, filepath.Join(outside, "child"))
			mustMkdir(t, filepath.Join(path, "real"))
			mustMkdir(t, filepath.Join(path, "real", "child"))
			mustWrite(t, filepath.Join(outside, "sentinel"), []byte("preserve outside bytes"), 0o640)
			before, err := os.Stat(filepath.Join(outside, "sentinel"))
			if err != nil {
				t.Fatal(err)
			}
			target := outside
			switch kind {
			case "dangling":
				target = "missing"
			case "cycle":
				target = "hop"
			case "external-return":
				target = filepath.Join(outside, "return")
				mustSymlink(t, filepath.Join(path, "real"), target)
			case "parent-return":
				target = "real/../real"
			case "hop-limit":
				target = "chain-0"
				for i := 0; i <= maxLinkHops; i++ {
					next := fmt.Sprintf("chain-%d", i+1)
					if i == maxLinkHops {
						next = "real"
					}
					mustSymlink(t, next, filepath.Join(path, fmt.Sprintf("chain-%d", i)))
				}
			}
			mustSymlink(t, target, filepath.Join(path, "hop"))
			mustSymlink(t, "hop/child", filepath.Join(path, "alias-child"))
			for _, rel := range []string{"hop/child", "alias-child"} {
				opened, err := root.OpenContainedRoot(rel)
				if opened != nil {
					_ = opened.Close()
				}
				if !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("%s accepted unsafe intermediate alias: %v", rel, err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(outside, "sentinel")); err != nil || string(data) != "preserve outside bytes" {
				t.Fatalf("refusal changed outside bytes: %q %v", data, err)
			}
			if after, err := os.Stat(filepath.Join(outside, "sentinel")); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("refusal changed outside metadata: %v", err)
			}
			if text, err := os.Readlink(filepath.Join(path, "hop")); err != nil || text != target {
				t.Fatalf("refusal changed intermediate alias: %q %v", text, err)
			}
		})
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
