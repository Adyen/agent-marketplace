package resolver

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// collectionDiamond builds root -> {a1,b1} -> s1 -> {a2,b2} -> ... -> sd, where
// sd lists one skill, so 3d+1 collections expand along 2^d include paths.
func collectionDiamond(depth int) (*config.Config, map[string][]catalog.Entry) {
	entries := []catalog.Entry{
		{Kind: ir.KindCollection, Name: "root", Resource: ir.NewCollection("root", "1.0.0", "root", nil, []string{"a1", "b1"}, nil, ir.SourceRef{})},
		{Kind: ir.KindSkill, Name: "leaf-skill"},
	}
	for i := 1; i <= depth; i++ {
		s := fmt.Sprintf("s%d", i)
		for _, side := range []string{"a", "b"} {
			name := fmt.Sprintf("%s%d", side, i)
			entries = append(entries, catalog.Entry{Kind: ir.KindCollection, Name: name, Resource: ir.NewCollection(name, "1.0.0", name, nil, []string{s}, nil, ir.SourceRef{})})
		}
		var members []ir.CollectionMember
		var includes []string
		if i == depth {
			members = []ir.CollectionMember{{Name: "leaf-skill", Kind: ir.KindSkill}}
		} else {
			includes = []string{fmt.Sprintf("a%d", i+1), fmt.Sprintf("b%d", i+1)}
		}
		entries = append(entries, catalog.Entry{Kind: ir.KindCollection, Name: s, Resource: ir.NewCollection(s, "1.0.0", s, members, includes, nil, ir.SourceRef{})})
	}
	cfg := &config.Config{Registries: []config.RegistryEntry{{URL: collectionsHost, Ref: "main", Collections: ks("root")}}}
	setCatalogKeys(cfg)
	return cfg, map[string][]catalog.Entry{cfg.Registries[0].CatalogKey: entries}
}

// BenchmarkResolveCollectionDiamond resolves shared nested-collection DAGs.
// Depth 16 exceeds the expansion work limit and is refused.
func BenchmarkResolveCollectionDiamond(b *testing.B) {
	for _, depth := range []int{8, 16} {
		cfg, catalogs := collectionDiamond(depth)
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = Resolve(cfg, catalogs, nil)
			}
		})
	}
}
