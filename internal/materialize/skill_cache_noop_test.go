package materialize

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type cacheEntryState struct {
	inode uint64
	mtime time.Time
	mode  os.FileMode
}

// snapshotCacheTree records every entry below root, after pinning modification
// times to a fixed past instant so any later rewrite is observable.
func snapshotCacheTree(t *testing.T, root string, pin bool) map[string]cacheEntryState {
	t.Helper()
	past := time.Unix(1_000_000_000, 0)
	states := make(map[string]cacheEntryState)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if pin && info.Mode().IsRegular() {
			if err := os.Chtimes(path, past, past); err != nil {
				return err
			}
			if info, err = os.Lstat(path); err != nil {
				return err
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		states[filepath.ToSlash(rel)] = cacheEntryState{inode: info.Sys().(*syscall.Stat_t).Ino, mtime: info.ModTime(), mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func writeSkillCacheSource(t *testing.T, dir string) {
	t.Helper()
	for rel, content := range map[string]string{
		"SKILL.md":                  "source skill",
		"scripts/run.sh":            "#!/bin/sh\necho run\n",
		"references/deep/guide.md":  "guide",
		"references/deep/table.csv": "a,b\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if filepath.Ext(rel) == ".sh" {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

func countSupportStages(t *testing.T) *int {
	t.Helper()
	stages := 0
	supportStageCopyHook = func(string) { stages++ }
	t.Cleanup(func() { supportStageCopyHook = nil })
	return &stages
}

func assertCacheMatchesDesired(t *testing.T, link skillLink, cacheDir string) {
	t.Helper()
	desired, err := portableSkillVirtualDigest(link)
	if err != nil {
		t.Fatalf("desired digest: %v", err)
	}
	actual, err := portablePathDigest(cacheDir)
	if err != nil || actual != desired {
		t.Fatalf("cache tree %q differs from desired %q: %v", actual, desired, err)
	}
}

// spec: materialization/unchanged-skill-cache-support-repeats-without-mutation
func TestPrepareSkillLinkCacheRepeatsUnchangedSupportWithoutMutation(t *testing.T) {
	sourceDir, cacheDir := t.TempDir(), t.TempDir()
	writeSkillCacheSource(t, sourceDir)
	link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
	stages := countSupportStages(t)
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("initial preparation: %v", err)
	}
	if *stages != 1 {
		t.Fatalf("initial preparation staged support %d times, want 1", *stages)
	}
	before := snapshotCacheTree(t, cacheDir, true)
	if len(before) != 8 {
		t.Fatalf("prepared cache entries = %v, want root, SKILL.md, three directories and three support files", before)
	}

	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("unchanged preparation: %v", err)
	}
	if *stages != 1 {
		t.Fatalf("unchanged preparation staged support again (%d stages)", *stages)
	}
	after := snapshotCacheTree(t, cacheDir, false)
	if len(after) != len(before) {
		t.Fatalf("unchanged preparation changed cache membership: before=%v after=%v", before, after)
	}
	for rel, state := range before {
		if after[rel] != state {
			t.Errorf("unchanged preparation changed %s: before=%+v after=%+v", rel, state, after[rel])
		}
	}
	assertCacheMatchesDesired(t, link, cacheDir)

	// Positive control: a real source change still reaches the cache.
	if err := os.WriteFile(filepath.Join(sourceDir, "references", "deep", "guide.md"), []byte("updated guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("changed preparation: %v", err)
	}
	if *stages != 2 {
		t.Fatalf("changed source did not stage support (%d stages)", *stages)
	}
	if got, err := os.ReadFile(filepath.Join(cacheDir, "references", "deep", "guide.md")); err != nil || string(got) != "updated guide" {
		t.Fatalf("changed support = %q, %v", got, err)
	}
	assertCacheMatchesDesired(t, link, cacheDir)
}

// spec: materialization/skill-cache-preparation-reconciles-support-drift
func TestPrepareSkillLinkCacheReconcilesSupportDrift(t *testing.T) {
	for _, tc := range []struct {
		name  string
		drift func(t *testing.T, sourceDir, cacheDir string)
	}{
		{"source bytes", func(t *testing.T, sourceDir, _ string) {
			mustWrite(t, filepath.Join(sourceDir, "scripts", "run.sh"), "#!/bin/sh\necho changed\n", 0o755)
		}},
		{"source mode", func(t *testing.T, sourceDir, _ string) {
			mustChmod(t, filepath.Join(sourceDir, "references", "deep", "guide.md"), 0o600)
		}},
		{"added source file", func(t *testing.T, sourceDir, _ string) {
			mustWrite(t, filepath.Join(sourceDir, "references", "new.md"), "new", 0o644)
		}},
		{"removed source file", func(t *testing.T, sourceDir, _ string) {
			if err := os.Remove(filepath.Join(sourceDir, "references", "deep", "table.csv")); err != nil {
				t.Fatal(err)
			}
		}},
		{"tampered cache bytes", func(t *testing.T, _, cacheDir string) {
			mustWrite(t, filepath.Join(cacheDir, "references", "deep", "guide.md"), "tampered", 0o644)
		}},
		{"tampered cache mode", func(t *testing.T, _, cacheDir string) {
			mustChmod(t, filepath.Join(cacheDir, "scripts", "run.sh"), 0o644)
		}},
		{"missing cache file", func(t *testing.T, _, cacheDir string) {
			if err := os.Remove(filepath.Join(cacheDir, "references", "deep", "table.csv")); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra cache file", func(t *testing.T, _, cacheDir string) {
			mustWrite(t, filepath.Join(cacheDir, "references", "stale.md"), "stale", 0o644)
		}},
		{"extra cache directory", func(t *testing.T, _, cacheDir string) {
			if err := os.Mkdir(filepath.Join(cacheDir, "obsolete"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"cache file replaced by link", func(t *testing.T, sourceDir, cacheDir string) {
			path := filepath.Join(cacheDir, "references", "deep", "guide.md")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(sourceDir, "references", "deep", "guide.md"), path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceDir, cacheDir := t.TempDir(), t.TempDir()
			writeSkillCacheSource(t, sourceDir)
			link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
			stages := countSupportStages(t)
			if err := prepareSkillLinkCache(link); err != nil {
				t.Fatalf("initial preparation: %v", err)
			}
			tc.drift(t, sourceDir, cacheDir)
			if err := prepareSkillLinkCache(link); err != nil {
				t.Fatalf("reconciling preparation: %v", err)
			}
			if *stages != 2 {
				t.Fatalf("drift was not reconciled through staged support (%d stages)", *stages)
			}
			assertCacheMatchesDesired(t, link, cacheDir)
		})
	}
}

// spec: materialization/skill-cache-preparation-reconciles-support-drift
func TestPrepareSkillLinkCacheRequiresDeclaredSupportFromCapturedWalk(t *testing.T) {
	sourceDir, cacheDir := t.TempDir(), t.TempDir()
	writeSkillCacheSource(t, sourceDir)
	// The support walk never copies VCS internals, so this declared file exists
	// on disk but can never reach the cache.
	mustWrite(t, filepath.Join(sourceDir, "scripts", ".git", "config"), "internal", 0o644)
	link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
	stages := countSupportStages(t)
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("initial preparation: %v", err)
	}
	before, err := portablePathDigest(cacheDir)
	if err != nil {
		t.Fatal(err)
	}

	declared := link
	declared.declaredSupport = []string{"scripts/run.sh", "scripts/.git/config"}
	if err := prepareSkillLinkCache(declared); err == nil {
		t.Fatal("declared support that the captured walk never copies must refuse like full preparation")
	}
	if after, err := portablePathDigest(cacheDir); err != nil || after != before {
		t.Fatalf("refused preparation changed the installed cache: %v", err)
	}

	// Control: declared support the walk does copy keeps the unchanged no-op.
	declared.declaredSupport = []string{"scripts/run.sh", "references/deep/guide.md"}
	staged := *stages
	if err := prepareSkillLinkCache(declared); err != nil {
		t.Fatalf("unchanged preparation with walked declared support: %v", err)
	}
	if *stages != staged {
		t.Fatalf("unchanged preparation with walked declared support staged again (%d -> %d)", staged, *stages)
	}
}

// spec: materialization/skill-cache-preparation-reconciles-support-drift
func TestPrepareSkillLinkCacheFallsBackWhenComparedCacheIsReplaced(t *testing.T) {
	sourceDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillCacheSource(t, sourceDir)
	link := skillLink{sourceDir: sourceDir, cacheDir: cacheDir, artifacts: []artifact.Artifact{{Path: "SKILL.md", Bytes: []byte("compiled skill")}}}
	stages := countSupportStages(t)
	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("initial preparation: %v", err)
	}
	compared := cacheDir + ".compared"
	replaced := false
	skillLinkCacheCompareHook = func(dir string) {
		if replaced {
			return
		}
		replaced = true
		if err := os.Rename(dir, compared); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(dir, "foreign.md"), "foreign", 0o644)
	}
	t.Cleanup(func() { skillLinkCacheCompareHook = nil })

	if err := prepareSkillLinkCache(link); err != nil {
		t.Fatalf("preparation after replacement: %v", err)
	}
	if !replaced {
		t.Fatal("unchanged comparison never reached the replacement point")
	}
	if *stages != 2 {
		t.Fatalf("replaced cache was accepted without full preparation (%d stages)", *stages)
	}
	assertCacheMatchesDesired(t, link, cacheDir)
	assertCacheMatchesDesired(t, link, compared)
}

// spec: materialization/unchanged-skill-cache-support-repeats-without-mutation
func TestRunRepeatSymlinkSkillPreservesCachedSupportIdentity(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	sourceDir := filepath.Join(t.TempDir(), "snapshot", "skills", "review")
	writeSkillCacheSource(t, sourceDir)
	mustWrite(t, filepath.Join(sourceDir, "SKILL.md"), "---\nname: review\ndescription: Review\n---\n\nbody", 0o644)
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{File: filepath.Join(sourceDir, "SKILL.md"), Line: 1})
	resource.SupportFiles = []string{"references/deep/guide.md", "references/deep/table.csv", "scripts/run.sh"}
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "review", Path: filepath.Join(sourceDir, "SKILL.md"), Resource: resource},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}}
	stages := countSupportStages(t)
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("initial run: %v", err)
	}
	installed := filepath.Join(repoRoot, ".claude", "skills", "review")
	if info, err := os.Lstat(installed); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected cache-backed skill link, info=%v err=%v", info, err)
	}
	cacheDir, err := filepath.EvalSymlinks(installed)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(cacheDir, "scripts", "run.sh")); err != nil || string(got) != "#!/bin/sh\necho run\n" {
		t.Fatalf("installed support = %q, %v", got, err)
	}
	before := snapshotCacheTree(t, cacheDir, true)
	linkBefore, err := os.Lstat(installed)
	if err != nil {
		t.Fatal(err)
	}
	initialStages := *stages

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("repeat run: %v", err)
	}
	for _, target := range report.PerRoot {
		if len(target.Written) != 0 || len(target.Removed) != 0 {
			t.Fatalf("unchanged repeat reported %s writes=%v removals=%v", target.Target, target.Written, target.Removed)
		}
	}
	if *stages != initialStages {
		t.Fatalf("unchanged repeat staged support (%d -> %d)", initialStages, *stages)
	}
	after := snapshotCacheTree(t, cacheDir, false)
	if len(after) != len(before) {
		t.Fatalf("unchanged repeat changed cache membership: before=%v after=%v", before, after)
	}
	for rel, state := range before {
		if after[rel] != state {
			t.Errorf("unchanged repeat changed cached %s: before=%+v after=%+v", rel, state, after[rel])
		}
	}
	if linkAfter, err := os.Lstat(installed); err != nil || !os.SameFile(linkBefore, linkAfter) || !linkAfter.ModTime().Equal(linkBefore.ModTime()) {
		t.Fatalf("unchanged repeat replaced the installed link: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	mustChmod(t, path, mode)
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
