package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadBytesInfoRegularFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "lock.yaml")
	data := bytes.Repeat([]byte("x"), 4096)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		raw, info, err := ReadBytesInfo(path)
		if err != nil || !bytes.Equal(raw, data) || info == nil || !info.Mode().IsRegular() || info.Size() != int64(len(data)) || info.Mode().Perm() != 0o600 {
			b.Fatalf("lock read info=%v error=%v", info, err)
		}
	}
}

func BenchmarkPlanUnchanged(b *testing.B) {
	commit, err := ParseGitOID(sha1a)
	if err != nil {
		b.Fatal(err)
	}
	doc := Document{APIVersion: APIVersionV2, Scope: ScopeRepo}
	requirements := Requirements{Scope: ScopeRepo}
	for i := 0; i < 100; i++ {
		url := fmt.Sprintf("https://git.example.com/team/repo-%03d", i)
		rootPath := fmt.Sprintf("resources/%03d", i)
		doc.Registries = append(doc.Registries, GitRegistry{URL: url, Ref: "main", Commit: commit, Roots: []GitRoot{{Path: rootPath, SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}})
		requirements.Git = append(requirements.Git, GitRequirement{URL: url, Ref: "main", Roots: []GitRootRequirement{{Path: rootPath, SourceHarness: "claude"}}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := Plan(requirements, &doc, OperationSync)
		if err != nil {
			b.Fatal(err)
		}
		if plan.Changed() || len(plan.Retained) != 100 {
			b.Fatalf("unexpected plan: %#v", plan)
		}
	}
}
