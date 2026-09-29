package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func benchEngine(n int) *Engine {
	res := make([]Resource, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, Resource{
			Ref:         fmt.Sprintf("reg/skill/skill-%05d", i),
			Kind:        "skill",
			Name:        fmt.Sprintf("skill-%05d", i),
			Description: "benchmark catalog resource with a realistic description",
			Tags:        []string{"tag-a", "tag-b"},
			Registry:    "reg",
			Revision:    "v1",
			Assessment:  "allow",
		})
	}
	store := &SnapshotStore{}
	store.Store(&Snapshot{Resources: res})
	return &Engine{Snapshots: store, Skills: fakeSkills{}, Pool: newFakePool(), Limits: Limits{}}
}

func BenchmarkSearchCatalog(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			e := benchEngine(n)
			args, _ := json.Marshal(map[string]any{
				"code": `async () => catalog.filter(r => r.kind === "skill" && r.name.includes("99")).map(r => r.ref)`,
			})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := e.Search(context.Background(), args); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkExecuteListTools(b *testing.B) {
	store := &SnapshotStore{}
	store.Store(&Snapshot{Resources: []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Revision: "v1", Assessment: "allow"},
	}})
	e := &Engine{Snapshots: store, Skills: fakeSkills{}, Pool: newFakePool(), Limits: Limits{}}
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => { const t = await mcp.srv.listTools(); return (await mcp.srv.ping({x: 1})).alias }`,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := e.Execute(context.Background(), args); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteVMStartup(b *testing.B) {
	e := benchEngine(1)
	args, _ := json.Marshal(map[string]any{
		"code": `async () => catalog.length`,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := e.Execute(context.Background(), args); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildSnapshot(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	base := b.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(b, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(b, "", "init", "--initial-branch=main", work)
	snapshotGit(b, work, "remote", "add", "origin", origin)
	for i := 0; i < 1000; i++ {
		writeSnapshotSkill(b, work, fmt.Sprintf("skill-%05d", i), "benchmark catalog resource with a realistic description")
	}
	snapshotGitCommitPush(b, work, "a")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main"},
	}}}
	if _, errs := (&SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}).BuildSnapshot(context.Background(), ec); len(errs) > 0 {
		b.Fatalf("warm BuildSnapshot errors: %v", errs)
	}

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap, errs := builder.BuildSnapshot(context.Background(), ec)
		if len(errs) > 0 {
			b.Fatalf("BuildSnapshot errors: %v", errs)
		}
		if len(snap.Resources) != 1000 {
			b.Fatalf("snapshot resources = %d, want 1000", len(snap.Resources))
		}
	}
}
