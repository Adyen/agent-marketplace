package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// spec: update-preview/preview-summarizes-a-real-update-without-content-hunks
// spec: update-preview/preview-with-a-diff-uses-the-same-candidates
// spec: update-output/applying-update-reports-the-reviewed-content
func TestUpdatePreviewFlagsAgainstV4Consumer(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	files := SkillsOnly(Skill{Name: "review", Description: "Original review"})
	files["collections/review-pack/COLLECTION.yaml"] = `name: review-pack
version: "1.0"
description: Review collection
resources:
  - name: review
    kind: skill
`
	url := env.CreateBareRepo("preview-v4", files)
	env.WriteRepoConfig(fmt.Sprintf("sources: {team: {git: %q, ref: main}}\nresources: {team: {include: [skill/review, collection/review-pack]}}\n", url))
	env.MustRunAM("sync")
	locked := env.ReadFile("/workspace/.agents/lock.yaml")
	installed := env.ReadFile("/workspace/.factory/skills/review/SKILL.md")

	publishRegistryFiles(t, env, url, map[string]string{
		"skills/review/SKILL.md": (Skill{Name: "review", Description: "Revised upstream"}).SkillFile(),
	})

	// A dry run previews the proposed update without changing the acquisition
	// lock or installed content and exits 0.
	dryRun := env.MustRunAM("update", "--dry-run")
	if !strings.Contains(dryRun.Combined, "Dry run: no updates were applied.") {
		t.Fatalf("dry run did not state that no updates were applied:\n%s", dryRun.Combined)
	}
	if !strings.Contains(dryRun.Combined, "review") {
		t.Fatalf("dry run did not summarize the proposed resource change:\n%s", dryRun.Combined)
	}
	if strings.Contains(dryRun.Combined, "--- a/SKILL.md") {
		t.Fatalf("dry run without --include-diff added content hunks:\n%s", dryRun.Combined)
	}
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != locked {
		t.Fatalf("dry run published a new acquisition lock:\n%s", got)
	}
	if got := env.ReadFile("/workspace/.factory/skills/review/SKILL.md"); got != installed {
		t.Fatalf("dry run changed installed content:\n%s", got)
	}

	// The same candidates, with diffs proposed rather than applied.
	dryRunDiff := env.MustRunAM("update", "--dry-run", "--include-diff")
	for _, want := range []string{"Dry run: no updates were applied.", `"description": "Revised upstream"`} {
		if !strings.Contains(dryRunDiff.Combined, want) {
			t.Fatalf("dry-run diff missing %q:\n%s", want, dryRunDiff.Combined)
		}
	}
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got != locked {
		t.Fatalf("dry-run diff published a new acquisition lock:\n%s", got)
	}
	if got := env.ReadFile("/workspace/.factory/skills/review/SKILL.md"); got != installed {
		t.Fatalf("dry-run diff changed installed content:\n%s", got)
	}

	// An applying update with --include-diff consumes the same prepared
	// candidates, reports their reviewed content, and refreshes the lock.
	applied := env.MustRunAM("update", "--include-diff")
	if !strings.Contains(applied.Combined, "Revised upstream") {
		t.Fatalf("applied update did not report the reviewed content change:\n%s", applied.Combined)
	}
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "Revised upstream")
	if got := env.ReadFile("/workspace/.agents/lock.yaml"); got == locked {
		t.Fatalf("applying update did not refresh the acquisition lock")
	}
	env.MustRunAM("sync", "--check")
}

// spec: update-output/applying-update-reports-the-reviewed-content
func TestUpdatePreviewFlagsAgainstMarketplacePins(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("version one", true))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	if result := consumer.RunAM("install", "--scope", "repo", "behavior:behavior-source/skill/skill-one", "--yes"); result.ExitCode != 0 {
		t.Fatalf("install marketplace pin failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")
	locked := consumer.ReadFile("/workspace/.agents/lock.yaml")
	config := consumer.ReadFile("/workspace/.agents/config.yaml")

	fixture.mutateSource(t, "skills/skill-one/SKILL.md", "---\nname: skill-one\ndescription: Behavior skill\nlicense: MIT\n---\nversion two\n")
	fixture.reindex(t, fixture.cfg)

	// The preview keeps the exact pin, configuration, and installed content
	// unchanged while proposing and diffing the newer revision.
	preview := consumer.MustRunAM("update", "--dry-run", "--include-diff")
	for _, want := range []string{"Dry run: no updates were applied.", "Marketplace pins proposed for update: 1", "version two"} {
		if !strings.Contains(preview.Combined, want) {
			t.Fatalf("marketplace dry-run diff missing %q:\n%s", want, preview.Combined)
		}
	}
	if got := consumer.ReadFile("/workspace/.agents/lock.yaml"); got != locked {
		t.Fatalf("marketplace dry run changed the acquisition lock:\n%s", got)
	}
	if got := consumer.ReadFile("/workspace/.agents/config.yaml"); got != config {
		t.Fatalf("marketplace dry run changed configuration:\n%s", got)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")

	applied := consumer.MustRunAM("update", "--include-diff")
	if !strings.Contains(applied.Combined, "version two") {
		t.Fatalf("marketplace applied diff did not report the reviewed content:\n%s", applied.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version two")
	if got := consumer.ReadFile("/workspace/.agents/lock.yaml"); got == locked {
		t.Fatalf("marketplace update did not refresh the exact acquisition lock")
	}
	consumer.MustRunAM("sync", "--check")
}
