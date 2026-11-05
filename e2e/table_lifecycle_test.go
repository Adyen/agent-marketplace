package e2e_test

func init() {
	testTable = append(testTable, TestCase{
		Name: "local_registry/v4_portable_directory",
		Desc: "Given repository-local sources, When syncing v4 dot and nested directory sources, Then live skills materialize without absolute config paths or Git locks",
		RepoCfg: v4(
			"sources: {local: {directory: ./resources/skills}}",
			"resources: {local: {all: true}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/local-skill/SKILL.md", (Skill{Name: "local-skill", Description: "Live local skill"}).SkillFile()),
			ok("sync").absent("./.agents/lock.yaml").symlinks("./.factory/skills/local-skill"),
			ok("sync", "--check"),
			writeFile("./skills/root-skill/SKILL.md", (Skill{Name: "root-skill", Description: "Repository root skill"}).SkillFile()),
			writeRepoCfg(v4(
				"sources: {registry: {directory: ., source_harness: claude}}",
				"resources: {registry: {include: [skill/root-skill]}}",
			)).
				then("sync").
				pass().
				symlinks("./.factory/skills/root-skill"),
		},
	})

	// ===== logging =====
	testTable = append(testTable, TestCase{
		Name: "logging/file_and_debug",
		Desc: "Given a configured registry, When list runs with and without --debug, Then operational logs are written to the sandbox cache log file and debug records appear only with the flag",
		Registries: []RegistryDef{
			{Name: "logging-registry", Skills: []Skill{
				{Name: "logging-skill", Description: "Logging test skill", Tags: []string{"ops"}},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:logging-registry}}', ref: main}}",
			"resources: {team: {include: [skill/logging-skill]}}",
		),
		Steps: []Step{
			ok("lock"),
			ok("list").
				exists("~/.agent-marketplace/logs/agent-marketplace.log").
				has("~/.agent-marketplace/logs/agent-marketplace.log", `"msg":"command start"`).
				lacks("~/.agent-marketplace/logs/agent-marketplace.log", `"level":"DEBUG"`),
			ok("--debug", "list").has("~/.agent-marketplace/logs/agent-marketplace.log", `"level":"DEBUG"`),
		},
	})

	// ===== sync edge cases =====
	testTable = append(testTable, TestCase{
		Name: "sync/strict_mode_exits_nonzero_on_missing",
		Desc: "Given a config selecting a skill absent from the registry, When sync --strict is run, Then it exits 1 mentioning the missing skill",
		Registries: []RegistryDef{
			{Name: "strict-reg", Skills: []Skill{
				{Name: "present-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:strict-reg}}', ref: main}}",
			"resources: {team: {include: [skill/present-skill, skill/absent-skill]}}",
		),
		Steps: []Step{
			exits(1, "sync", "--strict").out("absent-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "sync/idempotent_double_sync",
		Desc: "Given a skill synced once, When sync and sync --check are run again, Then both succeed with no drift",
		Registries: []RegistryDef{
			{Name: "idem-reg", Skills: []Skill{
				{Name: "idem-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:idem-reg}}', ref: main}}",
			"resources: {team: {include: [skill/idem-skill]}}",
		),
		Steps: []Step{
			ok("sync"),
			exits(0, "sync", "--check"),
			ok("sync"),
			exits(0, "sync", "--check"),
		},
	})

	// ===== other commands =====
	testTable = append(testTable, TestCase{
		Name: "version/flag_prints_version",
		Desc: "Given the e2e-test binary, When --version is run, Then it prints the version string and exits 0",
		Steps: []Step{
			ok("--version").out("e2e-test"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "list/shows_available_resources",
		Desc: "Given a locked registry with a skill, When list is run, Then the skill name appears in the output",
		Registries: []RegistryDef{
			{Name: "list-reg", Skills: []Skill{
				{Name: "visible-skill", Description: "A visible skill"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:list-reg}}', ref: main}}"),
		Steps: []Step{
			ok("lock"),
			ok("list").out("visible-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "list/kind_filter",
		Desc: "Given a locked registry with skills and commands, When list --kind command is run, Then only commands appear (no skills)",
		Registries: []RegistryDef{
			{Name: "kind-list", Skills: []Skill{
				{Name: "listed-skill"},
			}, Commands: []Command{
				{Name: "listed-cmd", Command: "echo hi"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:kind-list}}', ref: main}}"),
		Steps: []Step{
			ok("lock"),
			ok("list", "--kind", "command").out("listed-cmd").notOut("listed-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "doctor/reports_harness",
		Desc: "Given a repo with synced factory artifacts, When doctor is run, Then it reports 'factory' in the output",
		Registries: []RegistryDef{
			{Name: "doc-reg", Skills: []Skill{
				{Name: "doc-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:doc-reg}}', ref: main}}",
			"resources: {team: {include: [skill/doc-skill]}}",
		),
		Steps: []Step{
			ok("sync"),
			ok("doctor").out("factory"),
		},
	})

	// ===== complex multi-step =====
	testTable = append(testTable, TestCase{
		Name: "lifecycle/full_workflow",
		Desc: "Given a skill in global config, When sync→status→remove→sync cycle is run, Then the skill appears, is visible in status, and is pruned after removal",
		Registries: []RegistryDef{
			{Name: "lifecycle-reg", Skills: []Skill{
				{Name: "lifecycle-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:lifecycle-reg}}', ref: main}}",
			"resources: {team: {include: [skill/lifecycle-skill]}}",
		),
		Steps: []Step{
			// Sync materializes
			ok("sync").exists("~/.factory/skills/lifecycle-skill/SKILL.md"),
			// Status shows it
			ok("status").out("lifecycle-skill"),
			// Check shows in sync
			exits(0, "sync", "--check"),
			// Remove
			ok("remove", "--scope", "user", "lifecycle-skill"),
			// Sync prunes
			ok("sync").absent("~/.factory/skills/lifecycle-skill"),
			// Check shows in sync (no drift)
			exits(0, "sync", "--check"),
		},
	})

	// ===== local config registries =====
	testTable = append(testTable, TestCase{
		Name: "local_registry/lifecycle",
		Desc: "Given a global registry with skill A and a local config with a registries entry for a second registry with skill B, When sync is run, Then both A and B are materialized",
		Registries: []RegistryDef{
			{Name: "first", Skills: []Skill{
				{Name: "alpha", Description: "Alpha skill"},
			}},
			{Name: "second", Skills: []Skill{
				{Name: "beta", Description: "Beta skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {personal: {git: '{{REG:first}}', ref: main}}",
			"resources: {personal: {all: true}}",
		),
		LocalCfg: v4(
			"sources: {local: {git: '{{REG:second}}', ref: main}}",
			"resources: {local: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/alpha/SKILL.md", "./.factory/skills/beta/SKILL.md"),
			ok("status").out("alpha", "beta"),
			ok("remove", "--scope", "local", "beta"),
			ok("sync").exists("~/.factory/skills/alpha/SKILL.md").absent("./.factory/skills/beta"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "local_registry/per_registry_exclude",
		Desc: "Given a local config with a registries entry that has skills.exclude, When sync is run, Then the excluded skill is not materialized",
		Registries: []RegistryDef{
			{Name: "exclude-reg", Skills: []Skill{
				{Name: "kept", Description: "Kept skill"},
				{Name: "dropped", Description: "Dropped skill"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:exclude-reg}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:exclude-reg}}', ref: main}}",
			"resources: {team: {exclude: [skill/dropped]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/kept/SKILL.md").absent("./.factory/skills/dropped"),
		},
	})

	// ===== init =====
	testTable = append(testTable, TestCase{
		Name: "init/is_idempotent",
		Desc: "Given an existing user personal config, When init is run again, Then it exits 0 and does not modify the file",
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
		),
		Steps: []Step{
			ok("init", "--harnesses", "claude").out("already initialized"),
			ok("init", "--harnesses", "claude").has("~/.agents/config.yaml", "- factory").lacks("~/.agents/config.yaml", "- claude"),
		},
	})

	// ===== telemetry =====
	testTable = append(testTable, TestCase{
		Name: "telemetry/commands",
		Desc: "Given a minimal user personal config, When the telemetry control commands are exercised, Then status, enable, preview, disable, and purge behave as expected",
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
		),
		Steps: []Step{
			ok("telemetry", "status").out("Telemetry status", "Effective level:"),
			ok("telemetry", "enable", "--level", "basic").out("Telemetry enabled", "basic"),
			ok("telemetry", "status").out("Effective level:", "basic", "(user)"),
			ok("telemetry", "preview", "--level", "basic").out("Preview payload written to", "level:", "basic"),
			ok("telemetry", "disable").out("Telemetry disabled"),
			ok("telemetry", "status").out("Effective level:", "off").notOut("basic"),
			ok("telemetry", "purge"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "ingest/bare_bundle_mcp",
		Desc: "Given a bare Claude bundle with skills, a command, and undotted MCP configuration, when list and sync run, then MCP is discovered and materialized without changing the native definition.",
		Registries: []RegistryDef{{Name: "bare-bundle-mcp", Skills: []Skill{{Name: "demo"}}, Commands: []Command{{Name: "hi"}}, Files: map[string]string{
			"mcp.json": (MCPServer{Name: "probe", Command: "npx", Args: []string{"-y", "x"}}).MCPFile(),
		}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:bare-bundle-mcp}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("lock"),
			ok("list", "--kind", "skill").out("demo"),
			ok("list", "--kind", "command").out("hi"),
			ok("list", "--kind", "mcp").out("probe"),
			ok("sync").has("./.factory/mcp.json", "\"probe\": {"),
			Step{}.has("./.factory/mcp.json", "\"command\": \"npx\""),
			Step{}.has("./.factory/mcp.json", "\"-y\""),
			Step{}.has("./.factory/mcp.json", "\"x\""),
			ok("sync", "--check"),
			ok("status").out("probe"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "ingest/unconsumed_container",
		Desc: "Given a diagnostic-only Factory container with a nested skill, when lock, list, sync, and status run, then the child resource is discovered while the container audit remains visible.",
		Registries: []RegistryDef{{Name: "unconsumed-container", Files: map[string]string{
			"group/.factory-plugin/plugin.json": `{"name":"group"}`,
			"group/.mcp.json":                   `{"mcpServers":{"foreign":{"command":"npx"}}}`,
			"group/child/skills/demo/SKILL.md":  "---\nname: demo\ndescription: Demo\n---\nDemo\n",
		}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:unconsumed-container}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("lock"),
			ok("list", "--kind", "skill").out("demo", "ingest.factory.unconsumed-config"),
			ok("sync").has("./.factory/skills/demo/SKILL.md", "Demo"),
			ok("sync", "--check"),
			ok("status").out("demo", "ingest.factory.unconsumed-config"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "ingest/unconsumed_config",
		Desc: "Given an explicit Claude plugin with native MCP and foreign configurations, when list, sync, and status run, then the foreign files are warned about while the selected native MCP remains materializable.",
		Registries: []RegistryDef{{Name: "unconsumed-config", Skills: []Skill{{Name: "demo"}}, Files: map[string]string{
			".claude-plugin/plugin.json": `{"name":"unconsumed-config"}`,
			".mcp.json":                  (MCPServer{Name: "native", Command: "npx", Args: []string{"-y", "native"}}).MCPFile(),
			"mcp.json":                   `{"mcpServers":{"foreign":{"command":"npx"}}}`,
			"gemini-extension.json":      `{"name":"gemini","mcpServers":{"foreign-gemini":{"command":"npx"}}}`,
		}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:unconsumed-config}}', ref: main, source_harness: claude}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("lock"),
			ok("list").out("ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"),
			ok("sync").has("./.factory/mcp.json", "native").lacks("./.factory/mcp.json", "foreign"),
			ok("sync", "--check"),
			ok("status").out("ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "cli/help_and_completion",
		Desc: "Given any configuration, When help and every completion script are requested, Then each prints its generated text",
		Steps: []Step{
			ok("help").out("Usage:", "Available Commands:"),
			ok("completion").out("Usage:", "bash"),
			ok("completion", "bash").out("bash completion"),
			ok("completion", "zsh").out("#compdef agent-marketplace"),
			ok("completion", "fish").out("complete -c agent-marketplace"),
			ok("completion", "powershell").out("Register-ArgumentCompleter"),
		},
	})

	// spec: logging/environment-selects-the-log-file-and-debug-level
	testTable = append(testTable, TestCase{
		Name:       "logging/environment_selects_log_file_and_debug_level",
		Registries: []RegistryDef{{Name: "env-log", Skills: []Skill{{Name: "log-skill"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:env-log}}', ref: main}}",
			"resources: {team: {include: [skill/log-skill]}}",
			"logging: {file: '$HOME/logs/configured.log'}",
		),
		Steps: []Step{
			ok("lock").
				env("AGENT_MARKETPLACE_LOG_FILE", "$HOME/logs/environment.log").
				env("AGENT_MARKETPLACE_DEBUG", "1").
				exists("~/logs/environment.log").
				absent("~/logs/configured.log").
				has("~/logs/environment.log", `"level":"DEBUG"`),
		},
	})
}
