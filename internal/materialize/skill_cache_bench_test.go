package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// skillCacheBenchmarkFixture builds cache-backed Claude skills with support
// files. Disk I/O is in the timed path; there is no network.
func skillCacheBenchmarkFixture(b *testing.B, skills, files, size int) (string, []resolver.ResolvedArtifact) {
	b.Helper()
	repoRoot, homeDir, sourceRoot := b.TempDir(), b.TempDir(), b.TempDir()
	b.Setenv("HOME", homeDir)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	resolved := make([]resolver.ResolvedArtifact, 0, skills)
	for i := range skills {
		name := fmt.Sprintf("skill-%03d", i)
		dir := filepath.Join(sourceRoot, "skills", name)
		if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
			b.Fatal(err)
		}
		skillFile := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(skillFile, []byte("---\nname: "+name+"\ndescription: Benchmark\n---\n\nbody"), 0o644); err != nil {
			b.Fatal(err)
		}
		resource := ir.NewSkill(name, "Benchmark", "body", nil, nil, ir.SourceRef{File: skillFile, Line: 1})
		for j := range files {
			rel := fmt.Sprintf("references/ref-%02d.txt", j)
			prefix := name + "/" + rel + "\n"
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(prefix+strings.Repeat("s", size-len(prefix))), 0o644); err != nil {
				b.Fatal(err)
			}
			resource.SupportFiles = append(resource.SupportFiles, rel)
		}
		resolved = append(resolved, resolver.ResolvedArtifact{
			Entry:     catalog.Entry{Name: name, Path: skillFile, Resource: resource},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetClaude},
		})
	}
	return repoRoot, resolved
}

// BenchmarkRunChangedSkillLinkCaches measures a real Run after the first
// support file of some skills changed (same size, so only content differs), or
// after the compiled caches were removed; those caches need full preparation.
func BenchmarkRunChangedSkillLinkCaches(b *testing.B) {
	const skills, files, size = 64, 2, 15 << 10
	for _, changed := range []int{1, skills, -1} {
		name := fmt.Sprintf("changed=%d", changed)
		if changed < 0 {
			name = "missing_caches"
		}
		b.Run(name, func(b *testing.B) {
			discardBenchLogs(b)
			repoRoot, resolved := skillCacheBenchmarkFixture(b, skills, files, size)
			if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
				b.Fatal(err)
			}
			compiled := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "agent-marketplace", "compiled")
			if _, err := os.Stat(compiled); err != nil {
				b.Fatalf("compiled cache root: %v", err)
			}
			generation := 0
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				generation++
				if changed < 0 {
					if err := os.RemoveAll(compiled); err != nil {
						b.Fatal(err)
					}
				}
				for _, artifact := range resolved[:max(changed, 0)] {
					path := filepath.Join(filepath.Dir(artifact.Path), "references", "ref-00.txt")
					content := fmt.Sprintf("generation %d\n", generation)
					if err := os.WriteFile(path, []byte(content+strings.Repeat("s", size-len(content))), 0o644); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()
				if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			got, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "skills", "skill-000", "references", "ref-00.txt"))
			if err != nil || changed > 0 && !strings.HasPrefix(string(got), fmt.Sprintf("generation %d\n", generation)) {
				b.Fatalf("changed support did not reach the cache: %v", err)
			}
		})
	}
}

// BenchmarkRunUnchangedSkillLinkCaches measures a repeat real Run whose
// cache-backed skills, support files and links are already current.
func BenchmarkRunUnchangedSkillLinkCaches(b *testing.B) {
	for _, shape := range []struct{ skills, files, size int }{
		{64, 0, 0},
		{64, 2, 15 << 10},
		{64, 8, 16 << 10},
	} {
		b.Run(fmt.Sprintf("skills=%d/support=%dx%dKiB", shape.skills, shape.files, shape.size>>10), func(b *testing.B) {
			discardBenchLogs(b)
			repoRoot, resolved := skillCacheBenchmarkFixture(b, shape.skills, shape.files, shape.size)
			if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
				b.Fatal(err)
			}
			installed, err := filepath.EvalSymlinks(filepath.Join(repoRoot, ".claude", "skills", "skill-000"))
			if err != nil {
				b.Fatal(err)
			}
			if shape.files > 0 {
				if _, err := os.Stat(filepath.Join(installed, "references", "ref-00.txt")); err != nil {
					b.Fatalf("cached support missing: %v", err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
