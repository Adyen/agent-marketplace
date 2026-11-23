package e2e_test

func init() {
	// ===== requirements =====
	// spec: resource-prerequisites/unhinted-prerequisite-uses-the-requiring-source-root
	// spec: resource-prerequisites/disabled-expansion-preserves-selection-while-previewing-prerequisites
	testTable = append(testTable, TestCase{
		Name: "requires/same_source_expansion_selects_implied",
		Registries: []RegistryDef{
			{
				Name: "requires-same-source",
				Skills: []Skill{
					{Name: "parent", Requires: []string{"skill:child"}},
					{Name: "child"},
				},
			},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nrequires: {expand: true}\nsources:\n  tools: {git: '{{REG:requires-same-source}}', ref: main}\nresources:\n  tools: {include: [skill/parent]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				Symlinks: []string{"/workspace/.factory/skills/parent", "/workspace/.factory/skills/child"}},
			{
				Action:   ActionWriteRepoConfig,
				Body:     "apiVersion: agent-marketplace/v4\nrequires: {expand: false}\nsources:\n  tools: {git: '{{REG:requires-same-source}}', ref: main}\nresources:\n  tools: {include: [skill/parent]}\n",
				Args:     []string{"sync"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "parent" requires skill "child", but it is not selected`,
				},
				Symlinks: []string{"/workspace/.factory/skills/child"},
			},
		},
	})
	// spec: resource-prerequisites/configured-git-hint-narrows-lookup-to-its-root
	testTable = append(testTable, TestCase{
		Name: "requires/configured_cross_source_hint",
		Registries: []RegistryDef{
			{
				Name:   "requires-provider",
				Skills: []Skill{{Name: "child"}, {Name: "other"}},
			},
			{
				Name: "requires-requirer",
				Files: map[string]string{
					"skills/parent/SKILL.md": "---\nname: parent\ndescription: Requiring skill\nmetadata:\n  requires:\n    - kind: skill\n      name: child\n      registry: {{REG:requires-provider}}\n---\n\n# parent\n",
				},
			},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nrequires: {expand: true}\nsources:\n  provider: {git: '{{REG:requires-provider}}', ref: main}\n  requirer: {git: '{{REG:requires-requirer}}', ref: main}\nresources:\n  provider: {include: [skill/other]}\n  requirer: {include: [skill/parent]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				Symlinks: []string{
					"/workspace/.factory/skills/parent",
					"/workspace/.factory/skills/other",
					"/workspace/.factory/skills/child",
				}},
		},
	})
	// spec: resource-prerequisites/unconfigured-git-hint-has-no-fallback
	testTable = append(testTable, TestCase{
		Name: "requires/unconfigured_hint_has_no_fallback",
		Registries: []RegistryDef{
			{
				Name: "requires-unconfigured",
				Skills: []Skill{
					{Name: "child"},
				},
				Files: map[string]string{
					"skills/parent/SKILL.md": "---\nname: parent\ndescription: Requiring skill\nmetadata:\n  requires:\n    - kind: skill\n      name: child\n      registry: https://example.invalid/not-configured.git\n---\n\n# parent\n",
				},
			},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nrequires: {expand: true}\nsources:\n  tools: {git: '{{REG:requires-unconfigured}}', ref: main}\nresources:\n  tools: {include: [skill/parent]}\n",
		Steps: []Step{
			{
				Args:     []string{"sync"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "parent" requires skill "child", but it is not selected`,
				},
				Symlinks:    []string{"/workspace/.factory/skills/parent"},
				FilesAbsent: []string{"/workspace/.factory/skills/child"},
			},
		},
	})
}
