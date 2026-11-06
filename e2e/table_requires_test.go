package e2e_test

func init() {
	// spec: resource-prerequisites/unhinted-prerequisite-uses-the-requiring-source-root
	// spec: resource-prerequisites/disabled-expansion-preserves-selection-while-previewing-prerequisites
	testTable = append(testTable, TestCase{
		Name: "requires/same_source_expansion_selects_implied",
		Registries: []RegistryDef{{
			Name: "requires-same-source",
			Skills: []Skill{
				{Name: "parent", Requires: []string{"skill:child"}},
				{Name: "child"},
			},
		}},
		RepoCfg: v4(
			"requires: {expand: true}",
			"sources: {tools: {git: '{{REG:requires-same-source}}', ref: main}}",
			"resources: {tools: {include: [skill/parent]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.factory/skills/parent", "./.factory/skills/child"),
			writeRepoCfg(v4(
				"requires: {expand: false}",
				"sources: {tools: {git: '{{REG:requires-same-source}}', ref: main}}",
				"resources: {tools: {include: [skill/parent]}}",
			)),
			ok("sync").out(`warning: skill "parent" requires skill "child", but it is not selected`).
				symlinks("./.factory/skills/child").unchanged("./.factory/skills/child"),
		},
	})

	// spec: resource-prerequisites/configured-git-hint-narrows-lookup-to-its-root
	testTable = append(testTable, TestCase{
		Name: "requires/configured_cross_source_hint",
		Registries: []RegistryDef{
			{Name: "requires-provider", Skills: []Skill{{Name: "child"}, {Name: "other"}}},
			{
				Name: "requires-requirer",
				Files: map[string]string{
					"skills/parent/SKILL.md": "---\nname: parent\ndescription: Requiring skill\nmetadata:\n  requires:\n    - kind: skill\n      name: child\n      registry: {{REG:requires-provider}}\n---\n\n# parent\n",
				},
			},
		},
		RepoCfg: v4(
			"requires: {expand: true}",
			"sources:",
			"  provider: {git: '{{REG:requires-provider}}', ref: main}",
			"  requirer: {git: '{{REG:requires-requirer}}', ref: main}",
			"resources:",
			"  provider: {include: [skill/other]}",
			"  requirer: {include: [skill/parent]}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.factory/skills/parent", "./.factory/skills/other", "./.factory/skills/child"),
		},
	})

	// spec: resource-prerequisites/unconfigured-git-hint-has-no-fallback
	testTable = append(testTable, TestCase{
		Name: "requires/unconfigured_hint_has_no_fallback",
		Registries: []RegistryDef{{
			Name:   "requires-unconfigured",
			Skills: []Skill{{Name: "child"}},
			Files: map[string]string{
				"skills/parent/SKILL.md": "---\nname: parent\ndescription: Requiring skill\nmetadata:\n  requires:\n    - kind: skill\n      name: child\n      registry: https://example.invalid/not-configured.git\n---\n\n# parent\n",
			},
		}},
		RepoCfg: v4(
			"requires: {expand: true}",
			"sources: {tools: {git: '{{REG:requires-unconfigured}}', ref: main}}",
			"resources: {tools: {include: [skill/parent]}}",
		),
		Steps: []Step{
			ok("sync").out(`warning: skill "parent" requires skill "child", but it is not selected`).
				symlinks("./.factory/skills/parent").absent("./.factory/skills/child"),
		},
	})
}
