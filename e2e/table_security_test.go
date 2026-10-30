package e2e_test

import (
	"strings"
)

func init() {
	testTable = append(testTable, TestCase{
		Name: "security/cli_warning_controls",
		Desc: "Given a selected missing resource with a control-bearing v4 directory source identity, When syncing and listing, Then warning display is escaped and selection remains unchanged",
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {directory: \"./warning-fixture/REF\\u001b[31m\\u202e\"}}",
			"resources: {team: {include: [skill/missing]}}",
		),
		Steps: []Step{
			writeFile("./warning-fixture/REF\x1b[31m\u202e/skills/control/SKILL.md", "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"),
			ok("sync").out("warning:", `REF\x1b[31m\u202e`).notOut("\x1b", "\u202e").unchanged("./.agents/config.yaml"),
			ok("list").out(`REF\x1b[31m\u202e`, "control").notOut("\x1b", "\u202e").unchanged("./.agents/config.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/telemetry_failure_controls",
		Desc: "Given a persisted delivery failure containing terminal controls, When displaying telemetry status, Then output is escaped without rewriting the stored record",
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
			"telemetry:",
			"  level: off",
		),
		Steps: []Step{
			writeFile("~/.local/state/agent-marketplace/telemetry/last-delivery-failure", `{"time":"2026-09-01T00:00:00Z","error":"SERVER\u001b[31m\r\n\u202e"}`),
			ok("telemetry", "status").
				out(`SERVER\x1b[31m\r\n\u202e`).
				notOut("\x1b", "\r", "\u202e").
				unchanged("~/.local/state/agent-marketplace/telemetry/last-delivery-failure"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/command_error_controls",
		Desc: "Given a repository source name carrying terminal controls, When status fails and doctor reports configuration errors, Then both print visible escapes without changing the configuration",
		RepoCfg: v4(
			"sources:",
			"  \"\\e]0;TITLE\\a\\e[2J\\u202EEVIL\\N\":",
			"    directory: ./missing",
		),
		Steps: []Step{
			exits(1, "status").
				out(`Error: validate config "`, `/workspace/.agents/config.yaml": v4.source-name:`, `/workspace/.agents/config.yaml:3: sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85: source name "\x1b]0;TITLE\a\x1b[2J\u202eEVIL\u0085" must contain only letters, numbers, dots, underscores, or hyphens`, "hint: In ", "rename the source").
				notOut("\x1b", "\a", "\u202e", "\u0085").
				unchanged("./.agents/config.yaml"),
			ok("doctor").
				out(`error: validate config "`, `/workspace/.agents/config.yaml": v4.source-name:`, `/workspace/.agents/config.yaml:3: sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85: source name "\x1b]0;TITLE\a\x1b[2J\u202eEVIL\u0085" must contain only letters, numbers, dots, underscores, or hyphens`, "hint: In ", "rename the source").
				notOut("\x1b", "\a", "\u202e", "\u0085").
				unchanged("./.agents/config.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/cli_table_collection_controls",
		Desc: "Given local registry and collection metadata containing terminal controls, When listing checking status and installing a collection, Then display is escaped while selection and installation remain intact",
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {directory: \"./terminal-fixture/REF\\u001b[31m\\u202e\"}}",
			"resources: {team: {include: [skill/control]}}",
		),
		Steps: []Step{
			writeFile("./terminal-fixture/REF\x1b[31m\u202e/skills/control/SKILL.md", "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"),
			writeFile("./terminal-fixture/REF\x1b[31m\u202e/collections/bundle/COLLECTION.yaml", "name: bundle\nversion: \"VERSION\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"),
			writeFile("./terminal-fixture/REF\x1b[31m\u202e/collections/display/COLLECTION.yaml", "name: \"DISPLAY\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"),
			ok("list", "--kind", "collection").out(`DISPLAY\x1b[31m\u202e`).notOut("\x1b", "\u202e").unchanged("./.agents/config.yaml"),
			ok("status").out(`REF\x1b[31m\u202e`).notOut("\x1b", "\u202e").unchanged("./.agents/config.yaml"),
			ok("install", "bundle", "--kind", "collection", "--scope", "repo").
				out(`Installed collection bundle@VERSION\x1b[31m\u202e`).
				notOut("\x1b", "\u202e").
				exists("./.factory/skills/control/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/command_marker_body_lifecycle",
		Desc: "Given a Factory command with marker text in its body and a handwritten sibling, When installing removing and reinstalling, Then real frontmatter ownership enables cleanup without adopting the handwritten file",
		Registries: []RegistryDef{{Name: "marker-body", Files: map[string]string{
			"commands/marker-command.md": "---\nname: marker-command\ndescription: Marker command\n---\n```yaml\n_managed_by: agent-marketplace\n```\n",
			"commands/keep.md":           "---\nname: keep\ndescription: Keep\n---\nKeep body\n",
		}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:marker-body}}', ref: main}}",
			"resources: {team: {include: [command/marker-command, command/keep]}}",
		),
		Steps: []Step{
			writeFile("./.factory/commands/manual.md", "---\ndescription: Personal\n---\nExample: _managed_by: agent-marketplace\n"),
			ok("sync").
				unchanged("./.factory/commands/manual.md").
				regular("./.factory/commands/marker-command.md").
				has("./.factory/commands/marker-command.md", "```yaml\n_managed_by: agent-marketplace\n```"),
			ok("sync", "--locked").unchanged("./.factory/commands/marker-command.md", "./.factory/commands/keep.md", "./.factory/commands/manual.md"),
			ok("remove", "marker-command", "--kind", "command", "--scope", "repo").
				absent("./.factory/commands/marker-command.md").
				unchanged("./.factory/commands/keep.md", "./.factory/commands/manual.md"),
			ok("install", "marker-command", "--kind", "command", "--scope", "repo").
				unchanged("./.factory/commands/keep.md", "./.factory/commands/manual.md").
				has("./.factory/commands/marker-command.md", "---\n_managed_by: agent-marketplace\n"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "security/hook_merge_opaque_members",
		Desc:       "Given handwritten Claude settings with opaque root members and a native hook group alongside managed hooks, When sync updates hooks and one managed hook is removed, Then unrelated members survive byte-for-byte while only the owned hook is removed",
		Registries: []RegistryDef{{Name: "opaque-hooks", Files: map[string]string{"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo generated"}]}],"Stop":[{"hooks":[{"type":"command","command":"stale-owned"}]}]}}`}}},
		GlobalCfg: v4(
			"harnesses: [claude]",
			"sources: {}",
		),
		RepoCfg: v4(
			"harnesses: [claude]",
			"sources: {team: {git: '{{REG:opaque-hooks}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			writeFile("./.claude/settings.json", "{\n  // handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],\n  \"hooks\": {\n    \"PostToolUse\": [{\"hooks\":[{\"type\":\"command\",\"command\":\"manual-hook\"}]}]\n  }\n}\n"),
			ok("sync").has("./.claude/settings.json", "echo generated").lacks("./.claude/settings.json", "_managed_by"),
			Step{}.has("./.claude/settings.json", "stale-owned"),
			Step{}.
				has("./.claude/settings.json", "// handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],"),
			Step{}.has("./.claude/settings.json", "manual-hook"),
			ok("remove", "Stop-command", "--kind", "hook", "--scope", "repo").
				has("./.claude/settings.json", "echo generated").
				lacks("./.claude/settings.json", "stale-owned"),
			Step{}.
				has("./.claude/settings.json", "// handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],"),
			Step{}.has("./.claude/settings.json", "manual-hook"),
			ok("sync", "--check").unchanged("./.claude/settings.json"),
			ok("sync", "--locked").unchanged("./.claude/settings.json"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/codex_command_restriction",
		Desc: "Given a restricted command and an unrestricted sibling, When syncing to Codex, Then the unsupported restriction suppresses only that command",
		Registries: []RegistryDef{{Name: "codex-restrictions", Files: map[string]string{
			"commands/restricted.md": "---\nname: restricted\ndescription: Restricted\nallowed-tools: []\n---\nBody\n",
			"commands/plain.md":      "---\nname: plain\ndescription: Plain\n---\nBody\n",
		}}},
		GlobalCfg: v4(
			"harnesses: [codex]",
			"sources: {team: {git: '{{REG:codex-restrictions}}', ref: main}}",
			"resources: {team: {include: [command/restricted, command/plain]}}",
		),
		Steps: []Step{exits(1, "sync").
			out("skill.codex.security-restriction-unsupported").
			absent("~/.agents/skills/restricted").
			has("~/.agents/skills/plain/SKILL.md", "x-agent-marketplace-kind: command")},
	})

	testTable = append(testTable, TestCase{
		Name: "security/nested_bundle_lock_inventory",
		Desc: "Given an empty parent plugin with nested hook and MCP resources, When lock and locked sync run, Then both resources are inventoried before materialization",
		Registries: []RegistryDef{{Name: "nested-inventory", Files: map[string]string{
			".claude-plugin/plugin.json":     `{"name":"container","version":"1.0.0"}`,
			"plugins/audit/.mcp.json":        `{"mcpServers":{"nested-server":{"command":"fixture-server"}}}`,
			"plugins/audit/hooks/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo audit"}]}]}}`,
		}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:nested-inventory}}', ref: main, source_harness: claude}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("lock").has("./.agents/lock.yaml", "kind: hook"),
			ok("sync", "--locked").has("./.agents/lock.yaml", "name: nested-server").has("./.factory/mcp.json", "fixture-server"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/blocked_registry_credentials_redacted",
		Desc: "Given a denied credential-bearing registry, When sync loads effective config, Then error output and the persisted log redact credentials without changing the configured identity",
		GlobalCfg: v4(
			"harnesses: [factory]",
			"allowed_registries: []",
		),
		RepoCfg: v4(
			"sources: {team: {git: 'https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{exits(1, "sync").
			out("blocked by allowed_registries", "example.invalid/repo.git").
			notOut("YOUR_USER_HERE", "YOUR_TOKEN_HERE", "YOUR_QUERY_HERE", "YOUR_FRAGMENT_HERE").
			exists("~/.agent-marketplace/logs/agent-marketplace.log").
			absent("./.agents/lock.yaml", "./.factory").
			unchanged("./.agents/config.yaml").
			has("~/.agent-marketplace/logs/agent-marketplace.log", "blocked by allowed_registries").
			lacks("~/.agent-marketplace/logs/agent-marketplace.log", "YOUR_")},
	})

	testTable = append(testTable, TestCase{
		Name:       "security/blocked_command_link_cleanup",
		Desc:       "Given locked commands with an edited sibling, When one blocked command is removed, Then only its directory link is unlinked",
		Registries: []RegistryDef{{Name: "blocked-command-links", Commands: []Command{{Name: "remove"}, {Name: "keep"}}}},
		GlobalCfg:  v4("harnesses: [devin]"),
		RepoCfg: v4(
			"harnesses: [devin]",
			"sources: {team: {git: '{{REG:blocked-command-links}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("./.devin/skills/remove/SKILL.md", "./.devin/skills/keep/SKILL.md"),
			writeFile("./.devin/skills/keep/SKILL.md", "COMMAND SIBLING EDITED\n"),
			writeGlobalCfg(v4(
				"harnesses: [devin]",
				"allowed_registries: []",
			)),
			ok("remove", "remove", "--kind", "command", "--from", "{{REG:blocked-command-links}}").
				absent("./.devin/skills/remove").
				unchanged("./.devin/skills/keep/SKILL.md", "./.agents/lock.yaml").
				has("./.devin/skills/keep/SKILL.md", "COMMAND SIBLING EDITED"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "security/blocked_subagent_link_cleanup",
		Desc:       "Given locked subagents with an edited sibling, When one blocked subagent is removed, Then only its file link is unlinked",
		Registries: []RegistryDef{{Name: "blocked-subagent-links", Subagents: []Subagent{{Name: "remove"}, {Name: "keep"}}}},
		GlobalCfg:  v4("harnesses: [devin]"),
		RepoCfg: v4(
			"harnesses: [devin]",
			"sources: {team: {git: '{{REG:blocked-subagent-links}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("./.devin/agents/remove.md", "./.devin/agents/keep.md"),
			writeFile("./.devin/agents/keep.md", "SUBAGENT SIBLING EDITED\n"),
			writeGlobalCfg(v4(
				"harnesses: [devin]",
				"allowed_registries: []",
			)),
			ok("remove", "remove", "--kind", "subagent", "--from", "{{REG:blocked-subagent-links}}").
				absent("./.devin/agents/remove.md").
				unchanged("./.devin/agents/keep.md", "./.agents/lock.yaml").
				has("./.devin/agents/keep.md", "SUBAGENT SIBLING EDITED"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "security/blocked_skill_link_cleanup",
		Desc:       "Given locked skill symlinks and an edited installed sibling, When a blocked registry skill is removed, Then only its link is unlinked without rewriting siblings",
		Registries: []RegistryDef{{Name: "blocked-skill-links", Skills: []Skill{{Name: "remove", Description: "Remove original"}, {Name: "keep", Description: "Keep original"}}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:blocked-skill-links}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/remove/SKILL.md", "./.factory/skills/keep/SKILL.md"),
			writeFile("./.factory/skills/keep/SKILL.md", "KEEP EDITED BY USER\n"),
			writeFile("./.factory/skills/manual/SKILL.md", "HANDWRITTEN\n"),
			writeFile("./.gitignore", "handwritten-ignore\n"),
			writeGlobalCfg(v4(
				"harnesses: [factory]",
				"allowed_registries: []",
			)),
			ok("remove", "remove", "--kind", "skill", "--from", "{{REG:blocked-skill-links}}", "--ref", "main").
				absent("./.factory/skills/remove").
				unchanged("./.factory/skills/keep/SKILL.md", "./.factory/skills/manual", "./.agents/lock.yaml", "./.gitignore").
				has("./.factory/skills/keep/SKILL.md", "KEEP EDITED BY USER").
				has("./.factory/skills/manual/SKILL.md", "HANDWRITTEN"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/blocked_owned_mcp_cleanup",
		Desc: "Given an exact locked Devin MCP registry blocked after sync, When one named owned MCP is removed, Then edited and handwritten siblings remain unchanged",
		Registries: []RegistryDef{
			{Name: "blocked-owned-mcp", MCPServers: []MCPServer{
				{Name: "remove", Command: "remove-binary"},
				{Name: "keep", Command: "keep-binary"},
			}},
		},
		GlobalCfg: v4("harnesses: [devin]"),
		RepoCfg: v4(
			"harnesses: [devin]",
			"sources: {team: {git: '{{REG:blocked-owned-mcp}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").has("./.devin/mcp_config.json", "keep-binary"),
			writeFile("./.devin/mcp_config.json", "{\n  // retained comment\n  \"custom\": true,\n  \"mcpServers\": {\n    \"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"},\n    \"remove\": {\"command\": \"remove-binary\"}\n  }\n}\n"),
			writeGlobalCfg(v4(
				"harnesses: [devin]",
				"allowed_registries: []",
			)),
			ok("remove", "remove", "--kind", "mcp", "--from", "{{REG:blocked-owned-mcp}}").
				has("./.devin/mcp_config.json", "\"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"}").
				lacks("./.devin/mcp_config.json", "remove-binary"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/config_include_size",
		Desc: "Given an oversized valid included config, When sync runs, Then it fails without mutating the inputs or materializing output",
		RepoCfg: v4(
			"harnesses: [factory]",
			"include: [large.yaml]",
			"sources: {}",
		),
		Steps: []Step{
			writeFile("./.agents/large.yaml", "sources: {}\n#"+strings.Repeat("x", 4*1024*1024)+"\n"),
			exits(1, "sync").out("config file exceeds maximum size").absent("./.factory").unchanged("./.agents/config.yaml", "./.agents/large.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "security/config_include_small",
		Desc: "Given a small included registry and selector, When sync runs, Then the included skill materializes",
		RepoCfg: v4(
			"harnesses: [factory]",
			"include: [small.yaml]",
		),
		Steps: []Step{
			writeFile("./.agents/registry/skills/bounded-skill/SKILL.md", (Skill{Name: "bounded-skill", Description: "bounded include skill"}).SkillFile()),
			writeFile("./.agents/small.yaml", "sources: {team: {directory: ./registry}}\nresources: {team: {include: [skill/bounded-skill]}}\n"),
			ok("sync").exists("./.factory/skills/bounded-skill/SKILL.md"),
		},
	})
}
