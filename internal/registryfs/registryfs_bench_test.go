package registryfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The registry confinement layer sits on every discovery and sync read, so its
// per-read and per-tree cost is measured here and the benchmarks stay in the
// package as a regression guard (see the performance policy in AGENTS.md).

// buildRegistry writes a registry-shaped fixture: skills each holding a
// SKILL.md plus support files, and a `.git` directory of the size a real clone
// contributes. The `.git` content is what makes an eager whole-tree validation
// expensive, so it belongs in the fixture rather than being wished away.
func buildRegistry(tb testing.TB, skills, supportPerSkill, gitFiles int) string {
	tb.Helper()
	root := tb.TempDir()
	for i := 0; i < skills; i++ {
		dir := filepath.Join(root, "skills", fmt.Sprintf("skill-%03d", i))
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: s\ndescription: d\n---\nbody\n"), 0o644); err != nil {
			tb.Fatal(err)
		}
		for j := 0; j < supportPerSkill; j++ {
			name := filepath.Join(dir, "scripts", fmt.Sprintf("support-%03d.sh", j))
			if err := os.WriteFile(name, []byte("#!/bin/sh\necho support\n"), 0o755); err != nil {
				tb.Fatal(err)
			}
		}
	}
	for i := 0; i < gitFiles; i++ {
		dir := filepath.Join(root, ".git", "objects", fmt.Sprintf("%02x", i%256))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("obj-%04d", i)), []byte("object"), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return root
}

func BenchmarkFSReadFile(b *testing.B) {
	root := buildRegistry(b, 4, 4, 0)
	fsys, err := New(root, false)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	name := "skills/skill-002/scripts/support-002.sh"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fs.ReadFile(fsys, name); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFSStat(b *testing.B) {
	root := buildRegistry(b, 4, 4, 0)
	fsys, err := New(root, false)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	name := "skills/skill-002/SKILL.md"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fs.Stat(fsys, name); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFSReadDir(b *testing.B) {
	root := buildRegistry(b, 4, 32, 0)
	fsys, err := New(root, false)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	name := "skills/skill-002/scripts"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fs.ReadDir(fsys, name); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWalkDirRegistry measures the traversal ingestion performs over a
// whole registry through the confined FS.
func BenchmarkWalkDirRegistry(b *testing.B) {
	for _, skills := range []int{10, 100} {
		root := buildRegistry(b, skills, 5, 0)
		b.Run(fmt.Sprintf("skills=%d", skills), func(b *testing.B) {
			fsys, err := New(root, false)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = fsys.Close() }()
			b.ReportAllocs()
			for b.Loop() {
				if err := fs.WalkDir(fsys, ".", func(_ string, _ fs.DirEntry, err error) error { return err }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkValidateTree measures the eager remote-registry policy walk. The
// `.git` sub-benchmark is the blast-radius case: a clone's metadata dwarfs its
// content, which is why the walk skips it.
func BenchmarkValidateTree(b *testing.B) {
	cases := []struct {
		name     string
		skills   int
		support  int
		gitFiles int
	}{
		{name: "content-only", skills: 50, support: 5},
		{name: "with-git-metadata", skills: 50, support: 5, gitFiles: 2000},
	}
	for _, tc := range cases {
		root := buildRegistry(b, tc.skills, tc.support, tc.gitFiles)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := ValidateTree(root, true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
