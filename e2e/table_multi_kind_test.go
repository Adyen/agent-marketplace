package e2e_test

func init() {
	// ===== multi-resource kinds =====
	testTable = append(testTable, TestCase{
		Name: "multi_kind/subagents_materialize_as_droids",
		Desc: "Given a registry with a subagent explicitly selected, When sync is run, Then the subagent is compiled as droids/<name>.md",
		Registries: []RegistryDef{
			{Name: "subagent-reg", Subagents: []Subagent{
				{Name: "worker", Tools: []string{"Read", "Edit"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:subagent-reg}}', ref: main}}",
			"resources: {team: {include: [subagent/worker]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/worker.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "multi_kind/mcp_servers_materialize",
		Desc: "Given a registry with an MCP server explicitly selected, When sync is run, Then mcp.json is written containing the server definition",
		Registries: []RegistryDef{
			{Name: "mcp-reg", MCPServers: []MCPServer{
				{Name: "my-server", Command: "npx", Args: []string{"-y", "@my/mcp-server"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-reg}}', ref: main}}",
			"resources: {team: {include: [mcp/my-server]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "my-server"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "multi_kind/mcp_windsurf_env_normalized_to_factory",
		Desc: "Given a registry with a Windsurf-format mcp_config.json using ${env:VAR} interpolation, When sync is run with factory harness, Then the MCP server materializes with ${VAR} (canonical form) in the factory mcp.json",
		Registries: []RegistryDef{
			{Name: "windsurf-mcp-reg", Files: map[string]string{
				"mcp_config.json": `{"mcpServers":{"env-server":{"command":"npx","args":["-y","@my/env-server"],"env":{"API_KEY":"${env:MY_API_KEY}","LITERAL_VAL":"keep-me"}}}}`,
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:windsurf-mcp-reg}}', ref: main}}",
			"resources: {team: {include: [mcp/env-server]}}",
		),
		Steps: []Step{
			ok("sync").notOut("${env:MY_API_KEY}").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "env-server"),
			ok("status").out("env-server"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "multi_kind/mcp_windsurf_env_round_trip_to_windsurf",
		Desc: "Given a registry with a Windsurf-format mcp_config.json using ${env:VAR} interpolation, When sync is run with factory and windsurf harnesses, Then the windsurf mcp_config.json contains ${env:VAR} (re-expanded from canonical form)",
		Registries: []RegistryDef{
			{Name: "windsurf-rt-reg", Files: map[string]string{
				"mcp_config.json": `{"mcpServers":{"rt-server":{"command":"npx","args":["-y","@my/rt-server"],"env":{"TOKEN":"${env:MY_TOKEN}"}}}}`,
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - windsurf",
			"sources: {team: {git: '{{REG:windsurf-rt-reg}}', ref: main}}",
			"resources: {team: {include: [mcp/rt-server]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("~/.factory/mcp.json", "~/.codeium/windsurf/mcp_config.json").
				has("~/.factory/mcp.json", "${MY_TOKEN}").
				has("~/.codeium/windsurf/mcp_config.json", "${env:MY_TOKEN}"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "multi_kind/install_kind_filter_subagent",
		Desc: "Given a registry with skills and subagents, When only subagents are selected in config, Then subagents materialize but skills do not",
		Registries: []RegistryDef{
			{Name: "kind-filter", Skills: []Skill{
				{Name: "not-this-skill"},
			}, Subagents: []Subagent{
				{Name: "only-this-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:kind-filter}}', ref: main}}",
			"resources: {team: {include: [subagent/only-this-agent]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/droids/only-this-agent.md").absent("~/.factory/skills/not-this-skill"),
		},
	})
}
