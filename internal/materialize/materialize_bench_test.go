package materialize

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// discardBenchLogs keeps log-handler cost out of the measurement so that
// symlink-mode and file-mode numbers stay comparable.
func discardBenchLogs(b *testing.B) {
	b.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
}

func BenchmarkBuildWorkItemsSelectionPolicy(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(b.TempDir(), "cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	repoRoot := b.TempDir()
	resolved := make([]resolver.ResolvedArtifact, 0, 101)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("resource-%03d", i)
		resource := ir.NewSkill(name, "benchmark", "body", nil, nil, ir.SourceRef{})
		resolved = append(resolved, resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name, Resource: resource}, RegistryURL: "https://example.invalid/registry.git", Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}})
	}
	collision := ir.NewSkill("resource-050", "benchmark", "preferred", nil, nil, ir.SourceRef{})
	resolved = append(resolved, resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "resource-050", Resource: collision}, RegistryURL: "https://example.invalid/registry.git", Source: config.RepoCommitted, FilePrecedence: 2, Harnesses: []hcc.Target{hcc.TargetFactory}})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buildWorkItems(resolved, repoRoot)
	}
}

func BenchmarkAliasedMarketplaceSkillDryRunDiskIONoNetwork(b *testing.B) {
	discardBenchLogs(b)
	b.Setenv("HOME", b.TempDir())
	repo := b.TempDir()
	canonical := ir.NewSkill("original", "Original", "body", nil, nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{
		Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "review-copy", Resource: canonical},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
		IsMarketplace: true, MarketplaceResourceName: "original", InstalledName: "review-copy",
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report, err := Run(context.Background(), repo, resolved, WithDryRun())
		if err != nil || report == nil {
			b.Fatalf("dry-run Marketplace alias: report=%v err=%v", report, err)
		}
	}
}

// BenchmarkRunSupportFiles measures the materialize.Run hot-path cost
// when skills have support files that must be copied into the cache.
func BenchmarkRunSupportFiles(b *testing.B) {
	discardBenchLogs(b)
	b.Setenv("HOME", b.TempDir())

	sourceSkillDir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceSkillDir, "scripts"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("---\nname: bench-skill\ndescription: benchmark\n---\n\nBody text"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "helper.sh"), []byte("#!/bin/bash\necho hello"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "scripts", "run.py"), []byte("print('hello')"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.Symlink("scripts", filepath.Join(sourceSkillDir, "references")); err != nil {
		b.Fatal(err)
	}

	irSkill := ir.NewSkill("bench-skill", "benchmark", "Body text", nil, nil, ir.SourceRef{File: filepath.Join(sourceSkillDir, "SKILL.md"), Line: 1})
	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "bench-skill", Path: sourceSkillDir, Resource: irSkill},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		repoRoot := b.TempDir()
		b.StartTimer()
		if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunWindsurfPortablePruneDiskIONoNetwork(b *testing.B) {
	discardBenchLogs(b)
	home, repo := b.TempDir(), b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "prune-probe", Resource: ir.NewSkill("prune-probe", "benchmark", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetWindsurf}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		installed, err := Run(b.Context(), repo, resolved, WithMaterializationModes(modes))
		if err != nil || installed.TotalWritten() == 0 {
			b.Fatalf("native install control failed: %+v %v", installed, err)
		}
		b.StartTimer()
		pruned, err := Run(b.Context(), repo, nil, WithMaterializationModes(modes))
		if err != nil || pruned.TotalRemoved() != 1 {
			b.Fatalf("native prune did not remove its owned skill: %+v %v", pruned, err)
		}
	}
}

// BenchmarkRunFileModePortable measures the portable-copy materialization path,
// including manifest ownership bookkeeping, for a representative skill tree.
func BenchmarkRunFileModePortable(b *testing.B) {
	discardBenchLogs(b)
	b.Setenv("HOME", b.TempDir())
	sourceDir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("---\nname: portable\ndescription: benchmark\n---\n\nbody"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "helper.sh"), []byte("echo portable"), 0o755); err != nil {
		b.Fatal(err)
	}
	resource := ir.NewSkill("portable", "benchmark", "body", nil, nil, ir.SourceRef{File: filepath.Join(sourceDir, "SKILL.md")})
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "portable", Path: sourceDir, Resource: resource},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}}
	modes := config.MaterializationModes{Machine: config.MaterializationModeSymlink, Project: config.MaterializationModeFile}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		repoRoot := b.TempDir()
		b.StartTimer()
		if _, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(modes)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMaterializePlans_TargetInfoLookup measures the hot-path cost of the
// TargetInfoMap-driven lookups in materializePlans, ensureSkillFrontmatter,
// and resourceInstallDirRel.
func BenchmarkMaterializePlans_TargetInfoLookup(b *testing.B) {
	artifacts := []artifact.Artifact{
		{Path: "skills/demo/SKILL.md", Bytes: []byte("body")},
		{Path: "commands/demo.md", Bytes: []byte("cmd")},
	}
	linked := map[string]bool{"skills": true}
	irSkill := ir.NewSkill("demo", "benchmark", "Body text", nil, nil, ir.SourceRef{})
	resolved := resolver.ResolvedArtifact{
		Entry: catalog.Entry{Name: "demo", Resource: irSkill},
	}

	// Drive the loop from the target registry so new targets are covered
	// automatically instead of being silently omitted.
	targets := hcc.Targets()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, target := range targets {
			_ = materializePlans(target, "/tmp/out", artifacts, linked, false)
			_ = ensureSkillFrontmatter(resolved, target, append([]artifact.Artifact(nil), artifacts...))
			_ = resourceInstallDirRel(target, ir.KindCommand)
			_ = resourceInstallDirRel(target, ir.KindSubagent)
		}
	}
}

// BenchmarkRunDryRunLargeMixedResources measures the dry-run path for
// large mixed-resource workloads across multiple harnesses.
func BenchmarkRunDryRunLargeMixedResources(b *testing.B) {
	discardBenchLogs(b)
	cases := []struct {
		name         string
		numResources int
		targets      []hcc.Target
	}{
		{"100_mixed_1_target", 100, []hcc.Target{hcc.TargetFactory}},
		{"500_mixed_3_targets", 500, []hcc.Target{hcc.TargetFactory, hcc.TargetClaude, hcc.TargetCursor}},
		{"1000_mixed_1_target", 1000, []hcc.Target{hcc.TargetFactory}},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Setenv("HOME", b.TempDir())
			sourceDir := b.TempDir()

			resolved := make([]resolver.ResolvedArtifact, c.numResources)
			for i := 0; i < c.numResources; i++ {
				var name string
				var resource ir.Resource
				var kind ir.ResourceKind
				var path string
				switch i % 5 {
				case 0:
					kind = ir.KindSkill
					name = fmt.Sprintf("skill-%04d", i)
					skillDir := filepath.Join(sourceDir, name)
					if err := os.MkdirAll(skillDir, 0o755); err != nil {
						b.Fatal(err)
					}
					sourceFile := filepath.Join(skillDir, "SKILL.md")
					body := fmt.Sprintf("---\nname: %s\ndescription: benchmark skill %d\n---\n\n# Skill %d\n", name, i, i)
					if err := os.WriteFile(sourceFile, []byte(body), 0o644); err != nil {
						b.Fatal(err)
					}
					path = skillDir
					resource = ir.NewSkill(name, fmt.Sprintf("benchmark skill %d", i), "Body text", nil, nil, ir.SourceRef{File: sourceFile, Line: 1})
				case 1:
					kind = ir.KindCommand
					name = fmt.Sprintf("command-%04d", i)
					path = fmt.Sprintf("/cache/commands/%s.md", name)
					resource = ir.NewCommand(name, fmt.Sprintf("benchmark command %d", i), "Run the benchmark command.", nil, ir.SourceRef{File: path, Line: 1})
				case 2:
					kind = ir.KindSubagent
					name = fmt.Sprintf("subagent-%04d", i)
					path = fmt.Sprintf("/cache/agents/%s.md", name)
					resource = ir.NewSubagent(name, fmt.Sprintf("benchmark subagent %d", i), "You are a benchmark subagent.", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{File: path, Line: 1})
				case 3:
					kind = ir.KindHook
					name = fmt.Sprintf("hook-%04d", i)
					path = "/cache/hooks/hooks.json"
					resource = ir.NewHook(name, "postToolUse", "echo benchmark", "command", nil, ir.SourceRef{File: path, Line: 1})
				default:
					kind = ir.KindMcpServer
					name = fmt.Sprintf("mcp-%04d", i)
					path = "/cache/.mcp.json"
					resource = ir.NewMcpServer(name, "node", []string{"server.js"}, map[string]string{"BENCH": "1"}, nil, ir.SourceRef{File: path, Line: 1})
				}
				resolved[i] = resolver.ResolvedArtifact{
					Entry:     catalog.Entry{Kind: kind, Name: name, Path: path, Resource: resource},
					Source:    config.RepoCommitted,
					Harnesses: c.targets,
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				repoRoot := b.TempDir()
				b.StartTimer()
				if _, err := Run(context.Background(), repoRoot, resolved, WithDryRun()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSyncArtifactLinks covers missing links, already-synced links,
// stale symlinks, and user-owned regular files.
func BenchmarkSyncArtifactLinks(b *testing.B) {
	b.Run("missing_links", func(b *testing.B) {
		cacheDir := b.TempDir()
		artifacts := makeArtifacts(100)

		// Pre-write cache artifacts.
		for _, a := range artifacts {
			if err := writeCacheArtifact(cacheDir, a); err != nil {
				b.Fatal(err)
			}
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testOutDir := b.TempDir()
			b.StartTimer()
			if _, _, err := syncArtifactLinks(testOutDir, artifacts, cacheDir, false, true, nil); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("already_synced", func(b *testing.B) {
		outDir := b.TempDir()
		cacheDir := b.TempDir()
		_ = outDir
		artifacts := makeArtifacts(100)

		for _, a := range artifacts {
			if err := writeCacheArtifact(cacheDir, a); err != nil {
				b.Fatal(err)
			}
		}
		// Pre-create symlinks so they are already synced.
		if _, _, err := syncArtifactLinks(outDir, artifacts, cacheDir, false, false, nil); err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := syncArtifactLinks(outDir, artifacts, cacheDir, false, false, nil); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("stale_symlinks", func(b *testing.B) {
		outDir := b.TempDir()
		cacheDir := b.TempDir()
		artifacts := makeArtifacts(50)

		for _, a := range artifacts {
			if err := writeCacheArtifact(cacheDir, a); err != nil {
				b.Fatal(err)
			}
		}
		if _, _, err := syncArtifactLinks(outDir, artifacts, cacheDir, false, false, nil); err != nil {
			b.Fatal(err)
		}
		// Remove half the artifacts, making existing links stale.
		artifacts = artifacts[:25]

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testOutDir := b.TempDir()
			// Copy the pre-synced state to the test dir.
			if err := copyDirContents(outDir, testOutDir); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, _, err := syncArtifactLinks(testOutDir, artifacts, cacheDir, false, true, nil); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("user_owned_regular_files", func(b *testing.B) {
		outDir := b.TempDir()
		cacheDir := b.TempDir()
		artifacts := makeArtifacts(25)
		for _, a := range artifacts {
			if err := writeCacheArtifact(cacheDir, a); err != nil {
				b.Fatal(err)
			}
		}
		for i := 0; i < 50; i++ {
			path := filepath.Join(outDir, fmt.Sprintf("user-owned-%04d.txt", i))
			if err := os.WriteFile(path, []byte("user data"), 0o644); err != nil {
				b.Fatal(err)
			}
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testOutDir := b.TempDir()
			if err := copyDirContents(outDir, testOutDir); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, _, err := syncArtifactLinks(testOutDir, artifacts, cacheDir, false, true, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSyncSkillLinks covers empty, already-synced, stale symlink,
// and user-owned directory cases.
func BenchmarkSyncSkillLinks(b *testing.B) {
	b.Run("empty", func(b *testing.B) {
		links := makeSkillLinks(100)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testDir := b.TempDir()
			b.StartTimer()
			if _, err := syncSkillLinks(testDir, links, false, false); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("already_synced", func(b *testing.B) {
		skillsDir := b.TempDir()
		links := makeSkillLinks(100)

		if _, err := syncSkillLinks(skillsDir, links, false, false); err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := syncSkillLinks(skillsDir, links, false, false); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("stale_symlink", func(b *testing.B) {
		skillsDir := b.TempDir()
		links := makeSkillLinks(50)

		if _, err := syncSkillLinks(skillsDir, links, false, false); err != nil {
			b.Fatal(err)
		}
		// Remove half the links, making existing ones stale.
		links = links[:25]

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testDir := b.TempDir()
			if err := copyDirContents(skillsDir, testDir); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, err := syncSkillLinks(testDir, links, false, true); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("user_owned_directory", func(b *testing.B) {
		skillsDir := b.TempDir()
		userDir := filepath.Join(skillsDir, "user-owned-skill")
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(userDir, "SKILL.md"), []byte("# user skill"), 0o644); err != nil {
			b.Fatal(err)
		}
		links := makeSkillLinks(25)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testDir := b.TempDir()
			if err := copyDirContents(skillsDir, testDir); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, err := syncSkillLinks(testDir, links, false, true); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("user_owned_symlink_skip", func(b *testing.B) {
		skillsDir := b.TempDir()
		// Create user-owned symlinks that will be skipped by
		// isManagedSymlink because they don't point to the cache.
		for i := 0; i < 50; i++ {
			userLink := filepath.Join(skillsDir, fmt.Sprintf("user-skill-%04d", i))
			if err := os.Symlink(fmt.Sprintf("/custom/skills/skill-%04d", i), userLink); err != nil {
				b.Fatal(err)
			}
		}
		links := makeSkillLinks(25)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			testDir := b.TempDir()
			if err := copyDirContents(skillsDir, testDir); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, err := syncSkillLinks(testDir, links, false, true); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkEnsureSkillFrontmatter for small, 100KB, and 1MB skill bodies
// with and without existing frontmatter.
func BenchmarkEnsureSkillFrontmatter(b *testing.B) {
	cases := []struct {
		name     string
		bodySize int // bytes
		hasFM    bool
	}{
		{"small_no_fm", 100, false},
		{"small_with_fm", 100, true},
		{"100KB_no_fm", 100 * 1024, false},
		{"100KB_with_fm", 100 * 1024, true},
		{"1MB_no_fm", 1024 * 1024, false},
		{"1MB_with_fm", 1024 * 1024, true},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			body := strings.Repeat("a", c.bodySize)
			var content string
			if c.hasFM {
				content = "---\nname: bench-skill\ndescription: benchmark\n---\n\n" + body
			} else {
				content = body
			}
			artifacts := []artifact.Artifact{
				{Path: "skills/bench-skill/SKILL.md", Bytes: []byte(content)},
			}
			irSkill := ir.NewSkill("bench-skill", "benchmark", "Original body", nil, nil, ir.SourceRef{})
			resolved := resolver.ResolvedArtifact{
				Entry: catalog.Entry{Name: "bench-skill", Resource: irSkill},
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				arts := append([]artifact.Artifact(nil), artifacts...)
				_ = ensureSkillFrontmatter(resolved, hcc.TargetFactory, arts)
			}
		})
	}
}

// BenchmarkEnsureSymlinkPathUserOwnedSkip measures the cost of the
// user-owned check (isManagedSymlink) when the link path is occupied
// by a non-cache symlink that must be skipped.
func BenchmarkEnsureSymlinkPathUserOwnedSkip(b *testing.B) {
	b.Run("user_symlink_skip", func(b *testing.B) {
		dir := b.TempDir()
		linkPath := filepath.Join(dir, "link")
		target := filepath.Join(dir, "target")
		// Pre-create user-owned symlink.
		if err := os.Symlink("/some/user/path", linkPath); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := ensureSymlinkPath(linkPath, target, false)
			if !result.skippedUserOwned {
				b.Fatal("expected skippedUserOwned=true")
			}
		}
	})

	b.Run("user_directory_skip", func(b *testing.B) {
		dir := b.TempDir()
		linkPath := filepath.Join(dir, "link")
		target := filepath.Join(dir, "target")
		// Pre-create user-owned directory.
		if err := os.MkdirAll(linkPath, 0o755); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result := ensureSymlinkPath(linkPath, target, false)
			if !result.skippedUserOwned {
				b.Fatal("expected skippedUserOwned=true")
			}
		}
	})
}

// BenchmarkCopySupportFilesRecursive for skill support trees with 10, 100, 100,
// and 1000 nested files.
func BenchmarkCopySupportFilesRecursive(b *testing.B) {
	cases := []int{10, 100, 1000}

	for _, n := range cases {
		b.Run(fmt.Sprintf("%d_files", n), func(b *testing.B) {
			srcDir := b.TempDir()
			if err := createNestedFiles(srcDir, n); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				dstDir := b.TempDir()
				b.StartTimer()
				if err := copySupportFiles(srcDir, dstDir); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- helpers ---

func makeArtifacts(n int) []artifact.Artifact {
	arts := make([]artifact.Artifact, n)
	for i := 0; i < n; i++ {
		arts[i] = artifact.Artifact{
			Path:  fmt.Sprintf("artifact-%04d.txt", i),
			Bytes: []byte(fmt.Sprintf("content-%d", i)),
		}
	}
	return arts
}

func writeCacheArtifact(cacheDir string, a artifact.Artifact) error {
	destPath := filepath.Join(cacheDir, filepath.FromSlash(a.Path))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, a.Bytes, 0o644)
}

func makeSkillLinks(n int) []skillLink {
	links := make([]skillLink, n)
	for i := 0; i < n; i++ {
		links[i] = skillLink{
			name:   fmt.Sprintf("skill-%04d", i),
			target: fmt.Sprintf("/cache/compiled/skill-%04d", i),
		}
	}
	return links
}

func createNestedFiles(dir string, n int) error {
	dirsPerLevel := 3
	if n > 100 {
		dirsPerLevel = 10
	}
	created := 0
	for created < n {
		subDir := filepath.Join(dir, fmt.Sprintf("d-%d", created%dirsPerLevel))
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			return err
		}
		fileName := filepath.Join(subDir, fmt.Sprintf("f-%d.txt", created))
		if err := os.WriteFile(fileName, []byte("data"), 0o644); err != nil {
			return err
		}
		created++
	}
	return nil
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return err
			}
			continue
		}
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
