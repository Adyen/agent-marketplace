package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// TestPersonalLayerCannotSoftenAnOrgRequiredMemberMiss is the end-to-end proof
// for agent-marketplace-0236, driven through the real config layering rather
// than a unit test of the severity helper.
//
// The attack: an org/global layer declares a `required: true` entry for a
// monorepo (org policy). A personal, gitignored `.agents/config.local.yaml`
// adds a NON-required entry for the SAME repository at a different path. A
// collection member for that repository is absent from both locations. Under
// the old all-candidates-required rule the personal entry turned the
// org-mandated hard failure into a soft warning and `sync` exited 0. Any
// candidate location being required must now keep the miss hard.
func TestPersonalLayerCannotSoftenAnOrgRequiredMemberMiss(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// A local (non-git) monorepo with two disjoint subdirectories. Neither
	// contains `efficient-go`.
	monorepo := filepath.Join(t.TempDir(), "awesome-ai")
	writeSkillAt(t, monorepo, "teams/security", "devsecops", "DevSecOps baseline")
	writeSkillAt(t, monorepo, "personal/scratch", "scratch-note", "Personal scratch")

	// A separate registry hosting a collection whose member names the
	// monorepo by URL only — no `path:`.
	collectionHost := filepath.Join(t.TempDir(), "collections-host")
	writeSkill(t, collectionHost, "host-skill", "A skill so the host is a recognizable bundle", nil)
	writeCollection(t, collectionHost, "gotools", `
apiVersion: agent-marketplace/v1
kind: Collection
name: gotools
version: 1.0.0
description: Go tooling bundle
skills:
  - name: efficient-go
    registry: `+monorepo+`
`)

	// Org layer: the monorepo's security directory is org-mandated. This is the
	// externally managed v3 organization config, at the fixed path.
	orgCfgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeConfig(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+monorepo+`
    path: teams/security
    required: true
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)

	// No top-level harnesses: the target set is machine-wide and comes from the
	// user/org layers above.
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
registries:
  - url: `+collectionHost+`
collections:
  include:
    - gotools
`)
	// The attack lives here: a personal, gitignored layer adding a
	// non-required entry for the SAME repository at a different path.
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
apiVersion: agent-marketplace/v2
registries:
  - url: `+monorepo+`
    path: personal/scratch
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("a personal, gitignored config layer softened an org-mandated required miss; sync exited 0\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error()+output, "efficient-go") {
		t.Fatalf("expected the hard failure to name the missing member, got: %v\noutput:\n%s", err, output)
	}
}

// writeSkillAt writes a skill into a subdirectory within the repo working tree.
// Unlike writeSkill, the skill name in the metadata is just the basename, and
// the skill is placed under repoDir/subPath/skills/name/ to form a valid
// HCC-recognizable bundle layout.
//
// It lived next to the identity/team-profile tests until those surfaces were
// removed in the v3 cutover; this is now its only caller.
func writeSkillAt(t *testing.T, repoDir, subPath, name, description string) {
	t.Helper()
	skillDir := filepath.Join(repoDir, subPath, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", skillDir, err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), content)
}
