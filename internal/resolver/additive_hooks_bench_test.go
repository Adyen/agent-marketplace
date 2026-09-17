package resolver

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkResolveAdditiveHookSources(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	b.Cleanup(func() { slog.SetDefault(previous) })
	for _, throughCollections := range []bool{false, true} {
		name := "direct"
		if throughCollections {
			name = "collections"
		}
		b.Run(name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := make(map[string][]catalog.Entry)
			for source := 0; source < 20; source++ {
				url := fmt.Sprintf("git@example.com:registry-%02d.git", source)
				entry := config.RegistryEntry{URL: url, Ref: "main", Source: config.RepoCommitted, Default: true}
				if throughCollections {
					entry.Collections = config.KindSelector{Include: []string{fmt.Sprintf("guards-%02d", source)}}
				}
				cfg.Registries = append(cfg.Registries, entry)
			}
			setCatalogKeys(cfg)
			for source, entry := range cfg.Registries {
				var members []ir.CollectionMember
				for index := 0; index < 10; index++ {
					name := fmt.Sprintf("PostToolUse-command-%02d", index)
					resource := ir.NewHook(name, "PostToolUse", fmt.Sprintf("echo %d-%d", source, index), "command", nil, ir.SourceRef{})
					catalogs[entry.CatalogKey] = append(catalogs[entry.CatalogKey], catalog.Entry{Kind: ir.KindHook, Name: name, Resource: resource})
					members = append(members, ir.CollectionMember{Kind: ir.KindHook, Name: name})
				}
				if throughCollections {
					name := fmt.Sprintf("guards-%02d", source)
					resource := ir.NewCollection(name, "", "", members, nil, nil, ir.SourceRef{})
					catalogs[entry.CatalogKey] = append(catalogs[entry.CatalogKey], catalog.Entry{Kind: ir.KindCollection, Name: name, Resource: resource})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				Resolve(cfg, catalogs, nil)
			}
		})
	}
}
