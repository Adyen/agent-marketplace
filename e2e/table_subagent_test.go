package e2e_test

func init() {
	// ===== subagent renormalization & regression =====
	testTable = append(testTable, TestCase{
		Name: "subagent/model_mapping_to_factory",
		Desc: "Given a registry with a subagent using model=sonnet, When sync is run, Then the droid file contains the mapped Factory model ID",
		Registries: []RegistryDef{
			{Name: "sub-model", Subagents: []Subagent{
				{Name: "model-agent", Model: "sonnet", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-model}}', ref: main}}",
			"resources: {team: {include: [subagent/model-agent]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/model-agent.md").has("~/.factory/droids/model-agent.md", "model: claude-sonnet-5"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/unknown_model_omitted_without_failing_sync",
		Desc: "Given an unknown cross-harness model, When sync runs, Then a warning is emitted while the droid body and tools survive without the model and repeated sync converges",
		Registries: []RegistryDef{
			{Name: "sub-model-pt", Subagents: []Subagent{
				{Name: "passthrough-agent", Model: "custom-model-v2", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-model-pt}}', ref: main}}",
			"resources: {team: {include: [subagent/passthrough-agent]}}",
		),
		Steps: []Step{
			ok("sync").
				out("model.unresolved").
				exists("~/.factory/droids/passthrough-agent.md").
				has("~/.factory/droids/passthrough-agent.md", "# passthrough-agent").
				lacks("~/.factory/droids/passthrough-agent.md", "model:"),
			Step{}.has("~/.factory/droids/passthrough-agent.md", "- Read"),
			ok("sync").unchanged("~/.factory/droids/passthrough-agent.md"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "subagent/inheritance_uses_devin_default_without_failing",
		Desc:       "Given an inherited model and effort preference, When compiling for Devin, Then sync warns about default selection without treating inherit as an unknown model and keeps the resource",
		Registries: []RegistryDef{{Name: "sub-inherit", Subagents: []Subagent{{Name: "inherited-agent", Model: "inherit", Effort: "medium", Tools: []string{"Read"}}}}},
		GlobalCfg: v4(
			"harnesses: [devin]",
			"sources: {team: {git: '{{REG:sub-inherit}}', ref: main}}",
			"resources: {team: {include: [subagent/inherited-agent]}}",
		),
		Steps: []Step{
			ok("sync").
				out("subagent.devin.model-default", "subagent.devin.effort-dropped").
				notOut("subagent.devin.model-unmappable").
				has("~/.config/devin/agents/inherited-agent.md", "# inherited-agent").
				lacks("~/.config/devin/agents/inherited-agent.md", "model:"),
			Step{}.has("~/.config/devin/agents/inherited-agent.md", "- read"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/write_tool_maps_to_create",
		Desc: "Given a registry with a subagent using Write tool, When sync is run for factory, Then Write maps to Create alongside the existing Read tool",
		Registries: []RegistryDef{
			{Name: "sub-tools", Subagents: []Subagent{
				{Name: "write-agent", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-tools}}', ref: main}}",
			"resources: {team: {include: [subagent/write-agent]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/write-agent.md").has("~/.factory/droids/write-agent.md", "- Read"),
			Step{}.has("~/.factory/droids/write-agent.md", "- Create").lacks("~/.factory/droids/write-agent.md", "  - Edit"),
			Step{}.lacks("~/.factory/droids/write-agent.md", "  - Write"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/unknown_tool_refuses_output",
		Desc: "Given an unrepresentable Factory tool restriction and a valid sibling, When sync runs, Then the restricted droid is suppressed with an error while the valid sibling is installed",
		Registries: []RegistryDef{
			{Name: "sub-tool-pt", Subagents: []Subagent{
				{Name: "unk-tool-agent", Tools: []string{"Read", "CustomTool"}},
				{Name: "known-tool-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-tool-pt}}', ref: main}}",
			"resources: {team: {include: [subagent/unk-tool-agent, subagent/known-tool-agent]}}",
		),
		Steps: []Step{
			exits(1, "sync").
				out("subagent.factory.tool-unknown").
				exists("~/.factory/droids/known-tool-agent.md").
				absent("~/.factory/droids/unk-tool-agent.md").
				has("~/.factory/droids/known-tool-agent.md", "- Read"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/effort_renamed_to_reasoning_effort",
		Desc: "Given a registry with a subagent using effort=high, When sync is run for factory, Then the droid file contains reasoningEffort: high and not effort:",
		Registries: []RegistryDef{
			{Name: "sub-effort", Subagents: []Subagent{
				{Name: "effort-agent", Model: "gpt-5.6-terra", Effort: "high", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-effort}}', ref: main}}",
			"resources: {team: {include: [subagent/effort-agent]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/effort-agent.md").has("~/.factory/droids/effort-agent.md", "reasoningEffort: high"),
		},
	})

	testTable = append(testTable, TestCase{
		// disallowedTools and isolation are RESTRICTIONS. Factory has no way to
		// express either, so emitting the droid with the field merely omitted
		// would hand the agent BROADER permissions than the author wrote. The
		// compiler fails closed instead (compile/factory/translator.go), and
		// these two cases pin that: artifact suppressed, sync exits 1.
		Name: "subagent/disallowed_tools_fails_closed",
		Desc: "Given a registry with a subagent having disallowedTools, When sync is run for factory, Then the artifact is suppressed and sync exits 1 rather than silently granting the disallowed tool",
		Registries: []RegistryDef{
			{Name: "sub-disallowed", Subagents: []Subagent{
				{Name: "disallowed-agent", Tools: []string{"Read"}, DisallowedTools: []string{"Bash"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-disallowed}}', ref: main}}",
			"resources: {team: {include: [subagent/disallowed-agent]}}",
		),
		Steps: []Step{
			exits(1, "sync").out("subagent.factory.disallowed-tools-dropped").absent("~/.factory/droids/disallowed-agent.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/isolation_fails_closed",
		Desc: "Given a registry with a subagent having isolation=worktree, When sync is run for factory, Then the artifact is suppressed and sync exits 1 rather than emitting an unisolated droid",
		Registries: []RegistryDef{
			{Name: "sub-isolation", Subagents: []Subagent{
				{Name: "isolation-agent", Tools: []string{"Read"}, Isolation: "worktree"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-isolation}}', ref: main}}",
			"resources: {team: {include: [subagent/isolation-agent]}}",
		),
		Steps: []Step{
			exits(1, "sync").out("subagent.factory.isolation-dropped").absent("~/.factory/droids/isolation-agent.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/install_kind_subagent_disambiguation",
		Desc: "Given a registry with a skill and a subagent sharing the same name, When install --kind subagent is run, Then the subagent is installed and the skill is not",
		Registries: []RegistryDef{
			{Name: "sub-disambig", Skills: []Skill{
				{Name: "shared-name"},
			}, Subagents: []Subagent{
				{Name: "shared-name", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4("sources: {}"),
		RepoCfg:   v4("sources: {}"),
		Steps: []Step{
			ok("install", "--kind", "subagent", "--scope", "user", "shared-name", "--from", "{{REG:sub-disambig}}", "--ref", "main").out("shared-name"),
			ok("sync").exists("~/.factory/droids/shared-name.md").absent("~/.factory/skills/shared-name"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/remove_round_trip",
		Desc: "Given a subagent installed in global config, When remove is run and sync follows, Then the subagent is removed from both config and filesystem",
		Registries: []RegistryDef{
			{Name: "sub-remove", Subagents: []Subagent{
				{Name: "removable-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-remove}}', ref: main}}",
			"resources: {team: {include: [subagent/removable-agent]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/removable-agent.md"),
			ok("remove", "--scope", "user", "removable-agent").out("Removed"),
			ok("sync").absent("~/.factory/droids/removable-agent.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/remove_from_v4",
		Desc: "Given v4 user Git selections, When a named subagent is removed with --from, Then only that subagent is removed and the exact lock survives",
		Registries: []RegistryDef{{Name: "v4-sub-remove", Subagents: []Subagent{
			{Name: "removable-agent", Tools: []string{"Read"}},
			{Name: "keep-agent", Tools: []string{"Read"}},
		}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  agents: {git: \"{{REG:v4-sub-remove}}\", ref: main}",
			"resources:",
			"  agents: {include: [subagent/removable-agent, subagent/keep-agent]}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/removable-agent.md", "~/.factory/droids/keep-agent.md"),
			ok("remove", "--kind", "subagent", "--scope", "user", "--from", "{{REG:v4-sub-remove}}", "removable-agent").
				absent("~/.factory/droids/removable-agent.md").
				unchanged("~/.agents/lock.yaml", "~/.factory/droids/keep-agent.md"),
			ok("sync").exists("~/.factory/droids/keep-agent.md").absent("~/.factory/droids/removable-agent.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/remove_from_no_name_removes_entry",
		Desc: "Given a registry entry in global config with a subagent, When remove --scope user --from removes the entire entry and re-syncs, Then the subagent is gone",
		Registries: []RegistryDef{
			{Name: "sub-from-entry-rm", Subagents: []Subagent{
				{Name: "entry-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-from-entry-rm}}', ref: main}}",
			"resources: {team: {include: [subagent/entry-agent]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/entry-agent.md"),
			ok("remove", "--scope", "user", "--from", "{{REG:sub-from-entry-rm}}").out("Removed"),
			ok("sync").absent("~/.factory/droids/entry-agent.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "subagent/multi_harness_output",
		Desc: "Given factory and cursor harnesses configured with a tool-restricted subagent, When sync is run, Then Cursor fails closed on the unrepresentable restriction while the valid Factory sibling is materialized",
		Registries: []RegistryDef{
			{Name: "sub-multi", Subagents: []Subagent{
				{Name: "multi-agent", Model: "sonnet", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - cursor",
			"sources: {team: {git: '{{REG:sub-multi}}', ref: main}}",
			"resources: {team: {include: [subagent/multi-agent]}}",
		),
		Steps: []Step{
			exits(1, "sync").
				out("subagent.cursor.tool-unknown").
				exists("~/.factory/droids/multi-agent.md").
				absent("~/.cursor/agents/multi-agent.md").
				has("~/.factory/droids/multi-agent.md", "model: claude-sonnet-5"),
			Step{}.has("~/.factory/droids/multi-agent.md", "- Read"),
			Step{}.has("~/.factory/droids/multi-agent.md", "- Create").lacks("~/.factory/droids/multi-agent.md", "  - Edit"),
		},
	})

	testTable = append(testTable, TestCase{
		// The fixture deliberately carries NO disallowedTools and NO isolation.
		// Factory fails closed on both (see subagent/disallowed_tools_fails_closed
		// and subagent/isolation_fails_closed), which suppresses the artifact
		// entirely — so including them here would leave every renormalization
		// assertion below with no file to inspect, and the test would pass on an
		// absent artifact rather than on a correctly rewritten one.
		Name: "subagent/full_renormalization_end_to_end",
		Desc: "Given a registry with a subagent using model=sonnet, effort=high, tools=[Read,Write], When sync is run for factory, Then all renormalizations apply: model mapped, effort→reasoningEffort, Write→Create",
		Registries: []RegistryDef{
			{Name: "sub-full", Subagents: []Subagent{
				{Name: "full-agent", Model: "sonnet", Tools: []string{"Read", "Write"}, Effort: "high"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:sub-full}}', ref: main}}",
			"resources: {team: {include: [subagent/full-agent]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("~/.factory/droids/full-agent.md").
				has("~/.factory/droids/full-agent.md", "model: claude-sonnet-5").
				lacks("~/.factory/droids/full-agent.md", "isolation:"),
			Step{}.has("~/.factory/droids/full-agent.md", "reasoningEffort: high").lacks("~/.factory/droids/full-agent.md", "\neffort:"),
			Step{}.has("~/.factory/droids/full-agent.md", "- Read"),
			Step{}.has("~/.factory/droids/full-agent.md", "- Create").lacks("~/.factory/droids/full-agent.md", "  - Edit"),
			Step{}.lacks("~/.factory/droids/full-agent.md", "  - Write"),
		},
	})
}
