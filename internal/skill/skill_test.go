package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDiscoverSkillsFindsTopLevelSkillDirectories(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "vm-lifecycle", "SKILL.md"), `---
name: vm-lifecycle
description: VM lifecycle guidance
metadata:
  tags:
    - openstack
    - puppet
---

# VM Lifecycle
`)

	writeSkillMarkdown(t, filepath.Join(registryDir, "network-basics", "SKILL.md"), `---
name: network-basics
description: Network basics
metadata:
  tags:
    - network
---

# Network Basics
`)

	writeFile(t, filepath.Join(registryDir, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(registryDir, "README.md"), "# not a skill\n")
	writeFile(t, filepath.Join(registryDir, "references", "notes.txt"), "ignore me\n")

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	catalog := catalog{}
	for _, skill := range skills {
		catalog[skill.Name] = skill
	}

	if got := catalog["vm-lifecycle"].Description; got != "VM lifecycle guidance" {
		t.Fatalf("expected vm-lifecycle description, got %q", got)
	}

	if got := catalog["vm-lifecycle"].Path; got != filepath.Join(registryDir, "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle path %q, got %q", filepath.Join(registryDir, "vm-lifecycle"), got)
	}

	if got := catalog["network-basics"].Tags; !reflect.DeepEqual(got, []string{"network"}) {
		t.Fatalf("expected network-basics tags, got %#v", got)
	}
}

func TestDiscoverSkillsSkipsDirectoriesWithoutSkillMarkdown(t *testing.T) {
	registryDir := t.TempDir()

	writeFile(t, filepath.Join(registryDir, "references", "notes.txt"), "ignore me\n")
	writeFile(t, filepath.Join(registryDir, "plain-dir", "README.md"), "# no skill\n")

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 0 {
		t.Fatalf("expected no skills, got %#v", skills)
	}
}

func TestDiscoverSkillsReportsMalformedFrontmatter(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "valid-skill", "SKILL.md"), `---
name: valid-skill
description: Valid
metadata:
  tags:
    - tested
---

# Valid
`)

	writeSkillMarkdown(t, filepath.Join(registryDir, "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed despite malformed skill: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected only the valid skill, got %#v", skills)
	}

	if skills[0].Name != "valid-skill" {
		t.Fatalf("expected valid skill to remain, got %#v", skills[0])
	}

	if len(malformed) != 1 {
		t.Fatalf("expected one malformed skill, got %#v", malformed)
	}

	if malformed[0].DirName != "broken-skill" {
		t.Fatalf("expected malformed dir name broken-skill, got %#v", malformed[0])
	}

	if malformed[0].Path != filepath.Join(registryDir, "broken-skill") {
		t.Fatalf("expected malformed path %q, got %#v", filepath.Join(registryDir, "broken-skill"), malformed[0])
	}

	if malformed[0].Error == nil || !strings.Contains(malformed[0].Error.Error(), "parse yaml") {
		t.Fatalf("expected malformed parse error, got %#v", malformed[0].Error)
	}
}

func TestRecursiveDiscoverSkillsFindsNestedSkillDirectories(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "vm-lifecycle", "SKILL.md"), `---
name: vm-lifecycle
description: VM lifecycle guidance
metadata:
  tags:
    - openstack
---

# VM Lifecycle
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "network", "firewall-basics", "SKILL.md"), `---
name: firewall-basics
description: Firewall basics
metadata:
  tags:
    - network
---

# Firewall Basics
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %#v", skills)
	}

	paths := map[string]string{}
	for _, discovered := range skills {
		paths[discovered.Name] = discovered.Path
	}

	if got := paths["vm-lifecycle"]; got != filepath.Join(registryDir, "skills", "vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle path %q, got %q", filepath.Join(registryDir, "skills", "vm-lifecycle"), got)
	}

	if got := paths["firewall-basics"]; got != filepath.Join(registryDir, "skills", "network", "firewall-basics") {
		t.Fatalf("expected firewall-basics path %q, got %q", filepath.Join(registryDir, "skills", "network", "firewall-basics"), got)
	}
}

func TestRecursiveDiscoverSkillsSkipsHiddenDirectoriesUnlessExplicitlyTargeted(t *testing.T) {
	t.Run("default walk skips hidden directories", func(t *testing.T) {
		registryDir := t.TempDir()
		writeSkillMarkdown(t, filepath.Join(registryDir, ".factory", "skills", "hidden-skill", "SKILL.md"), `---
name: hidden-skill
description: Hidden
---

# Hidden
`)

		skills, malformed, err := DiscoverSkills(registryDir)
		if err != nil {
			t.Fatalf("expected discovery to succeed: %v", err)
		}
		if len(malformed) != 0 {
			t.Fatalf("expected no malformed skills, got %#v", malformed)
		}

		if len(skills) != 0 {
			t.Fatalf("expected hidden skills to be skipped, got %#v", skills)
		}
	})

	t.Run("explicit subtree allows hidden directories", func(t *testing.T) {
		registryDir := t.TempDir()
		target := filepath.Join(registryDir, ".factory", "skills")
		writeSkillMarkdown(t, filepath.Join(target, "hidden-skill", "SKILL.md"), `---
name: hidden-skill
description: Hidden
---

# Hidden
`)

		skills, malformed, err := DiscoverSkills(target)
		if err != nil {
			t.Fatalf("expected discovery to succeed: %v", err)
		}
		if len(malformed) != 0 {
			t.Fatalf("expected no malformed skills, got %#v", malformed)
		}

		if len(skills) != 1 || skills[0].Name != "hidden-skill" {
			t.Fatalf("expected hidden skill to be discovered when explicitly targeted, got %#v", skills)
		}
	})
}

func TestRecursiveDiscoverSkillsSkipsIgnoredDirectories(t *testing.T) {
	registryDir := t.TempDir()
	writeSkillMarkdown(t, filepath.Join(registryDir, "tests", "fixture-skill", "SKILL.md"), `---
name: fixture-skill
description: Fixture
---

# Fixture
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "node_modules", "dep-skill", "SKILL.md"), `---
name: dep-skill
description: Dependency
---

# Dependency
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "real-skill", "SKILL.md"), `---
name: real-skill
description: Real
---

# Real
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "real-skill" {
		t.Fatalf("expected only real-skill, got %#v", skills)
	}
}

func TestRecursiveDiscoverSkillsKeepsFirstDuplicateSkillDirectoryName(t *testing.T) {
	registryDir := t.TempDir()
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "shared-name", "SKILL.md"), `---
name: shared-name
description: First
---

# First
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "examples", "shared-name", "SKILL.md"), `---
name: shared-name
description: Second
---

# Second
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to tolerate duplicate names: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one deduplicated skill, got %#v", skills)
	}
	if skills[0].Name != "shared-name" || skills[0].Description != "Second" {
		t.Fatalf("expected lexically first duplicate to win, got %#v", skills[0])
	}
}

func TestRecursiveDiscoverSkillsToleratesNestedDuplicateFrontmatterName(t *testing.T) {
	registryDir := t.TempDir()
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "brainstorming", "SKILL.md"), `---
name: brainstorming
description: General brainstorming
---

# Brainstorming
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "product-skills", "pm-brainstorming", "SKILL.md"), `---
name: brainstorming
description: Product brainstorming
---

# PM Brainstorming
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to tolerate nested duplicate names: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one deduplicated skill, got %#v", skills)
	}
	if skills[0].Name != "brainstorming" {
		t.Fatalf("expected brainstorming skill, got %#v", skills[0])
	}
	if skills[0].Path != filepath.Join(registryDir, "skills", "brainstorming") {
		t.Fatalf("expected top-level brainstorming to win, got %q", skills[0].Path)
	}
}

func TestRecursiveDiscoverSkillsReturnsSortedResults(t *testing.T) {
	registryDir := t.TempDir()
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "zebra", "SKILL.md"), `---
name: zebra
description: Zebra
---

# Zebra
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "alpha", "SKILL.md"), `---
name: alpha
description: Alpha
---

# Alpha
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	names := []string{skills[0].Name, skills[1].Name}
	if !slices.Equal(names, []string{"alpha", "zebra"}) {
		t.Fatalf("expected sorted names, got %#v", names)
	}
}

func TestParseFrontmatterExtractsNameDescriptionAndTags(t *testing.T) {
	skillMdPath := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkillMarkdown(t, skillMdPath, `---
name: vm-lifecycle
description: >
  How to create and decommission VMs.
metadata:
  version: "1.2"
  tags:
    - openstack
    - puppet
    - network
---

# VM Lifecycle
`)

	name, description, tags, err := parseFrontmatter(skillMdPath)
	if err != nil {
		t.Fatalf("expected frontmatter to parse: %v", err)
	}

	if name != "vm-lifecycle" {
		t.Fatalf("expected name %q, got %q", "vm-lifecycle", name)
	}

	if description != "How to create and decommission VMs.\n" {
		t.Fatalf("expected folded description, got %q", description)
	}

	if !reflect.DeepEqual(tags, []string{"openstack", "puppet", "network"}) {
		t.Fatalf("expected tags to be parsed, got %#v", tags)
	}
}

func TestParseFrontmatterReturnsErrorForMalformedContent(t *testing.T) {
	skillMdPath := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkillMarkdown(t, skillMdPath, `---
name: broken
description: [unterminated
---

# Broken
`)

	_, _, _, err := parseFrontmatter(skillMdPath)
	if err == nil {
		t.Fatalf("expected malformed frontmatter to fail")
	}
}

func TestDiscoverSkillsFindsStandaloneMarkdownFiles(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "resolve-merge-conflict.md"), `---
name: resolve-merge-conflict
description: Resolve git merge conflicts
metadata:
  tags:
    - git
    - merge
---

# Resolve Merge Conflicts
`)

	writeSkillMarkdown(t, filepath.Join(registryDir, "code-review.md"), `---
name: code-review
description: Code review guidance
metadata:
  tags:
    - review
---

# Code Review
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	catalog := catalog{}
	for _, skill := range skills {
		catalog[skill.Name] = skill
	}

	if _, ok := catalog["resolve-merge-conflict"]; !ok {
		t.Fatalf("expected resolve-merge-conflict skill to be found")
	}

	if got := catalog["resolve-merge-conflict"].Description; got != "Resolve git merge conflicts" {
		t.Fatalf("expected resolve-merge-conflict description, got %q", got)
	}

	if got := catalog["resolve-merge-conflict"].Path; got != filepath.Join(registryDir, "resolve-merge-conflict.md") {
		t.Fatalf("expected resolve-merge-conflict path %q, got %q", filepath.Join(registryDir, "resolve-merge-conflict.md"), got)
	}

	if got := catalog["code-review"].Tags; !reflect.DeepEqual(got, []string{"review"}) {
		t.Fatalf("expected code-review tags, got %#v", got)
	}
}

func TestDiscoverSkillsSilentlySkipsStandaloneMdWithoutFrontmatter(t *testing.T) {
	registryDir := t.TempDir()

	writeFile(t, filepath.Join(registryDir, "README.md"), "# Not a skill\n")
	writeFile(t, filepath.Join(registryDir, "CHANGELOG.md"), "# Changelog\n")
	writeSkillMarkdown(t, filepath.Join(registryDir, "real-skill.md"), `---
name: real-skill
description: A real skill
---

# Real Skill
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "real-skill" {
		t.Fatalf("expected only real-skill, got %#v", skills)
	}
}

func TestDiscoverSkillsMixesDirectoryAndStandaloneSkills(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "dir-skill", "SKILL.md"), `---
name: dir-skill
description: A directory-based skill
---

# Dir Skill
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "skills", "file-skill.md"), `---
name: file-skill
description: A file-based skill
---

# File Skill
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	catalog := catalog{}
	for _, skill := range skills {
		catalog[skill.Name] = skill
	}

	if got := catalog["dir-skill"].Path; got != filepath.Join(registryDir, "skills", "dir-skill") {
		t.Fatalf("expected dir-skill path %q, got %q", filepath.Join(registryDir, "skills", "dir-skill"), got)
	}

	if got := catalog["file-skill"].Path; got != filepath.Join(registryDir, "skills", "file-skill.md") {
		t.Fatalf("expected file-skill path %q, got %q", filepath.Join(registryDir, "skills", "file-skill.md"), got)
	}
}

func TestDiscoverSkillsSkipsStandaloneMdInsideSkillDirectory(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "my-skill", "SKILL.md"), `---
name: my-skill
description: A directory-based skill
---

# My Skill
`)
	// Auxiliary .md file inside the skill directory — should NOT be treated as a separate skill
	writeSkillMarkdown(t, filepath.Join(registryDir, "my-skill", "notes.md"), `---
name: notes
description: Just notes
---

# Notes
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "my-skill" {
		t.Fatalf("expected only my-skill, got %#v", skills)
	}
}

func TestDiscoverSkillsSkipsNestedSkillMdInsideSkillDirectory(t *testing.T) {
	registryDir := t.TempDir()

	// Top-level skill with a proper SKILL.md.
	writeSkillMarkdown(t, filepath.Join(registryDir, "rpl-to-spring", "SKILL.md"), `---
name: rpl-to-spring
description: Parent skill
---

# Parent
`)
	// Nested SKILL.md files that are supplementary assets of the parent skill
	// (e.g. rpl-to-spring/windsurf/rpl-analyzer/SKILL.md in awesome-ai). These
	// typically lack frontmatter and must not be surfaced as independent
	// skills (and therefore must not be reported as malformed).
	writeFile(t, filepath.Join(registryDir, "rpl-to-spring", "windsurf", "rpl-analyzer", "SKILL.md"), "# RPL Analyzer\n\nNo frontmatter, this is an asset.\n")
	writeFile(t, filepath.Join(registryDir, "rpl-to-spring", "windsurf", "rpl-code-writer", "SKILL.md"), "# RPL Code Writer\n\nNo frontmatter.\n")

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected nested asset SKILL.md files to be ignored, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "rpl-to-spring" {
		t.Fatalf("expected only rpl-to-spring, got %#v", skills)
	}
}

func TestDiscoverSkillsSkipsNestedStandaloneMdInsideSkillDirectory(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "my-skill", "SKILL.md"), `---
name: my-skill
description: A directory-based skill
---

# My Skill
`)
	// Nested auxiliary .md file deep inside the skill directory
	writeSkillMarkdown(t, filepath.Join(registryDir, "my-skill", "references", "_index.md"), `---
name: _index
description: Index page
---

# References
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "my-skill" {
		t.Fatalf("expected only my-skill, got %#v", skills)
	}
}

func TestDiscoverSkillsKeepsDirectorySkillBeforeDuplicateStandaloneFile(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "shared-name", "SKILL.md"), `---
name: shared-name
description: Directory skill
---

# Dir
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "shared-name.md"), `---
name: shared-name
description: File skill
---

# File
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to tolerate duplicate names: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one deduplicated skill, got %#v", skills)
	}
	if skills[0].Description != "Directory skill" {
		t.Fatalf("expected directory skill to win over standalone file, got %#v", skills[0])
	}
}

func TestDiscoverSkillsIgnoresDuplicateStandaloneMarkdownWithoutFrontmatter(t *testing.T) {
	registryDir := t.TempDir()

	// Non-skill documents that happen to share the same basename in different
	// folders (e.g. awesome-ai's AAA/Build & Automate/UseCases.md and
	// AAA/Knowledge & Docs/UseCases.md) must not block discovery.
	writeFile(t, filepath.Join(registryDir, "AAA", "Build & Automate", "UseCases.md"), "# Build & Automate\n\nSome prose, no frontmatter.\n")
	writeFile(t, filepath.Join(registryDir, "AAA", "Knowledge & Docs", "UseCases.md"), "# Knowledge & Docs\n\nMore prose.\n")
	writeSkillMarkdown(t, filepath.Join(registryDir, "real-skill.md"), `---
name: real-skill
description: A real skill
---

# Real Skill
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed despite duplicate non-skill basenames: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "real-skill" {
		t.Fatalf("expected only real-skill, got %#v", skills)
	}
}

func TestDiscoverSkillsSilentlySkipsStandaloneMdWithFrontmatterButNoName(t *testing.T) {
	registryDir := t.TempDir()

	// Doc-style markdown with YAML frontmatter that only declares metadata
	// fields (title/description/keywords) but no "name:" — e.g. awesome-ai's
	// docs/prompting-best-practices.md. These must not surface as nameless
	// skills in list output.
	writeSkillMarkdown(t, filepath.Join(registryDir, "docs", "prompting-best-practices.md"), `---
title: Prompt Crafting for Different Models
description: Model-specific prompting techniques.
keywords: ['prompts', 'claude', 'gpt']
---

# Prompt Crafting
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "docs", "blank-name.md"), `---
name: "   "
description: Whitespace-only name should be treated the same as missing.
---

# Blank Name
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "real-skill.md"), `---
name: real-skill
description: A real skill
---

# Real Skill
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to succeed: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}

	if len(skills) != 1 || skills[0].Name != "real-skill" {
		t.Fatalf("expected only real-skill, got %#v", skills)
	}
}

func TestDiscoverSkillsKeepsFirstDuplicateParsedNameAcrossStandaloneFiles(t *testing.T) {
	registryDir := t.TempDir()

	writeSkillMarkdown(t, filepath.Join(registryDir, "a", "first.md"), `---
name: shared-skill
description: First
---

# First
`)
	writeSkillMarkdown(t, filepath.Join(registryDir, "b", "second.md"), `---
name: shared-skill
description: Second
---

# Second
`)

	skills, malformed, err := DiscoverSkills(registryDir)
	if err != nil {
		t.Fatalf("expected discovery to tolerate duplicate parsed names: %v", err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed skills, got %#v", malformed)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one deduplicated skill, got %#v", skills)
	}
	if skills[0].Description != "First" {
		t.Fatalf("expected first standalone markdown skill to win, got %#v", skills[0])
	}
}

func writeSkillMarkdown(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}
