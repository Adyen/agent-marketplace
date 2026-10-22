package e2e_test

func init() {
	// ===== collision =====
	// spec: resource-selection/repository-winner-does-not-displace-a-user-scope-selection
	testTable = append(testTable, TestCase{
		Name:       "collision/cross_scope_sync_keeps_both_skills",
		Registries: []RegistryDef{{Name: "user-skill", Skills: []Skill{{Name: "shared", Description: "User selection"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:user-skill}}', ref: main}}",
			"resources: {user: {include: [skill/shared]}}",
		),
		RepoCfg: v4(
			"sources: {repo: {directory: ./resources}}",
			"resources: {repo: {include: [skill/shared]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/shared/SKILL.md", (Skill{Name: "shared", Description: "Repository selection"}).SkillFile()),
			ok("sync").
				notOut("multiple registries").
				symlinks("~/.factory/skills/shared", "./.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "User selection").
				has("./.factory/skills/shared/SKILL.md", "Repository selection"),
			ok("sync", "--check"),
		},
	})

	// spec: resource-selection/required-user-scope-selection-suppresses-a-repository-selection
	testTable = append(testTable, TestCase{
		Name:       "collision/required_machine_prevents_repo_shadow",
		Registries: []RegistryDef{{Name: "required-user", Skills: []Skill{{Name: "shared", Description: "Mandatory user selection"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:required-user}}', ref: main}}",
			"resources: {team: {include: [skill/shared], required: true}}",
		),
		RepoCfg: v4(
			"sources: {repo: {directory: ./resources}}",
			"resources: {repo: {include: [skill/shared]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/shared/SKILL.md", (Skill{Name: "shared", Description: "Unreviewed repository selection"}).SkillFile()),
			ok("sync").
				out("multiple registries").
				absent("./.factory/skills/shared").
				symlinks("~/.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "Mandatory user selection"),
			ok("sync").absent("./.factory/skills/shared").unchanged("~/.factory/skills/shared"),
			ok("sync", "--check").unchanged("~/.factory/skills/shared"),
		},
	})

	// spec: resource-selection/collection-member-is-not-displaced-by-a-selection-in-the-other-scope
	testTable = append(testTable, TestCase{
		Name: "collision/cross_scope_collection_member_keeps_both_skills",
		Registries: []RegistryDef{{Name: "user-bundle", Skills: []Skill{{Name: "shared", Description: "User collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:user-bundle}}', ref: main}}",
			"resources: {user: {include: [collection/bundle]}}",
		),
		RepoCfg: v4(
			"sources: {repo: {directory: ./resources}}",
			"resources: {repo: {include: [skill/shared]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/shared/SKILL.md", (Skill{Name: "shared", Description: "Repository selection"}).SkillFile()),
			ok("sync").
				notOut("multiple registries").
				symlinks("~/.factory/skills/shared", "./.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "User collection member").
				has("./.factory/skills/shared/SKILL.md", "Repository selection"),
			ok("sync", "--check").notOut("multiple registries").unchanged("~/.factory/skills/shared", "./.factory/skills/shared"),
		},
	})

	// spec: resource-selection/required-selection-through-a-collection-suppresses-a-repository-selection
	testTable = append(testTable, TestCase{
		Name: "collision/required_collection_member_prevents_repo_shadow",
		Registries: []RegistryDef{{Name: "org-bundle", Skills: []Skill{{Name: "shared", Description: "Mandatory collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}}},
		OrgCfg: v4(
			"sources: {org: {git: '{{REG:org-bundle}}', ref: main}}",
			"resources: {org: {include: [collection/bundle], required: true}}",
		),
		RepoCfg: v4(
			"sources: {repo: {directory: ./resources}}",
			"resources: {repo: {include: [skill/shared]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/shared/SKILL.md", (Skill{Name: "shared", Description: "Unreviewed repository selection"}).SkillFile()),
			ok("sync").
				out("multiple registries").
				absent("./.factory/skills/shared").
				symlinks("~/.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "Mandatory collection member"),
			ok("sync", "--check").absent("./.factory/skills/shared").unchanged("~/.factory/skills/shared"),
		},
	})

	// spec: resource-selection/required-collection-member-displaces-an-optional-selection-in-the-same-scope
	testTable = append(testTable, TestCase{
		Name: "collision/required_collection_member_displaces_user_selection",
		Registries: []RegistryDef{
			{Name: "org-bundle", Skills: []Skill{{Name: "shared", Description: "Mandatory collection member"}},
				Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}},
			{Name: "user-skill", Skills: []Skill{{Name: "shared", Description: "Optional user selection"}}},
		},
		OrgCfg: v4(
			"sources: {org: {git: '{{REG:org-bundle}}', ref: main}}",
			"resources: {org: {include: [collection/bundle], required: true}}",
		),
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:user-skill}}', ref: main}}",
			"resources: {user: {include: [skill/shared]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").
				out("multiple registries").
				absent("./.factory/skills/shared").
				symlinks("~/.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "Mandatory collection member"),
			ok("sync", "--check").unchanged("~/.factory/skills/shared"),
		},
	})

	// spec: harness-compilation/required-resource-keeps-a-case-only-collision
	testTable = append(testTable, TestCase{
		Name: "collision/required_command_keeps_case_only_path",
		Registries: []RegistryDef{
			{Name: "org-command", Commands: []Command{{Name: "deploy", Description: "Mandatory organization command", Command: "echo org"}}},
			{Name: "user-opencode", Files: map[string]string{".opencode/commands/Deploy.md": "---\ndescription: User selected command\n---\nRun the user command.\n"}},
		},
		OrgCfg: v4(
			"harnesses: [claude]",
			"sources: {org: {git: '{{REG:org-command}}', ref: main}}",
			"resources: {org: {all: true, required: true}}",
		),
		GlobalCfg: v4(
			"harnesses: [claude]",
			"sources: {user: {git: '{{REG:user-opencode}}', ref: main, source_harness: opencode}}",
			"resources: {user: {include: [command/Deploy]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			// Content checks rather than an absent Deploy.md keep the case
			// portable: on a case-insensitive host both names are one file.
			ok("sync").
				out("materialize.collision.shared-path", `command "Deploy"`).
				notOut("bundle.conflict.duplicate-artifact-path").
				has("~/.claude/commands/deploy.md", "Mandatory organization command").
				lacks("~/.claude/commands/deploy.md", "Run the user command."),
			ok("sync", "--check").unchanged("~/.claude/commands"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collision/required_skill_keeps_shared_native_path",
		Desc: "Given an organization-required skill and a user-selected command with the same name, When syncing for Devin CLI and Claude Code, Then Devin's shared skills path keeps the required skill with a shared-path warning, and Claude Code installs both",
		Registries: []RegistryDef{
			{Name: "org-skill", Skills: []Skill{{Name: "audit", Description: "Mandatory organization skill"}}},
			{Name: "user-command", Commands: []Command{{Name: "audit", Description: "User selected command", Command: "echo user"}}},
		},
		OrgCfg: v4(
			"harnesses: [devin, claude]",
			"sources: {org: {git: '{{REG:org-skill}}', ref: main}}",
			"resources: {org: {all: true, required: true}}",
		),
		GlobalCfg: v4(
			"harnesses: [devin, claude]",
			"sources: {user: {git: '{{REG:user-command}}', ref: main}}",
			"resources: {user: {include: [command/audit]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").
				out("materialize.collision.shared-path").
				has("~/.config/devin/skills/audit/SKILL.md", "Mandatory organization skill").
				has("~/.claude/skills/audit/SKILL.md", "Mandatory organization skill").
				has("~/.claude/commands/audit.md", "User selected command"),
			ok("sync", "--check").unchanged("~/.config/devin/skills/audit"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collision/same_name_different_registries",
		Desc: "Given two registries each providing a skill named 'deploy', When sync is run, Then a collision warning is emitted but sync succeeds with the skill materialized from the first registry",
		Registries: []RegistryDef{
			{Name: "registry-a", Skills: []Skill{
				{Name: "deploy", Description: "Deploy from registry A", Tags: []string{"infra"}},
			}},
			{Name: "registry-b", Skills: []Skill{
				{Name: "deploy", Description: "Deploy from registry B", Tags: []string{"infra"}},
			}},
		},
		GlobalCfg: v4(
			"sources:",
			"  first: {git: '{{REG:registry-a}}', ref: main}",
			"  second: {git: '{{REG:registry-b}}', ref: main}",
			"resources:",
			"  first: {include: [skill/deploy]}",
			"  second: {include: [skill/deploy]}",
		),
		Steps: []Step{
			ok("sync").out("deploy", "multiple registries").exists("~/.factory/skills/deploy/SKILL.md"),
			// --check must explain a partial/ambiguous result for the same
			// reason `sync` does. It used to print only ineffective-subdir
			// warnings and silently drop collisions, overlaps and soft
			// selected-not-found, so a green-but-ambiguous check looked clean.
			exits(0, "sync", "--check").out("warning:", "multiple registries"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collision/same_url_different_paths",
		Desc: "Given one registry configured twice with different path values, When sync is run, Then both path-scoped resources materialize without collision",
		Registries: []RegistryDef{
			{Name: "mono-registry", Files: map[string]string{
				"team-a/skills/skill-a/SKILL.md": Skill{Name: "skill-a", Description: "Skill A", Tags: []string{"team-a"}}.SkillFile(),
				"team-b/skills/skill-b/SKILL.md": Skill{Name: "skill-b", Description: "Skill B", Tags: []string{"team-b"}}.SkillFile(),
			}},
		},
		GlobalCfg: v4(
			"sources:",
			"  first: {git: '{{REG:mono-registry}}', ref: main, path: team-a}",
			"  second: {git: '{{REG:mono-registry}}', ref: main, path: team-b}",
			"resources:",
			"  first: {include: [skill/skill-a]}",
			"  second: {include: [skill/skill-b]}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/skill-a/SKILL.md", "~/.factory/skills/skill-b/SKILL.md"),
			run("status").out("skill-a", "skill-b"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collision/overlapping_name_different_kinds",
		Desc: "Given a registry with a skill and a command sharing the name 'deploy', When sync is run, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "mixed-registry",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill", Tags: []string{"ops"}}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mixed-registry}}', ref: main}}",
			"resources: {team: {include: [skill/deploy, command/deploy]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/deploy/SKILL.md", "~/.factory/commands/deploy.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collision/allowed_registries_blocks_unauthorized",
		Desc: "Given allowed_registries restricting to internal domains, When sync is run against a file:// registry, Then it exits 1 with an allowed_registries warning",
		Registries: []RegistryDef{
			{Name: "blocked-reg", Skills: []Skill{
				{Name: "blocked-skill"},
			}},
		},
		GlobalCfg: v4(
			"allowed_registries:",
			"  - internal.company.com",
			"sources: {team: {git: '{{REG:blocked-reg}}', ref: main}}",
		),
		Steps: []Step{
			exits(1, "sync").out("allowed_registries", "blocked"),
		},
	})
}
