package e2e_test

func init() {
	// spec: resource-removal/disambiguate-collections-before-removal
	testTable = append(testTable, TestCase{
		Name: "selection/remove_collection_stale_kind_ambiguity",
		RepoCfg: v4(
			"sources: {team: {directory: ./resources, source_harness: factory}}",
			"resources: {team: [collection/team, skill/team, skill/keep]}",
		),
		Steps: []Step{
			writeFile("./resources/skills/member/SKILL.md", (Skill{Name: "member", Description: "Collection member"}).SkillFile()),
			writeFile("./resources/skills/keep/SKILL.md", (Skill{Name: "keep", Description: "Keep selected"}).SkillFile()),
			writeFile("./resources/collections/team/COLLECTION.yaml", "name: team\nresources:\n  - name: member\n    kind: skill\n"),
			ok("sync").exists("./.factory/skills/member/SKILL.md", "./.factory/skills/keep/SKILL.md"),
			exits(1, "remove", "team").out("ambiguous across kinds", "--kind").unchanged("./.agents", "./.factory"),
			exits(1, "remove", "team", "--from", "./resources").out("ambiguous across kinds", "--kind").unchanged("./.agents", "./.factory"),
			ok("remove", "team", "--from", "./resources", "--kind", "skill").
				unchanged("./.factory/skills/member", "./.factory/skills/keep").
				has("./.agents/config.yaml", "collection/team").
				lacks("./.agents/config.yaml", "skill/team"),
			ok("remove", "team").absent("./.factory/skills/member").unchanged("./.factory/skills/keep"),
			ok("sync", "--check"),
		},
	})

	// spec: resource-removal/remove-a-selection-after-its-resource-disappears
	testTable = append(testTable, TestCase{
		Name:       "selection/remove_undiscoverable_git_selection",
		Registries: []RegistryDef{{Name: "missing-selection", Skills: []Skill{{Name: "keep", Description: "Keep selected"}}}},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:missing-selection}}', ref: main}}",
			"resources: {team: [skill/gone, skill/keep]}",
		),
		Steps: []Step{
			ok("sync").out(`skill "gone" was selected but not found`).exists("./.factory/skills/keep/SKILL.md"),
			ok("remove", "gone").out("Removed gone").has("./.agents/config.yaml", "skill/keep").lacks("./.agents/config.yaml", "skill/gone"),
			ok("sync").notOut(`"gone"`).exists("./.factory/skills/keep/SKILL.md"),
			ok("sync", "--check"),
		},
	})

	// spec: resource-removal/remove-a-malformed-explicit-selection
	testTable = append(testTable, TestCase{
		Name: "selection/remove_malformed_command",
		RepoCfg: v4(
			"sources: {team: {directory: ./resources, source_harness: factory}}",
			"resources: {team: [command/gone, skill/keep]}",
		),
		Steps: []Step{
			writeFile("./resources/commands/gone.md", (Command{Name: "gone", Description: "Will become malformed"}).CommandFile()),
			writeFile("./resources/skills/keep/SKILL.md", (Skill{Name: "keep", Description: "Keep selected"}).SkillFile()),
			ok("sync").exists("./.factory/commands/gone.md"),
			writeFile("./resources/commands/gone.md", "---\ndescription: [invalid\n---\n"),
			ok("sync").out(`command "gone" was selected and found`, "failed to load"),
			ok("remove", "gone", "--from", "./resources").unchanged("./resources").lacks("./.agents/config.yaml", "command/gone"),
			ok("sync").notOut(`command "gone" was selected`).exists("./.factory/skills/keep/SKILL.md"),
		},
	})

	// spec: regression-gates/v4-selection-workflows-remain-usable
	testTable = append(testTable, TestCase{
		Name: "selection/v4_bulk_selection_lifecycle",
		Registries: []RegistryDef{{Name: "migrated-bulk", Skills: []Skill{
			{Name: "review", Description: "Review fixture"},
			{Name: "deploy", Description: "Deploy fixture"},
		}}},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:migrated-bulk}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("install", "--all").has("./.factory/skills/review/SKILL.md", "Review fixture").has("./.factory/skills/deploy/SKILL.md", "Deploy fixture"),
			ok("remove", "deploy").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("sync", "--check"),
		},
	})

	// spec: regression-gates/v4-selection-workflows-remain-usable
	testTable = append(testTable, TestCase{
		Name: "selection/v4_tag_selection_lifecycle",
		Registries: []RegistryDef{{Name: "migrated-tag", Skills: []Skill{
			{Name: "review", Description: "Review fixture"},
			{Name: "deploy", Description: "Deploy fixture", Tags: []string{"ops"}},
			{Name: "unselected", Description: "Unselected fixture"},
		}}},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:migrated-tag}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy", "./.factory/skills/unselected"),
			ok("install", "--tag", "ops").
				absent("./.factory/skills/unselected").
				has("./.factory/skills/review/SKILL.md", "Review fixture").
				has("./.factory/skills/deploy/SKILL.md", "Deploy fixture"),
			ok("remove", "deploy").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy", "./.factory/skills/unselected"),
			ok("sync", "--check"),
		},
	})

	// spec: regression-gates/v4-selection-workflows-remain-usable
	testTable = append(testTable, TestCase{
		Name: "selection/v4_ad_hoc_selection_lifecycle",
		Registries: []RegistryDef{
			{Name: "migrated-original", Skills: []Skill{{Name: "review", Description: "Review fixture"}}},
			{Name: "migrated-ad-hoc", Skills: []Skill{{Name: "deploy", Description: "Deploy fixture"}}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:migrated-original}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("install", "deploy", "--from", "{{REG:migrated-ad-hoc}}", "--ref", "main", "--scope", "repo").
				has("./.factory/skills/review/SKILL.md", "Review fixture").
				has("./.factory/skills/deploy/SKILL.md", "Deploy fixture"),
			ok("remove", "deploy", "--from", "{{REG:migrated-ad-hoc}}", "--ref", "main", "--scope", "repo").
				exists("./.factory/skills/review/SKILL.md").
				absent("./.factory/skills/deploy"),
			ok("sync").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "selection/local_resources_bind_repository_source",
		Desc:       "Given matching v4 repository and personal-local source declarations, When syncing the local selection, Then the shared provider materializes without editing either root",
		Registries: []RegistryDef{{Name: "local-selector", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		RepoCfg:    v4("sources: {team: {git: '{{REG:local-selector}}', ref: main}}"),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:local-selector}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/review/SKILL.md"),
			ok("sync").unchanged("./.agents/config.local.yaml", "./.agents/config.yaml"),
			ok("sync", "--check").exists("./.factory/skills/review/SKILL.md"),
		},
	})
}
