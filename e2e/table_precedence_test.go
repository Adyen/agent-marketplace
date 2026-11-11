package e2e_test

func init() {
	// ===== precedence =====
	testTable = append(testTable, TestCase{
		Name: "precedence/repo_exclude_overrides_repo_include",
		Desc: "Given repo config with both include and exclude for skill-a, When sync is run, Then exclude wins and skill-a is absent while skill-b is present",
		Registries: []RegistryDef{
			{Name: "precedence-registry", Skills: []Skill{
				{Name: "skill-a", Description: "Skill A"},
				{Name: "skill-b", Description: "Skill B"},
				{Name: "skill-c", Description: "Skill C"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:precedence-registry}}', ref: main}}",
			"resources: {team: {include: [skill/skill-a, skill/skill-b], exclude: [skill/skill-a]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/skill-b/SKILL.md").absent("./.factory/skills/skill-a"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/local_exclude_overrides_repo_include",
		Desc: "Given repo config including two skills and local config excluding one, When sync is run, Then the locally excluded skill is absent while the other remains",
		Registries: []RegistryDef{
			{Name: "local-precedence", Skills: []Skill{
				{Name: "wanted", Description: "Wanted skill"},
				{Name: "unwanted", Description: "Unwanted skill"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:local-precedence}}', ref: main}}",
			"resources: {team: {include: [skill/wanted, skill/unwanted]}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:local-precedence}}', ref: main}}",
			"resources: {team: {exclude: [skill/unwanted]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/wanted/SKILL.md").absent("./.factory/skills/unwanted"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/required_immune_to_excludes",
		Desc: "Given a required:true registry skill, When repo and local configs try to exclude it, Then the skill remains materialized",
		Registries: []RegistryDef{
			{Name: "required-registry", Skills: []Skill{
				{Name: "security-audit", Description: "Mandatory security audit", Tags: []string{"security"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:required-registry}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:required-registry}}', ref: main}}",
			"resources: {team: {exclude: [skill/security-audit]}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:required-registry}}', ref: main}}",
			"resources: {team: {exclude: [skill/security-audit]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/security-audit/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/org_config_sets_floor",
		Desc: "Given an unlocked org config with a required registry, When locked sync runs without a global registry, Then the org skill is materialized without creating an org lock",
		Registries: []RegistryDef{
			{Name: "org-registry", Skills: []Skill{
				{Name: "compliance-check", Description: "Org-mandated compliance"},
			}},
		},
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:org-registry}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		Steps: []Step{
			ok("sync", "--locked").exists("~/.factory/skills/compliance-check/SKILL.md").absent("/etc/agent-marketplace/lock.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/include_exclude_same_level",
		Desc: "Given a registry entry with both include and exclude for the same resource, When sync is run, Then exclude wins",
		Registries: []RegistryDef{
			{Name: "same-level", Skills: []Skill{
				{Name: "included", Description: "Should be included"},
				{Name: "conflicted", Description: "Include and exclude at same level"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:same-level}}', ref: main}}",
			"resources: {team: {include: [skill/included, skill/conflicted], exclude: [skill/conflicted]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/included/SKILL.md").absent("~/.factory/skills/conflicted"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/default_true_auto_selects_all",
		Desc: "Given a registry with all:true, When sync is run, Then all discovered resources are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "default-registry", Skills: []Skill{
				{Name: "auto-a", Description: "Auto-selected A"},
				{Name: "auto-b", Description: "Auto-selected B"},
				{Name: "auto-c", Description: "Auto-selected C"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:default-registry}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/auto-a/SKILL.md", "~/.factory/skills/auto-b/SKILL.md", "~/.factory/skills/auto-c/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "precedence/default_true_with_exclude",
		Desc: "Given an all:true registry with an explicit exclude, When sync is run, Then excluded resources are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "default-exclude", Skills: []Skill{
				{Name: "keep-me", Description: "Should stay"},
				{Name: "drop-me", Description: "Should be excluded"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:default-exclude}}', ref: main}}",
			"resources: {team: {all: true, exclude: [skill/drop-me]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/keep-me/SKILL.md").absent("~/.factory/skills/drop-me"),
		},
	})
}
