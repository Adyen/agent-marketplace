package gemini

import (
	"testing"
	"testing/fstest"
)

func BenchmarkLoadBundleSkillsTreeWithGitMetadata(b *testing.B) {
	src := fstest.MapFS{
		".gemini/skills/good-skill/SKILL.md":   {Data: []byte("---\nname: good-skill\ndescription: A valid skill\n---\nBody")},
		".gemini/skills/.git/HEAD":             {Data: []byte("ref: refs/heads/main\n")},
		".gemini/skills/.git/config":           {Data: []byte("[core]\nrepositoryformatversion = 0\n")},
		".gemini/skills/.git/hooks/pre-commit": {Data: []byte("#!/bin/sh\n")},
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
