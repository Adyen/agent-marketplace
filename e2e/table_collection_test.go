package e2e_test

func init() {
	// spec: resource-selection/unqualified-member-retains-the-collection-owner
	testTable = append(testTable, TestCase{
		Name: "collection/unqualified_repo_member_keeps_scope",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Repository collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:shared-bundle}}', ref: main}}",
		),
		RepoCfg: v4(
			"sources: {repo: {git: '{{REG:shared-bundle}}', ref: main}}",
			"resources: {repo: {include: [collection/bundle]}}",
		),
		Steps: []Step{
			ok("sync").
				notOut("multiple registries").
				absent("~/.factory/skills/shared").
				symlinks("./.factory/skills/shared").
				has("./.factory/skills/shared/SKILL.md", "Repository collection member"),
			ok("sync", "--check").absent("~/.factory/skills/shared").unchanged("./.factory/skills/shared"),
		},
	})

	// spec: resource-selection/same-collection-remains-selected-in-both-install-scopes
	testTable = append(testTable, TestCase{
		Name: "collection/unqualified_members_keep_both_scopes",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Shared collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:shared-bundle}}', ref: main}}",
			"resources: {user: {include: [collection/bundle]}}",
		),
		RepoCfg: v4(
			"sources: {repo: {git: '{{REG:shared-bundle}}', ref: main}}",
			"resources: {repo: {include: [collection/bundle]}}",
		),
		Steps: []Step{
			ok("sync").
				notOut("multiple registries").
				symlinks("~/.factory/skills/shared", "./.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "Shared collection member").
				has("./.factory/skills/shared/SKILL.md", "Shared collection member"),
			ok("sync", "--check").notOut("multiple registries").unchanged("~/.factory/skills/shared", "./.factory/skills/shared"),
		},
	})

	// spec: resource-selection/unqualified-required-member-retains-required-policy
	testTable = append(testTable, TestCase{
		Name: "collection/unqualified_required_member_keeps_policy",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Required collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		OrgCfg: v4("sources: {org: {git: '{{REG:shared-bundle}}', ref: main}}"),
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {user: {git: '{{REG:shared-bundle}}', ref: main}}",
			"resources: {user: {include: [collection/bundle], required: true}}",
		),
		RepoCfg: v4(
			"sources: {repo: {directory: ./resources}}",
			"resources: {repo: {include: [skill/shared]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/shared/SKILL.md", (Skill{Name: "shared", Description: "Optional repository selection"}).SkillFile()),
			ok("sync").
				out("multiple registries").
				absent("./.factory/skills/shared").
				symlinks("~/.factory/skills/shared").
				has("~/.factory/skills/shared/SKILL.md", "Required collection member"),
			ok("sync", "--check").absent("./.factory/skills/shared").unchanged("~/.factory/skills/shared"),
		},
	})

	// ===== collections =====
	// spec: resource-selection/collection-keeps-required-all-selection
	testTable = append(testTable, TestCase{
		Name: "collection/required_org_survives_collection_selection",
		Registries: []RegistryDef{
			{Name: "mandatory-collection", Skills: []Skill{{Name: "mandatory-alpha"}, {Name: "mandatory-beta"}}},
			{Name: "optional-collection", Skills: []Skill{{Name: "optional-member"}}, Files: map[string]string{
				"collections/shared/COLLECTION.yaml": "name: shared\nversion: '1.0.0'\nresources:\n  - name: optional-member\n",
			}},
		},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources: {mandatory: {git: '{{REG:mandatory-collection}}', ref: main}}",
			"resources: {mandatory: {all: true, required: true}}",
		),
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {optional: {git: '{{REG:optional-collection}}', ref: main}}",
			"resources: {optional: {include: [collection/shared]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("~/.factory/skills/mandatory-alpha/SKILL.md", "~/.factory/skills/mandatory-beta/SKILL.md", "./.factory/skills/optional-member/SKILL.md"),
			ok("sync", "--locked").unchanged("~/.factory/skills/mandatory-alpha", "~/.factory/skills/mandatory-beta", "./.factory/skills/optional-member"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collection/multi_kind_resources_materialize",
		Desc: "Given a registry with a collection referencing skills, commands, and subagents, When sync is run, Then all resource kinds materialize correctly",
		Registries: []RegistryDef{
			{Name: "collection-reg",
				Skills:    []Skill{{Name: "threat-model", Description: "Threat modeling skill"}},
				Commands:  []Command{{Name: "security-scan", Description: "Security scan command", Command: "echo scan"}},
				Subagents: []Subagent{{Name: "reviewer", Description: "Code reviewer agent", Tools: []string{"Read"}}},
				Files: map[string]string{
					"collections/security/COLLECTION.yaml": `name: security
version: "1.0"
description: Security tooling collection
resources:
  - name: threat-model
  - name: security-scan
    kind: command
  - name: reviewer
    kind: subagent
`,
				}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:collection-reg}}', ref: main}}",
			"resources: {team: {include: [collection/security]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/threat-model/SKILL.md", "~/.factory/commands/security-scan.md", "~/.factory/droids/reviewer.md"),
			run("status").out("threat-model", "security-scan", "reviewer", "collection:security"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collection/cross_kind_same_name_no_collision",
		Desc: "Given a collection with a skill and a command sharing the name 'build', When sync is run, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "same-name-reg",
				Skills:   []Skill{{Name: "build", Description: "Build skill"}},
				Commands: []Command{{Name: "build", Description: "Build command", Command: "echo build"}},
				Files: map[string]string{
					"collections/dev/COLLECTION.yaml": `name: dev
version: "1.0"
resources:
  - name: build
  - name: build
    kind: command
`,
				}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:same-name-reg}}', ref: main}}",
			"resources: {team: {include: [collection/dev]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/build/SKILL.md", "~/.factory/commands/build.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "collection/unknown_kind_errors",
		Desc: "Given a collection with an invalid kind value, When sync is run, Then an error mentions the unsupported kind",
		Registries: []RegistryDef{
			{Name: "bad-kind-reg",
				Skills: []Skill{{Name: "good-skill"}},
				Files: map[string]string{
					"collections/bad/COLLECTION.yaml": `name: bad
version: "1.0"
resources:
  - name: good-skill
  - name: broken
    kind: commnad
`,
				}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:bad-kind-reg}}', ref: main}}",
			"resources: {team: {include: [collection/bad]}}",
		),
		Steps: []Step{
			exits(1, "sync").out("unsupported collection member kind", "commnad"),
		},
	})
}
