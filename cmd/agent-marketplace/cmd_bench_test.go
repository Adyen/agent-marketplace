package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkBuildListRows measures the cost of building list rows from
// resolved artifacts and catalogs.
func BenchmarkBuildListRows(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
	}{
		{"5reg_50skills", 5, 50},
		{"10reg_100skills", 10, 100},
		{"20reg_200skills", 20, 200},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			cfg := &config.Config{}
			catalogs := make(map[string]catalog.LoadedCatalog)
			var resolved []resolver.ResolvedArtifact

			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				key := cache.RegistryCachePath(url, ref)
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{
					URL:        url,
					Ref:        ref,
					Source:     config.RepoCommitted,
					CatalogKey: key,
				})

				entries := make([]catalog.Entry, c.perReg)
				for s := 0; s < c.perReg; s++ {
					name := fmt.Sprintf("reg%02d-skill-%03d", r, s)
					entries[s] = catalog.Entry{
						Kind: kindForIdx(s),
						Name: name,
						Tags: []string{fmt.Sprintf("tag-%d", s%5)},
						Path: fmt.Sprintf("/cache/reg%02d/%s", r, name),
					}
					resolved = append(resolved, resolver.ResolvedArtifact{
						Entry:       entries[s],
						RegistryURL: url,
						CatalogKey:  key,
					})
				}
				catalogs[key] = catalog.LoadedCatalog{Entries: entries}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = buildListRows(cfg, catalogs, resolved)
			}
		})
	}
}

// BenchmarkBuildStatusRowsAndTables measures the cost of building status rows
// from resolved artifacts.
func BenchmarkBuildStatusRowsAndTables(b *testing.B) {
	cases := []struct {
		name       string
		registries int
		perReg     int
	}{
		{"5reg_50skills", 5, 50},
		{"10reg_100skills", 10, 100},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			repoRoot := b.TempDir()
			resolved := make([]resolver.ResolvedArtifact, 0)
			cacheFreshness := make(map[string]string)

			for r := 0; r < c.registries; r++ {
				url := fmt.Sprintf("git@example.com:reg-%02d.git", r)
				ref := "main"
				key := cache.RegistryCachePath(url, ref)
				cacheFreshness[key] = "fresh"

				for s := 0; s < c.perReg; s++ {
					resolved = append(resolved, resolver.ResolvedArtifact{
						Entry: catalog.Entry{
							Kind: kindForIdx(s),
							Name: fmt.Sprintf("reg%02d-skill-%03d", r, s),
							Path: fmt.Sprintf("/cache/reg%02d/skill-%03d", r, s),
						},
						RegistryURL: url,
						Ref:         ref,
						Source:      config.RepoCommitted,
						CatalogKey:  key,
					})
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = buildStatusRows(repoRoot, resolved, cacheFreshness, nil)
			}
		})
	}
}

// BenchmarkRunSyncCheckLocalRegistry measures the sync --check path with a
// local registry.
func BenchmarkRunSyncCheckLocalRegistry(b *testing.B) {
	cases := []struct {
		name      string
		numSkills int
	}{
		{"10_skills", 10},
		{"50_skills", 50},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Setenv("HOME", b.TempDir())

			repoRoot := b.TempDir()
			agentsDir := filepath.Join(repoRoot, ".agents")
			if err := os.MkdirAll(agentsDir, 0o755); err != nil {
				b.Fatal(err)
			}

			localSkillDir := filepath.Join(repoRoot, "local-skills")
			if err := os.MkdirAll(localSkillDir, 0o755); err != nil {
				b.Fatal(err)
			}
			for s := 0; s < c.numSkills; s++ {
				skillDir := filepath.Join(localSkillDir, fmt.Sprintf("skill-%04d", s))
				if err := os.MkdirAll(skillDir, 0o755); err != nil {
					b.Fatal(err)
				}
				body := fmt.Sprintf("---\nname: skill-%04d\ndescription: bench skill %d\n---\n\n# Skill %d\n", s, s, s)
				if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}

			userAgentsDir := filepath.Join(os.Getenv("HOME"), ".agents")
			if err := os.MkdirAll(userAgentsDir, 0o755); err != nil {
				b.Fatal(err)
			}
			userConfigYAML := "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"
			if err := os.WriteFile(filepath.Join(userAgentsDir, "config.yaml"), []byte(userConfigYAML), 0o644); err != nil {
				b.Fatal(err)
			}

			configYAML := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - path: %s\n    default: true\n", localSkillDir)
			if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(configYAML), 0o644); err != nil {
				b.Fatal(err)
			}

			origDir, err := os.Getwd()
			if err != nil {
				b.Fatalf("get working directory: %v", err)
			}
			if err := os.Chdir(repoRoot); err != nil {
				b.Fatalf("chdir to repo root %s: %v", repoRoot, err)
			}
			b.Cleanup(func() {
				if err := os.Chdir(origDir); err != nil {
					b.Fatalf("restore working directory %s: %v", origDir, err)
				}
			})

			var buf bytes.Buffer
			err = runSyncCheck(context.Background(), &buf)
			if err == nil || !strings.Contains(err.Error(), "out of sync") {
				b.Fatalf("runSyncCheck error = %v, want out of sync", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				if err := runSyncCheck(context.Background(), &buf); err == nil {
					b.Fatal("runSyncCheck error = nil, want out of sync")
				}
			}
		})
	}
}

func BenchmarkRemoveNamedFromWarmGit(b *testing.B) {
	homeDir := b.TempDir()
	b.Setenv("HOME", homeDir)
	origin, working := createBareRepoWithWorkingClone(b, "remove-bench.git")
	writeSkill(b, working, "bench-skill", "Benchmark skill", nil)
	commitAndPushAll(b, working, "setup")

	repoRoot := b.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	configBody := `
registries:
  - url: ` + origin + `
    ref: main
    skills:
      - bench-skill
`
	writeConfig(b, configPath, configBody)
	withWorkingDir(b, repoRoot)
	if out, err := executeCommand("sync"); err != nil {
		b.Fatalf("warm sync failed: %v\n%s", err, out)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		writeConfig(b, configPath, configBody)
		b.StartTimer()
		if out, err := executeCommand("remove", "bench-skill", "--from", origin); err != nil {
			b.Fatalf("remove failed: %v\n%s", err, out)
		}
	}
}

func kindForIdx(s int) ir.ResourceKind {
	v := s % 5
	switch v {
	case 1:
		return ir.KindCommand
	case 2:
		return ir.KindSubagent
	case 3:
		return ir.KindHook
	case 4:
		return ir.KindMcpServer
	default:
		return ir.KindSkill
	}
}

// BenchmarkDescribeCacheFreshness measures the cache-freshness lookup path
// used by agent-marketplace status (hot path).
func BenchmarkDescribeCacheFreshness(b *testing.B) {
	b.Setenv("HOME", b.TempDir())

	base := cache.RegistryCachePath("git@example.com:repo.git", "main")
	if err := os.MkdirAll(base, 0o755); err != nil {
		b.Fatal(err)
	}
	keyWithSuffix := base + ":" + cache.HashURL("skills")
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = describeCacheFreshness(keyWithSuffix, now)
		_ = describeCacheFreshness(base, now)
	}
}

// BenchmarkRunDoctor measures the whole non-repair doctor report. doctor runs
// interactively, so per the repository's performance policy it earns a
// benchmark the first time it is extended. The Devin version probe is injected
// rather than executed: measuring a third-party child process would measure
// that binary, not this code.
//
// The git-root memo is cleared at the top of every iteration so each iteration
// models one fresh `agent-marketplace doctor` process, which starts with an
// empty memo. Without the reset the benchmark would report a warm-cache cost
// no real invocation ever pays.
func BenchmarkRunDoctor(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(b.TempDir(), "cache"))

	repoRoot := b.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		b.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		b.Fatalf("chdir to benchmark repo root: %v", err)
	}
	b.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			b.Fatalf("restore working directory: %v", err)
		}
	})

	probe := devincli.Probe{
		Status:  devincli.StatusSupported,
		Path:    filepath.Join(home, "bin", "devin"),
		Version: devincli.Version{Major: devincli.FloorMajor, Minor: devincli.FloorMinor, Patch: 27},
	}
	opts := doctorOptions{
		out:   io.Discard,
		probe: func(context.Context) devincli.Probe { return probe },
	}
	// Route slog to io.Discard: the benchmark measures doctor, not the
	// default text handler writing to the test's stderr.
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })

	b.Cleanup(config.ResetRepoRootCache)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config.ResetRepoRootCache()
		if err := runDoctor(opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildHarnessTargetRows measures the status HARNESS TARGETS section,
// which runs on every `status` invocation.
func BenchmarkBuildHarnessTargetRows(b *testing.B) {
	harnesses := []string{"factory", "windsurf", "devin", "claude", "cursor", "gemini"}
	b.Setenv("HOME", b.TempDir())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := buildHarnessTargetRows("/repo", harnesses)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != len(harnesses) {
			b.Fatalf("unexpected row count %d", len(rows))
		}
	}
}

func BenchmarkBuildCodexHarnessTargetRows(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("CODEX_HOME", filepath.Join(b.TempDir(), "codex-home"))

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rows, err := buildHarnessTargetRows("/repo", []string{"codex"})
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != 1 {
			b.Fatalf("unexpected row count %d", len(rows))
		}
	}
}

func BenchmarkRunConvertToCodex(b *testing.B) {
	b.Setenv("CODEX_HOME", "")
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	srcDir := b.TempDir()
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name":"benchmark","version":"1.0.0"}`,
		"skills/demo/SKILL.md":       "---\nname: demo\ndescription: Benchmark skill\n---\n\n# Demo\n",
		"hooks/hooks.json":           `{"hooks":{"PreToolUse":[{"type":"command","command":"lint"}]}}`,
		".mcp.json":                  `{"mcpServers":{"docs":{"command":"docs-server","args":["--stdio"]}}}`,
	}
	for name, content := range files {
		path := filepath.Join(srcDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	outRoot := b.TempDir()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outDir := filepath.Join(outRoot, fmt.Sprintf("run-%d", i))
		if err := runConvert(io.Discard, srcDir, "claude", "codex", outDir, false, true, false); err != nil {
			b.Fatal(err)
		}
	}
}
