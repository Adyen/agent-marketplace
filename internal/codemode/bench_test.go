package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
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
