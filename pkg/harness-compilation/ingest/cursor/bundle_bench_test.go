package cursor

import (
	"testing"
	"testing/fstest"
)

func BenchmarkLoadBundleSkillsTreeWithGitMetadata(b *testing.B) {
	src := fstest.MapFS{
		".cursor/skills/good-skill/SKILL.md":   {Data: []byte("---\nname: good-skill\ndescription: A valid skill\n---\nBody")},
		".cursor/skills/.git/HEAD":             {Data: []byte("ref: refs/heads/main\n")},
		".cursor/skills/.git/config":           {Data: []byte("[core]\nrepositoryformatversion = 0\n")},
		".cursor/skills/.git/hooks/pre-commit": {Data: []byte("#!/bin/sh\n")},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bundle, err := LoadBundle(src)
		if err != nil {
			b.Fatal(err)
		}
		if len(bundle.Resources) != 1 {
			b.Fatalf("resources = %d, want 1", len(bundle.Resources))
		}
	}
}
