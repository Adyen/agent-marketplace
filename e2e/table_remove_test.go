package e2e_test

func init() {
	// ===== remove command =====
	testTable = append(testTable, TestCase{
		Name: "remove/install_then_remove_roundtrip",
		Desc: "Given a skill synced to machine scope, When remove --scope user is run and re-synced, Then the skill is gone from the filesystem",
		Registries: []RegistryDef{
			{Name: "roundtrip-reg", Skills: []Skill{
				{Name: "roundtrip-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:roundtrip-reg}}', ref: main}}",
			"resources: {team: {include: [skill/roundtrip-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/roundtrip-skill/SKILL.md"),
			ok("remove", "--scope", "user", "roundtrip-skill"),
			ok("sync").absent("~/.factory/skills/roundtrip-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "remove/required_skill_fails",
		Desc: "Given a required:true registry skill, When remove is attempted, Then it exits 1 with 'required' in the error",
		Registries: []RegistryDef{
			{Name: "required-reg", Skills: []Skill{
				{Name: "mandatory-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:required-reg}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		Steps: []Step{
			ok("sync"),
			exits(1, "remove", "mandatory-skill").out("required"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "remove/default_true_adds_exclude",
		Desc: "Given all:true auto-selected skills, When remove --scope user is run for one skill and re-synced, Then that skill is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "default-rm", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "drop-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:default-rm}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/keep-skill/SKILL.md", "~/.factory/skills/drop-skill/SKILL.md"),
			ok("remove", "--scope", "user", "drop-skill"),
			ok("sync").exists("~/.factory/skills/keep-skill/SKILL.md").absent("~/.factory/skills/drop-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "remove/from_removes_named_resource",
		Desc: "Given two skills from one registry, When remove --scope user removes one and re-syncs, Then only the removed skill is pruned",
		Registries: []RegistryDef{
			{Name: "from-rm", Skills: []Skill{
				{Name: "from-skill-a"},
				{Name: "from-skill-b"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:from-rm}}', ref: main}}",
			"resources: {team: {include: [skill/from-skill-a, skill/from-skill-b]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/from-skill-a/SKILL.md", "~/.factory/skills/from-skill-b/SKILL.md"),
			ok("remove", "--scope", "user", "from-skill-a").out("from-skill-a"),
			ok("sync").exists("~/.factory/skills/from-skill-b/SKILL.md").absent("~/.factory/skills/from-skill-a"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "remove/from_no_name_removes_entry",
		Desc: "Given a registry entry in global config, When remove --scope user --from removes the entire entry and re-syncs, Then the skill is gone",
		Registries: []RegistryDef{
			{Name: "from-entry-rm", Skills: []Skill{
				{Name: "entry-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:from-entry-rm}}', ref: main}}",
			"resources: {team: {include: [skill/entry-skill]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/entry-skill/SKILL.md"),
			ok("remove", "--scope", "user", "--from", "{{REG:from-entry-rm}}").out("Removed"),
			ok("sync").absent("~/.factory/skills/entry-skill"),
		},
	})

	// spec: materialization/deselected-gemini-subagents-are-removed
	testTable = append(testTable, TestCase{
		Name: "remove/gemini_last_subagent_removes_repo_output",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-remove",
				Skills:    []Skill{{Name: "keep-skill", Description: "Stays installed"}},
				Subagents: []Subagent{{Name: "helper", Description: "Removed by the test"}}},
		},
		GlobalCfg: v4("harnesses: [gemini]"),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:gemini-subagent-remove}}', ref: main}}",
			"resources: {team: {include: [skill/keep-skill, subagent/helper]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.gemini/agents/helper.md").exists("./.gemini/skills/keep-skill/SKILL.md"),
			ok("remove", "--scope", "repo", "helper"),
			ok("sync").absent("./.gemini/agents/helper.md").exists("./.gemini/skills/keep-skill/SKILL.md"),
			ok("sync", "--check"),
		},
	})

	// spec: materialization/deselected-gemini-subagents-are-removed
	testTable = append(testTable, TestCase{
		Name: "remove/gemini_last_subagent_removes_user_output",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-remove-user",
				Skills:    []Skill{{Name: "keep-skill", Description: "Stays installed"}},
				Subagents: []Subagent{{Name: "helper", Description: "Removed by the test"}}},
		},
		GlobalCfg: v4(
			"harnesses: [gemini]",
			"sources: {team: {git: '{{REG:gemini-subagent-remove-user}}', ref: main}}",
			"resources: {team: {include: [skill/keep-skill, subagent/helper]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("~/.gemini/agents/helper.md").exists("~/.gemini/skills/keep-skill/SKILL.md"),
			ok("remove", "--scope", "user", "helper"),
			ok("sync").absent("~/.gemini/agents/helper.md").exists("~/.gemini/skills/keep-skill/SKILL.md"),
			ok("sync", "--check"),
		},
	})
}
