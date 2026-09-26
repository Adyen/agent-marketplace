package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkTUIConfigTransactionDiskIONoNetwork(b *testing.B) {
	for _, enable := range []bool{true, false} {
		b.Run(fmt.Sprintf("enable=%t", enable), func(b *testing.B) {
			repo := setupLocalGitSyncBenchmark(b, 4)
			runSyncBenchmark(b)
			path := config.ProjectConfigPath(repo)
			cfg, err := config.LoadConfigFromPath(path)
			if err != nil {
				b.Fatal(err)
			}
			entry := cfg.Registries[0]
			mutate := func(enable bool) {
				b.Helper()
				err := runTUIConfigMutation(context.Background(), repo, func(mapper pipeline.ConfigPathMapper) error {
					staged, err := mapper(path)
					if err != nil {
						return err
					}
					return pipeline.EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: staged, DeclaringPath: path}, func(m *pipeline.LayerMutation) error {
						binding, err := m.EnsureRegistry(entry)
						if err != nil {
							return err
						}
						op := config.SelectionExclude
						if enable {
							op = config.SelectionInclude
						}
						return m.Apply(binding, config.SelectionChange{Operation: op, Locators: []config.ResourceLocator{{Kind: ir.KindSkill, Name: "review-0"}}})
					})
				})
				if err != nil {
					b.Fatal(err)
				}
			}
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			before, err := os.ReadFile(lockPath)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				mutate(!enable)
				b.StartTimer()
				mutate(enable)
			}
			b.StopTimer()
			after, err := os.ReadFile(lockPath)
			if err != nil || string(before) != string(after) {
				b.Fatalf("config-only toggle changed Git lock: %v", err)
			}
			cfg, err = config.LoadConfigFromPath(path)
			if err != nil || len(cfg.Registries) != 4 {
				b.Fatalf("reload toggled configuration: %v", err)
			}
			if selected := cfg.Registries[0].Skills; slices.Contains(selected.Include, "review-0") != enable || slices.Contains(selected.Exclude, "review-0") == enable {
				b.Fatalf("toggle enable=%t did not persist selection: %+v", enable, selected)
			}
			if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review-0", "SKILL.md")); err != nil {
				b.Fatalf("config-only toggle changed installed output: %v", err)
			}
		})
	}
}
