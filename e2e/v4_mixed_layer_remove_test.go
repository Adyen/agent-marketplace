package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// The organization layer is always legacy, while init creates a v4 user
// config. Opting out of an optional organization resource must edit the v4
// user layer, and opting back in must restore it.
func TestV4UserLayerOptsOutOfLegacyOrganizationResource(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	registry := env.CreateBareRepo("org-defaults", SkillsOnly(
		Skill{Name: "review", Description: "Review changes"},
		Skill{Name: "other", Description: "Other changes"},
	))
	env.WriteOrgConfig(fmt.Sprintf("registries:\n  - url: %s\n    ref: main\n    default: true\n", registry))
	env.MustRunAM("init", "--harnesses", "factory")
	env.MustRunAM("sync")
	env.AssertFileExists("/root/.factory/skills/review/SKILL.md")

	removed := env.RunAM("remove", "review")
	if removed.ExitCode != 0 {
		t.Fatalf("opting out of an organization resource from a v4 user config failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/review")
	env.AssertFileExists("/root/.factory/skills/other/SKILL.md")
	userConfig := env.ReadFile("/root/.agents/config.yaml")
	if !strings.Contains(userConfig, "apiVersion: agent-marketplace/v4") || !strings.Contains(userConfig, "skill/review") || strings.Contains(userConfig, "registries:") {
		t.Fatalf("opt-out did not stay a v4 user edit:\n%s", userConfig)
	}
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)

	restored := env.RunAM("install", "review", "--scope", "user")
	if restored.ExitCode != 0 {
		t.Fatalf("opting back into the organization resource failed (exit %d):\n%s", restored.ExitCode, restored.Combined)
	}
	env.AssertFileExists("/root/.factory/skills/review/SKILL.md")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}

// Excluding one member of a legacy organization collection edits the v4 user
// layer and keeps the rest of the collection installed.
func TestV4UserLayerExcludesLegacyOrganizationCollectionMember(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	files := SkillsOnly(Skill{Name: "review", Description: "Review changes"}, Skill{Name: "other", Description: "Other changes"})
	files["collections/bundle/COLLECTION.yaml"] = "name: bundle\nversion: \"1.0\"\nresources:\n  - name: review\n  - name: other\n"
	registry := env.CreateBareRepo("org-collection", files)
	env.WriteOrgConfig(fmt.Sprintf("registries:\n  - url: %s\n    ref: main\ncollections:\n  include:\n    - bundle\n", registry))
	env.MustRunAM("init", "--harnesses", "factory")
	env.MustRunAM("sync")
	env.AssertFileExists("/root/.factory/skills/review/SKILL.md")

	removed := env.RunAM("remove", "review")
	if removed.ExitCode != 0 {
		t.Fatalf("excluding an organization collection member from a v4 user config failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
	}
	env.AssertFileAbsent("/root/.factory/skills/review")
	env.AssertFileExists("/root/.factory/skills/other/SKILL.md")
	if userConfig := env.ReadFile("/root/.agents/config.yaml"); !strings.Contains(userConfig, "apiVersion: agent-marketplace/v4") || !strings.Contains(userConfig, "skill/review") {
		t.Fatalf("collection-member exclusion did not stay a v4 user edit:\n%s", userConfig)
	}
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}
