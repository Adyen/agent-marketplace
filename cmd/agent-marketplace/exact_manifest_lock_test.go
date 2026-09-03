package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

const tamperedLockDigest = lockfile.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

func setupExactManifestRegistry(t *testing.T) (repoRoot, origin, working, lockPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working = createBareRepoWithWorkingClone(t, "exact-manifest.git")
	writeSkill(t, working, "alpha", "Alpha locked", nil)
	writeFile(t, filepath.Join(working, "skills", "alpha", "scripts", "run.sh"), "#!/bin/sh\necho alpha\n")
	writeSkill(t, working, "beta", "Beta locked", nil)
	commitAndPushAll(t, working, "locked inventory")

	repoRoot = filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    skills:
      include: [alpha]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed exact sync: %v\n%s", err, output)
	}
	lockPath = filepath.Join(repoRoot, ".agents", "lock.yaml")
	return repoRoot, origin, working, lockPath
}

func TestLockedGitManifestTamperingBlocksMaterialization(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*lockfile.GitRoot)
	}{
		{name: "revision digest", tamper: func(root *lockfile.GitRoot) { root.RevisionDigest = tamperedLockDigest }},
		{name: "content digest", tamper: func(root *lockfile.GitRoot) { root.Resources[0].ContentDigest = tamperedLockDigest }},
		{name: "archive digest", tamper: func(root *lockfile.GitRoot) { root.Resources[0].ArchiveDigest = tamperedLockDigest }},
		{name: "support digest", tamper: func(root *lockfile.GitRoot) { root.Resources[0].SupportFiles["scripts/run.sh"] = tamperedLockDigest }},
		{name: "missing resource", tamper: func(root *lockfile.GitRoot) { root.Resources = root.Resources[1:] }},
		{name: "extra resource", tamper: func(root *lockfile.GitRoot) {
			root.Resources = append(root.Resources, lockfile.GitResource{Kind: "skill", Name: "extra", ContentDigest: tamperedLockDigest, ArchiveDigest: tamperedLockDigest})
		}},
		{name: "root layout", tamper: func(root *lockfile.GitRoot) { root.SourceHarness = "not-a-harness" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot, _, _, lockPath := setupExactManifestRegistry(t)
			doc := readExactManifestLock(t, lockPath)
			if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 || len(doc.Registries[0].Roots[0].Resources) != 2 {
				t.Fatalf("fixture lacks complete locked inventory: %+v", doc.Registries)
			}
			if doc.Registries[0].Roots[0].Resources[0].SupportFiles["scripts/run.sh"] == "" {
				t.Fatalf("fixture lacks locked support digest: %+v", doc.Registries[0].Roots[0].Resources[0])
			}
			tt.tamper(&doc.Registries[0].Roots[0])
			writeExactManifestLock(t, lockPath, doc)
			beforeLock, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeOutput, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "alpha", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}

			output, err := executeCommand("sync")
			if err == nil || !strings.Contains(err.Error(), "lock.manifest-mismatch") {
				t.Fatalf("sync accepted %s tamper: %v\n%s", tt.name, err, output)
			}
			afterLock, readErr := os.ReadFile(lockPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterLock) != string(beforeLock) {
				t.Fatalf("failed sync rewrote lock\nbefore:\n%s\nafter:\n%s", beforeLock, afterLock)
			}
			afterOutput, readErr := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "alpha", "SKILL.md"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterOutput) != string(beforeOutput) {
				t.Fatalf("failed sync changed materialized output\nbefore:\n%s\nafter:\n%s", beforeOutput, afterOutput)
			}
		})
	}
}

func TestReadOnlyGitViewsRejectManifestTamperingWithoutMutation(t *testing.T) {
	for _, command := range []string{"status", "list"} {
		t.Run(command, func(t *testing.T) {
			repoRoot, _, _, lockPath := setupExactManifestRegistry(t)
			doc := readExactManifestLock(t, lockPath)
			doc.Registries[0].Roots[0].Resources[0].ContentDigest = tamperedLockDigest
			writeExactManifestLock(t, lockPath, doc)
			beforeLock, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeOutput, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "alpha", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}

			output, err := executeCommand(command)
			if err == nil || !strings.Contains(err.Error(), "lock.manifest-mismatch") {
				t.Fatalf("%s accepted tampered exact inventory: %v\n%s", command, err, output)
			}
			afterLock, readErr := os.ReadFile(lockPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterLock) != string(beforeLock) {
				t.Fatalf("%s rewrote tampered lock", command)
			}
			afterOutput, readErr := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "alpha", "SKILL.md"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(afterOutput) != string(beforeOutput) {
				t.Fatalf("%s changed materialized output", command)
			}
		})
	}
}

func TestSelectorOnlyChangeReusesCompleteLockedInventoryWithoutRefResolution(t *testing.T) {
	repoRoot, origin, _, lockPath := setupExactManifestRegistry(t)
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    skills:
      include: [beta]
`)
	if err := os.Rename(origin, origin+".unavailable"); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("selector-only sync resolved the unavailable mutable ref: %v\n%s", err, output)
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterLock) != string(beforeLock) {
		t.Fatalf("selector-only sync rewrote complete inventory\nbefore:\n%s\nafter:\n%s", beforeLock, afterLock)
	}
	assertMaterializedSkillContains(t, repoRoot, "beta", "Beta locked")
	if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("selector-only sync retained excluded alpha: %v", err)
	}
}

func TestLockedInventoryVerificationPreservesMalformedDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "exact-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid locked", nil)
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), brokenSkillFrontmatter("broken-skill"))
	commitAndPushAll(t, working, "valid and malformed inventory")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills: [valid-skill, broken-skill]
`)
	withWorkingDir(t, repoRoot)
	for run := 1; run <= 2; run++ {
		output, err := executeCommand("sync")
		if err != nil {
			t.Fatalf("sync %d: %v\n%s", run, err, output)
		}
		for _, want := range []string{"broken-skill", "failed to parse"} {
			if !strings.Contains(output, want) {
				t.Fatalf("sync %d lost malformed diagnostic %q:\n%s", run, want, output)
			}
		}
		assertMaterializedSkillContains(t, repoRoot, "valid-skill", "Valid locked")
	}
	doc := readExactManifestLock(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	resources := doc.Registries[0].Roots[0].Resources
	if len(resources) != 1 || resources[0].Name != "valid-skill" {
		t.Fatalf("locked inventory = %+v, want valid sibling only", resources)
	}
}

func TestEmptyExactCacheReacquiresAndVerifiesLockedCommit(t *testing.T) {
	repoRoot, _, working, lockPath := setupExactManifestRegistry(t)
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "alpha", "Alpha mutable", nil)
	writeSkill(t, working, "gamma", "Gamma mutable", nil)
	commitAndPushAll(t, working, "advance mutable ref")
	if err := os.RemoveAll(filepath.Join(cache.CacheDir(), "exact-git")); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("exact reacquisition: %v\n%s", err, output)
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterLock) != string(beforeLock) {
		t.Fatalf("exact reacquisition advanced lock\nbefore:\n%s\nafter:\n%s", beforeLock, afterLock)
	}
	assertMaterializedSkillContains(t, repoRoot, "alpha", "Alpha locked")
}

func readExactManifestLock(t *testing.T, name string) lockfile.Document {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func writeExactManifestLock(t *testing.T, name string, doc lockfile.Document) {
	t.Helper()
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
