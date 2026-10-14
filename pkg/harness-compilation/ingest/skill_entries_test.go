package ingest_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

func TestReadSkillEntriesExcludesGitMetadata(t *testing.T) {
	src := fstest.MapFS{
		"root/.git/config":       {Data: []byte("gitdir")},
		"root/.gitmodules":       {Data: []byte("sub")},
		"root/alpha/SKILL.md":    {Data: []byte("a")},
		"root/git/SKILL.md":      {Data: []byte("near-match")},
		"root/.gitkeep/SKILL.md": {Data: []byte("near-match")},
		"root/zeta/SKILL.md":     {Data: []byte("z")},
	}
	entries, err := ingest.ReadSkillEntries(src, "root")
	if err != nil {
		t.Fatalf("ReadSkillEntries: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{".gitkeep", ".gitmodules", "alpha", "git", "zeta"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestReadSkillEntriesExcludesGitFileAndSymlink(t *testing.T) {
	for name, file := range map[string]*fstest.MapFile{
		"file":    {Data: []byte("gitdir: elsewhere")},
		"symlink": {Data: []byte("gitdir: elsewhere"), Mode: fs.ModeSymlink},
	} {
		src := fstest.MapFS{
			"root/.git":           file,
			"root/skill/SKILL.md": {Data: []byte("s")},
		}
		entries, err := ingest.ReadSkillEntries(src, "root")
		if err != nil {
			t.Fatalf("%s: ReadSkillEntries: %v", name, err)
		}
		if len(entries) != 1 || entries[0].Name() != "skill" {
			t.Fatalf("%s: entries = %v, want only skill", name, entries)
		}
	}
}

type errFS struct{ err error }

func (e errFS) Open(string) (fs.File, error) { return nil, e.err }

func TestReadSkillEntriesReadErrorIdentity(t *testing.T) {
	sentinel := errors.New("boom")
	entries, err := ingest.ReadSkillEntries(errFS{sentinel}, "root")
	if entries != nil {
		t.Fatalf("entries = %v, want nil", entries)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
}
