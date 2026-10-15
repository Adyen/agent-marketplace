package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPruneStaleSourceIdentityOccurrences(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, variant := range []string{"refs", "paths", "directories"} {
		t.Run(variant, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			repoRoot := t.TempDir()
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			var sources string
			switch variant {
			case "refs":
				origin, working := createBareRepoWithWorkingClone(t, "prune-refs.git")
				writeSkill(t, working, "keeper", "Keeper", nil)
				commitAndPushAll(t, working, "Add keeper")
				runGit(t, working, "branch", "alternate")
				runGit(t, working, "push", "origin", "alternate")
				runGit(t, working, "checkout", "-b", "healthy")
				writeSkill(t, working, "gone", "Present only at the healthy ref", nil)
				commitAndPushAllToBranch(t, working, "Add healthy sibling", "healthy")
				sources = "  a_healthy: {git: 'file://" + origin + "', ref: healthy}\n" +
					"  first: {git: 'file://" + origin + "', ref: main}\n" +
					"  second: {git: 'file://" + origin + "', ref: alternate}\n"
			case "paths":
				origin, working := createBareRepoWithWorkingClone(t, "prune-paths.git")
				writeSkill(t, filepath.Join(working, "first"), "keeper", "Keeper", nil)
				writeSkill(t, filepath.Join(working, "second"), "keeper", "Keeper", nil)
				writeSkill(t, filepath.Join(working, "healthy"), "gone", "Present only at the healthy path", nil)
				commitAndPushAll(t, working, "Add three independent roots")
				sources = "  a_healthy: {git: 'file://" + origin + "', ref: main, path: healthy}\n" +
					"  first: {git: 'file://" + origin + "', ref: main, path: first}\n" +
					"  second: {git: 'file://" + origin + "', ref: main, path: second}\n"
			case "directories":
				// A directory's .git suffix is part of its name, not a Git alias.
				writeSkill(t, filepath.Join(repoRoot, "catalog"), "gone", "Healthy directory sibling", nil)
				writeSkill(t, filepath.Join(repoRoot, "catalog.git"), "keeper", "Keeper", nil)
				writeSkill(t, filepath.Join(repoRoot, "other.git"), "keeper", "Keeper", nil)
				sources = "  a_healthy: {directory: ./catalog}\n" +
					"  first: {directory: ./catalog.git}\n" +
					"  second: {directory: ./other.git}\n"
			}
			prefix := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# Keep authored source spelling.\nsources:\n" + sources + "resources:\n"
			writeFile(t, configPath, prefix+"  a_healthy:\n    include: [skill/gone]\n  first:\n    include: [skill/keeper, skill/gone]\n  second:\n    include: [skill/keeper, skill/gone]\n")
			withWorkingDir(t, repoRoot)
			output, err := executeCommand("sync")
			if err != nil {
				t.Fatalf("prime sync: %v\n%s", err, output)
			}
			assertFactorySkillExists(t, repoRoot, "keeper")
			assertFactorySkillExists(t, repoRoot, "gone")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			var lockBefore []byte
			var lockInfo os.FileInfo
			if variant != "directories" {
				lockBefore, lockInfo = statSnapshot(t, lockPath)
			}
			healthyPath := filepath.Join(repoRoot, ".factory", "skills", "gone", "SKILL.md")
			healthyBefore, healthyInfo := statSnapshot(t, healthyPath)
			healthyLink, err := os.Lstat(filepath.Dir(healthyPath))
			if err != nil {
				t.Fatal(err)
			}

			output, err = executeCommand("sync", "--prune-stale")
			if err != nil {
				t.Fatalf("prune distinct source occurrences: %v\n%s", err, output)
			}
			if strings.Count(output, `pruned stale skill "gone" from config`) != 2 || !strings.Contains(output, "Pruned 2 stale selection(s); re-syncing...") {
				t.Fatalf("expected both absent requests pruned exactly once:\n%s", output)
			}
			want := prefix + "  a_healthy:\n    include: [skill/gone]\n  first:\n    include: [skill/keeper]\n  second:\n    include: [skill/keeper]\n"
			got, err := os.ReadFile(configPath)
			if err != nil || string(got) != want {
				t.Fatalf("pruned config =\n%s\nwant:\n%s\nread error: %v", got, want, err)
			}
			assertFactorySkillExists(t, repoRoot, "keeper")
			assertStatUnchanged(t, "healthy sibling prune", healthyPath, healthyBefore, healthyInfo)
			if lockInfo != nil {
				assertStatUnchanged(t, "per-ref/path locks", lockPath, lockBefore, lockInfo)
			} else if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
				t.Fatalf("directory-only prune created a lock: %v", err)
			}
			configBefore, configInfo := statSnapshot(t, configPath)
			output, err = executeCommand("sync", "--prune-stale")
			if err != nil || strings.Contains(output, "pruned stale") {
				t.Fatalf("repeat prune was not a no-op: %v\n%s", err, output)
			}
			assertStatUnchanged(t, "repeat prune", configPath, configBefore, configInfo)
			assertStatUnchanged(t, "repeat healthy sibling", healthyPath, healthyBefore, healthyInfo)
			linkAfter, err := os.Lstat(filepath.Dir(healthyPath))
			if err != nil || !os.SameFile(healthyLink, linkAfter) || healthyLink.Mode() != linkAfter.Mode() || !healthyLink.ModTime().Equal(linkAfter.ModTime()) {
				t.Fatalf("prune replaced or changed the healthy sibling's installation: %v", err)
			}
			if lockInfo != nil {
				assertStatUnchanged(t, "repeat lock", lockPath, lockBefore, lockInfo)
			}
		})
	}
}

func TestPruneStaleSourceIdentityDuplicateWarnings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, configPath := pruneFixture(t, "duplicate-prune.git", func(origin string) string {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  main: {git: 'file://" + origin + "', ref: main}\nresources:\n  main:\n    include: [skill/keeper, skill/gone]\n"
	})
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("prime sync: %v\n%s", err, output)
	}
	cfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil || len(cfg.Registries) != 1 {
		t.Fatalf("load source identity: %v", err)
	}
	entry := cfg.Registries[0]
	warning := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "gone", URL: entry.URL, Ref: entry.Ref, Path: entry.Path}
	duplicate := *warning
	duplicate.Hint = "a repeated resolver warning does not own another selection"
	stale := []*resolver.SelectedNotFoundError{warning, &duplicate, warning}
	var out bytes.Buffer
	pruned, err := prunePhase(context.Background(), &out, repoRoot, &pipeline.SyncResult{}, stale)
	if err != nil || pruned != 1 || strings.Count(out.String(), `pruned stale skill "gone" from config`) != 1 || strings.Contains(out.String(), "prune failed") {
		t.Fatalf("duplicate warnings must remove one request: pruned=%d err=%v\n%s", pruned, err, &out)
	}
	v4, err := config.LoadV4ConfigFromPath(configPath)
	if err != nil || len(v4.Groups["main"].Include) != 1 || v4.Groups["main"].Include[0].Locator != "skill/keeper" {
		t.Fatalf("duplicate prune did not retain only keeper: %v, %v", v4, err)
	}
	configBefore, configInfo := statSnapshot(t, configPath)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	lockBefore, lockInfo := statSnapshot(t, lockPath)
	output, err = executeCommand("sync", "--prune-stale")
	if err != nil || strings.Contains(output, "pruned stale") {
		t.Fatalf("duplicate-warning follow-up was not a no-op: %v\n%s", err, output)
	}
	assertStatUnchanged(t, "duplicate-warning follow-up", configPath, configBefore, configInfo)
	assertStatUnchanged(t, "duplicate-warning lock", lockPath, lockBefore, lockInfo)
	assertFactorySkillExists(t, repoRoot, "keeper")
}

func TestPruneStaleSourceIdentityNonRootSelections(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	repoRoot := t.TempDir()
	registry := filepath.Join(repoRoot, "catalog")
	writeSkill(t, registry, "keeper", "Keeper", nil)
	writeCollection(t, registry, "bundle", "apiVersion: agent-marketplace/v1\nkind: Collection\nname: bundle\nversion: 1.0.0\ndescription: Keep the collection request\nskills:\n  - name: gone-via\n")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	fragmentPath := filepath.Join(repoRoot, ".agents", "resources.yaml")
	userPath := filepath.Join(home, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: resources.yaml\nsources:\n  main: {directory: ./catalog}\nresources:\n  main:\n    include: [skill/keeper, collection/bundle]\n")
	writeFile(t, fragmentPath, "resources:\n  main:\n    include: [skill/gone-fragment]\n")
	writeFile(t, userPath, "apiVersion: agent-marketplace/v4\nsources:\n  personal: {directory: "+registry+"}\nresources:\n  personal:\n    include: [skill/gone-user]\n")
	writeFile(t, localPath, "apiVersion: agent-marketplace/v4\nsources:\n  personal: {directory: ./catalog}\nresources:\n  personal:\n    include: [skill/gone-local]\n")
	withWorkingDir(t, repoRoot)
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("prime non-root selections: %v\n%s", err, output)
	}
	paths := []string{configPath, fragmentPath, userPath, localPath, filepath.Join(repoRoot, ".factory", "skills", "keeper", "SKILL.md")}
	before := make([][]byte, len(paths))
	infos := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		before[i], infos[i] = statSnapshot(t, path)
	}
	output, err = executeCommand("sync", "--prune-stale")
	if err != nil || strings.Contains(output, "pruned stale") || strings.Contains(output, "re-syncing...") {
		t.Fatalf("prune must not claim non-root or collection requests: %v\n%s", err, output)
	}
	for _, name := range []string{"gone-fragment", "gone-user", "gone-local", "gone-via", "include-owned"} {
		if !strings.Contains(output, name) {
			t.Errorf("expected retained selection/ownership warning %q:\n%s", name, output)
		}
	}
	for i, path := range paths {
		assertStatUnchanged(t, "non-root selection", path, before[i], infos[i])
	}
}

func TestPruneStaleSourceIdentityUntrustedCatalog(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, configPath := pruneFixture(t, "untrusted-prune.git", func(origin string) string {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  first: {git: 'file://" + origin + "', ref: main, path: skills}\n  second: {git: 'file://" + origin + "', ref: main}\nresources:\n  first:\n    include: [skill/keeper, skill/gone]\n  second:\n    include: [skill/gone]\n"
	})
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("prime sync: %v\n%s", err, output)
	}
	configBefore, configInfo := statSnapshot(t, configPath)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	lockBefore, lockInfo := statSnapshot(t, lockPath)
	keeperPath := filepath.Join(repoRoot, ".factory", "skills", "keeper", "SKILL.md")
	keeperBefore, keeperInfo := statSnapshot(t, keeperPath)
	cfg, err := config.LoadRootConfigFromPath(configPath)
	if err != nil || len(cfg.Registries) != 2 {
		t.Fatalf("load source identities: %v", err)
	}
	var stale []*resolver.SelectedNotFoundError
	for _, entry := range cfg.Registries {
		stale = append(stale, &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "gone", URL: entry.URL, Ref: entry.Ref, Path: entry.Path})
	}
	stale = append(stale, stale[0], stale[1])
	var out bytes.Buffer
	pruned, err := prunePhase(context.Background(), &out, repoRoot, &pipeline.SyncResult{CatalogTrustErrors: []error{io.ErrUnexpectedEOF}}, stale)
	if err != nil || pruned != 0 || !strings.Contains(out.String(), "2 stale selection(s) were kept") {
		t.Fatalf("untrusted catalog pruned or miscounted distinct requests: pruned=%d err=%v\n%s", pruned, err, &out)
	}
	assertStatUnchanged(t, "untrusted catalog", configPath, configBefore, configInfo)
	assertStatUnchanged(t, "untrusted catalog lock", lockPath, lockBefore, lockInfo)
	assertStatUnchanged(t, "untrusted catalog output", keeperPath, keeperBefore, keeperInfo)

	// The same requests are actionable once the catalog is trusted, so the
	// no-write assertion above is not an empty-selection control.
	out.Reset()
	pruned, err = prunePhase(context.Background(), &out, repoRoot, &pipeline.SyncResult{}, stale)
	if err != nil || pruned != 2 || strings.Contains(out.String(), "prune failed") {
		t.Fatalf("trusted control did not prune both requests: pruned=%d err=%v\n%s", pruned, err, &out)
	}
	v4, err := config.LoadV4ConfigFromPath(configPath)
	if err != nil || len(v4.Groups["first"].Include) != 1 || v4.Groups["first"].Include[0].Locator != "skill/keeper" || len(v4.Groups["second"].Include) != 0 {
		t.Fatalf("trusted control left a stale request: %v, %v", v4, err)
	}
	assertStatUnchanged(t, "trusted control lock", lockPath, lockBefore, lockInfo)
	assertStatUnchanged(t, "trusted control output", keeperPath, keeperBefore, keeperInfo)
}
