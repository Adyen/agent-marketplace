package e2e_test

func init() {
	// ===== codex =====
	testTable = append(testTable, TestCase{
		Name: "codex/repo_all_kinds_deterministic",
		Desc: "Given repository-scoped resources of every Codex kind, When sync and check run, Then native Codex outputs are complete and deterministic",
		Registries: []RegistryDef{
			{Name: "codex-repo", Skills: []Skill{
				{Name: "repo-skill", Description: "Codex repository skill"},
			}, Commands: []Command{
				{Name: "repo-command", Description: "Codex repository command", Command: "echo repo-command"},
			}, Subagents: []Subagent{
				{Name: "repo-agent", Description: "Codex repository agent"},
			}, MCPServers: []MCPServer{
				{Name: "zeta-server", Command: "zeta-mcp"},
				{Name: "alpha-server", Command: "alpha-mcp"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo repo-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - codex",
			"sources: {}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:codex-repo}}', ref: main, source_harness: claude}}",
			"resources: {team: {include: [skill/repo-skill, command/repo-command, subagent/repo-agent, hook/PreToolUse-command, mcp/zeta-server, mcp/alpha-server]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("./.agents/skills/repo-skill/SKILL.md", "./.agents/skills/repo-command/SKILL.md", "./.codex/agents/repo-agent.toml", "./.codex/hooks.json", "./.codex/config.toml").
				has("./.agents/skills/repo-command/SKILL.md", "x-agent-marketplace-kind: command").
				has("./.codex/agents/repo-agent.toml", "name = 'repo-agent'").
				has("./.codex/hooks.json", "echo repo-hook").
				has("./.codex/config.toml", "[mcp_servers.\"zeta-server\"]\ncommand = 'zeta-mcp'\n[mcp_servers.\"alpha-server\"]\ncommand = 'alpha-mcp'"),
			exits(0, "sync", "--check").
				unchanged("./.agents", "./.agents/skills/repo-skill/SKILL.md", "./.agents/skills/repo-command/SKILL.md", "./.codex", "~/.local/state/agent-marketplace"),
			ok("sync").
				unchanged("./.agents", "./.agents/skills/repo-skill/SKILL.md", "./.agents/skills/repo-command/SKILL.md", "./.codex").
				has("./.codex/config.toml", "[mcp_servers.\"zeta-server\"]\ncommand = 'zeta-mcp'\n[mcp_servers.\"alpha-server\"]\ncommand = 'alpha-mcp'"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "codex/repo_shared_file_collisions_fail_closed",
		Desc: "Given handwritten Codex hook and MCP entries, When sync finds a same-key MCP conflict, Then it preserves that value while the distinct hooks coexist",
		Registries: []RegistryDef{
			{Name: "codex-collisions", MCPServers: []MCPServer{
				{Name: "collision-server", Command: "generated-mcp"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo generated-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - codex",
			"sources: {}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:codex-collisions}}', ref: main, source_harness: claude}}",
			"resources: {team: {include: [hook/PreToolUse-command, mcp/collision-server]}}",
		),
		Steps: []Step{
			writeFile("./.codex/hooks.json", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo handwritten-hook"}]}]}}`),
			writeFile("./.codex/config.toml", "[mcp_servers.\"collision-server\"]\ncommand = 'handwritten-mcp'\n"),
			exits(1, "sync").
				out("ownership.unmanaged-key").
				has("./.codex/hooks.json", "echo generated-hook").
				has("./.codex/config.toml", "command = 'handwritten-mcp'").
				lacks("./.codex/config.toml", "generated-mcp"),
			exits(1, "sync", "--check").
				out("ownership.unmanaged-key").
				has("./.codex/hooks.json", "echo handwritten-hook").
				has("./.codex/config.toml", "command = 'handwritten-mcp'").
				lacks("./.codex/config.toml", "generated-mcp"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "codex/machine_lifecycle_multi_harness",
		Desc: "Given machine-scoped Codex resources selected with Factory, When sync, check, prune, restore, and deinit run, Then both Codex root partitions and Factory follow the same lifecycle without check-mode mutation",
		Registries: []RegistryDef{
			{Name: "codex-machine", Skills: []Skill{
				{Name: "machine-skill", Description: "Codex machine skill"},
			}, Subagents: []Subagent{
				{Name: "machine-agent", Description: "Codex machine agent"},
			}, MCPServers: []MCPServer{
				{Name: "machine-mcp", Command: "machine-mcp-bin"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo machine-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - codex",
			"  - factory",
			"sources: {team: {git: '{{REG:codex-machine}}', ref: main, source_harness: claude}}",
			"resources: {team: {harnesses: [codex, factory], include: [skill/machine-skill, subagent/machine-agent, hook/PreToolUse-command, mcp/machine-mcp]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("~/.agents/skills/machine-skill/SKILL.md", "~/.codex/agents/machine-agent.toml", "~/.codex/hooks.json", "~/.codex/config.toml", "~/.factory/skills/machine-skill/SKILL.md", "~/.factory/droids/machine-agent.md", "~/.factory/hooks.json", "~/.factory/mcp.json").
				has("~/.codex/agents/machine-agent.toml", "name = 'machine-agent'").
				has("~/.codex/hooks.json", "echo machine-hook").
				has("~/.codex/config.toml", "machine-mcp-bin").
				has("~/.factory/droids/machine-agent.md", "name: machine-agent").
				has("~/.factory/hooks.json", "echo machine-hook").
				has("~/.factory/mcp.json", "machine-mcp-bin"),
			exits(0, "sync", "--check").
				unchanged("~/.agents", "~/.agents/skills/machine-skill/SKILL.md", "~/.codex", "~/.factory", "~/.factory/skills/machine-skill/SKILL.md", "~/.local/state/agent-marketplace"),
			writeGlobalCfg(v4(
				"harnesses:",
				"  - codex",
				"  - factory",
				"sources: {}",
			)),
			ok("lock"),
			exits(1, "sync", "--check").
				out("out of sync", "machine-agents", "machine-skill", "machine-codex", "machine-agent.toml", "hooks.json", "config.toml", ".factory").
				unchanged("~/.agents", "~/.agents/skills/machine-skill/SKILL.md", "~/.codex", "~/.factory", "~/.factory/skills/machine-skill/SKILL.md", "~/.local/state/agent-marketplace"),
			ok("sync").
				absent("~/.agents/skills/machine-skill", "~/.codex/agents/machine-agent.toml", "~/.factory/skills/machine-skill", "~/.factory/droids/machine-agent.md").
				lacks("~/.codex/hooks.json", "echo machine-hook").
				lacks("~/.codex/config.toml", "machine-mcp-bin").
				lacks("~/.factory/hooks.json", "echo machine-hook").
				lacks("~/.factory/mcp.json", "machine-mcp-bin"),
			writeGlobalCfg(v4(
				"harnesses:",
				"  - codex",
				"  - factory",
				"sources: {team: {git: '{{REG:codex-machine}}', ref: main, source_harness: claude}}",
				"resources: {team: {harnesses: [codex, factory], include: [skill/machine-skill, subagent/machine-agent, hook/PreToolUse-command, mcp/machine-mcp]}}",
			)),
			ok("sync").
				exists("~/.agents/skills/machine-skill/SKILL.md", "~/.codex/agents/machine-agent.toml", "~/.codex/hooks.json", "~/.codex/config.toml", "~/.factory/skills/machine-skill/SKILL.md", "~/.factory/droids/machine-agent.md", "~/.factory/hooks.json", "~/.factory/mcp.json").
				has("~/.codex/hooks.json", "echo machine-hook").
				has("~/.codex/config.toml", "machine-mcp-bin").
				has("~/.factory/hooks.json", "echo machine-hook").
				has("~/.factory/mcp.json", "machine-mcp-bin"),
			ok("deinit").
				absent("~/.agents/skills/machine-skill", "~/.codex/agents/machine-agent.toml", "~/.factory/skills/machine-skill", "~/.factory/droids/machine-agent.md").
				lacks("~/.codex/hooks.json", "echo machine-hook").
				lacks("~/.codex/config.toml", "machine-mcp-bin").
				lacks("~/.factory/hooks.json", "echo machine-hook").
				lacks("~/.factory/mcp.json", "machine-mcp-bin"),
		},
	})

	// ===== devin =====
	testTable = append(testTable, TestCase{
		Name: "devin/project_mode_lifecycle_and_aggregate_preservation",
		Desc: "Given all five Devin resource kinds and handwritten shared entries, When sync switches between symlink and file modes and then prunes, Then resources follow the configured mode while shared files retain handwritten content and ownership-safe reconciliation",
		Registries: []RegistryDef{
			{Name: "devin-reg",
				Skills:     []Skill{{Name: "devin-skill", Description: "A Devin skill"}},
				Commands:   []Command{{Name: "devin-command", Description: "A Devin command"}},
				Subagents:  []Subagent{{Name: "devin-agent", Description: "A Devin subagent"}},
				MCPServers: []MCPServer{{Name: "devin-mcp", Command: "devin-mcp-bin"}},
				Files:      map[string]string{"hooks/hooks.json": `{"hooks":{"PostToolUse":[{"type":"command","command":"echo generated-hook"}]}}`},
			},
		},
		// harnesses is required on the global layer and is inherited by the
		// project layer; the registry itself lives in the repo config so its
		// Source is repo scope and materialization targets <repo>/.devin.
		GlobalCfg: v4(
			"harnesses:",
			"  - devin",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:devin-reg}}', ref: main}}",
			"resources: {team: {include: [skill/devin-skill, command/devin-command, subagent/devin-agent, hook/PostToolUse-command, mcp/devin-mcp]}}",
		),
		Steps: []Step{
			writeFile("./.devin/hooks.v1.json", "{\n  \"PostToolUse\": [{\"hooks\":[{\"type\":\"command\",\"command\":\"echo handwritten-hook\"}]}]\n}\n"),
			writeFile("./.devin/mcp_config.json", "{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"),
			// Devin ownership is proven from the durable state root, never
			// inferred from a marker inside the user's file.
			ok("sync").
				symlinks("./.devin/skills/devin-skill", "./.devin/skills/devin-command", "./.devin/agents/devin-agent.md").
				regular("./.devin/mcp_config.json").
				has("./.devin/skills/devin-command/SKILL.md", "x-agent-marketplace-kind: command").
				has("./.devin/mcp_config.json", "devin-mcp").
				has("./.devin/hooks.v1.json", "echo generated-hook").
				lacks("./.devin/mcp_config.json", "_managed_by"),
			// The handwritten entry survives reconciliation.
			ok("sync").has("./.devin/mcp_config.json", "user-owned-binary"),
			writeLocalCfg(v4(
				"materialization:",
				"  mode: file",
			)).
				then("sync").
				pass().
				unchanged("./.devin/mcp_config.json", "./.devin/hooks.v1.json").
				regular("./.devin/skills/devin-skill/SKILL.md", "./.devin/skills/devin-command/SKILL.md", "./.devin/agents/devin-agent.md"),
			ok("sync", "--check").unchanged("./.devin"),
			writeLocalCfg(v4(
				"materialization:",
				"  mode: symlink",
			)).
				then("sync").
				pass().
				unchanged("./.devin/mcp_config.json", "./.devin/hooks.v1.json").
				symlinks("./.devin/skills/devin-skill", "./.devin/skills/devin-command", "./.devin/agents/devin-agent.md"),
			writeLocalCfg(v4(
				"materialization:",
				"  mode: file",
			)).
				then("sync").
				pass(),
			writeRepoCfg(v4("sources: {}")).
				then("sync").
				pass().
				absent("./.devin/skills/devin-skill", "./.devin/skills/devin-command", "./.devin/agents/devin-agent.md").
				has("./.devin/mcp_config.json", "user-owned-binary").
				has("./.devin/hooks.v1.json", "echo handwritten-hook").
				lacks("./.devin/mcp_config.json", "devin-mcp").
				lacks("./.devin/hooks.v1.json", "echo generated-hook"),
		},
	})

	testTable = append(testTable, TestCase{
		// Bead agent-marketplace-pu6b. This is the case that used to take the
		// whole run down: three skills selected by this repo's own committed
		// .agents/config.yaml carry `allowed-tools: Bash(openspec:*)`, which
		// Devin cannot represent (five bare tool names, no per-command scoping).
		// The compiler emitted skill.devin.tool-unknown at ERROR severity and
		// produced no artifact, so sync exited non-zero and installed nothing.
		//
		// It now omits the allowed-tools key, warns, and installs. The exit code
		// is the assertion that matters most: a non-zero sync also gates off
		// --prune-stale, so the old behaviour broke unrelated cleanup too.
		Name: "devin/openspec_scoped_bash_allowed_tools_still_installs",
		Desc: "Given a Devin harness with an openspec-style skill whose allowed-tools is the scoped specifier Bash(openspec:*) and a subagent whose tools name a tool with no Devin equivalent, When sync runs, Then it exits ZERO, both resources are installed, the compiled subagent profile carries no allowed-tools key, and no diagnostic quotes the offending specifier",
		Registries: []RegistryDef{
			{Name: "devin-scoped-reg",
				Subagents: []Subagent{{Name: "scoped-agent", Description: "A Devin subagent", Model: "sonnet", Tools: []string{"Read", "WebSearch"}}},
				Files: map[string]string{
					// Written raw rather than through the Skill builder because
					// the scoped specifier IS the fixture.
					"skills/openspec-apply-change/SKILL.md": "---\nname: openspec-apply-change\ndescription: Apply an openspec change\nallowed-tools: Bash(openspec:*)\n---\n\n# openspec-apply-change\n\nRun openspec.\n",
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - devin",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:devin-scoped-reg}}', ref: main}}",
			"resources: {team: {include: [skill/openspec-apply-change, subagent/scoped-agent]}}",
		),
		Steps: []Step{
			// MustPass is the acceptance criterion: exit ZERO, not a
			// suppressed resource and a non-zero run.
			// The compiled profile inherits Devin's default tool set
			// rather than carrying a restriction Devin cannot express.
			// A silent widening would be worse than the original error, so
			// the loss must be REPORTED and ATTRIBUTED: both codes, and the
			// name of each resource that lost its restriction. The message
			// itself is generic by design, so without the resource name the
			// warning is unactionable (adversarial review of the bead).
			// The reported loss never echoes the specifier: allowed-tools
			// entries carry command strings.
			ok("sync").
				out("warning: skill.devin.tool-unknown", "openspec-apply-change", "warning: subagent.devin.tool-unknown", "scoped-agent").
				notOut("Bash(openspec", "WebSearch").
				symlinks("./.devin/skills/openspec-apply-change", "./.devin/agents/scoped-agent.md").
				has("./.devin/agents/scoped-agent.md", "name: scoped-agent").
				lacks("./.devin/agents/scoped-agent.md", "allowed-tools"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "devin/machine_file_mode_routes_to_user_root",
		Desc: "Given file mode and a Devin registry in the global config, When sync runs, Then skills, commands, subagents and the MCP aggregate are regular outputs under ~/.config/devin and nothing is written into the repo's .devin",
		Registries: []RegistryDef{
			{Name: "devin-machine-reg",
				Skills:     []Skill{{Name: "machine-skill", Description: "A machine-scope Devin skill"}},
				Commands:   []Command{{Name: "machine-command", Description: "A machine-scope Devin command"}},
				Subagents:  []Subagent{{Name: "machine-agent", Description: "A machine-scope Devin subagent"}},
				MCPServers: []MCPServer{{Name: "machine-mcp", Command: "machine-mcp-bin"}},
			},
		},
		// A registry declared in ~/.agents/config.yaml carries a machine-scope
		// Source, which routes materialization to the target's machine root.
		GlobalCfg: v4(
			"harnesses:",
			"  - devin",
			"materialization:",
			"  mode: file",
			"sources: {team: {git: '{{REG:devin-machine-reg}}', ref: main}}",
			"resources: {team: {include: [skill/machine-skill, command/machine-command, subagent/machine-agent, mcp/machine-mcp]}}",
		),
		Steps: []Step{
			// Machine scope never leaks into the repo root.
			ok("sync").
				absent("./.devin").
				regular("~/.config/devin/skills/machine-skill/SKILL.md", "~/.config/devin/skills/machine-command/SKILL.md", "~/.config/devin/agents/machine-agent.md", "~/.config/devin/mcp_config.json").
				has("~/.config/devin/skills/machine-command/SKILL.md", "x-agent-marketplace-kind: command").
				has("~/.config/devin/mcp_config.json", "machine-mcp"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "devin/doctor_reports_target_and_repair_is_preview_only_off_tty",
		Desc: "Given a repo-scope Devin harness synced over a handwritten .devin/mcp_config.json, When doctor runs, Then it reports Devin's canonical roots and bidirectional capabilities; When doctor --repair runs off a terminal, Then it previews only, exits 0, and leaves the reconciled repo aggregate (generated entry plus handwritten entry) as found; And doctor --yes without --repair is rejected",
		Registries: []RegistryDef{
			{Name: "devin-doctor-reg",
				MCPServers: []MCPServer{{Name: "devin-doctor-mcp", Command: "devin-doctor-bin"}},
			},
		},
		// harnesses is required on the global layer (and WriteGlobalConfig would
		// otherwise inject `factory`), but the registry itself must live in the
		// repo config: a machine-scope Source routes materialization to
		// ~/.config/devin, which would leave the handwritten
		// /workspace/.devin/mcp_config.json below untouched and every assertion
		// on it vacuous.
		GlobalCfg: v4(
			"harnesses:",
			"  - devin",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:devin-doctor-reg}}', ref: main}}",
			"resources: {team: {include: [mcp/devin-doctor-mcp]}}",
		),
		Steps: []Step{
			writeFile("./.devin/mcp_config.json", "{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"),
			// Proof that this is the file under reconciliation: the run read
			// it, merged the generated entry in, and rewrote it in place.
			ok("sync").regular("./.devin/mcp_config.json").has("./.devin/mcp_config.json", "devin-doctor-mcp"),
			// No Devin binary is resolvable from the sandbox PATH (the
			// framework shadows it out, so this holds even on a host
			// with Devin installed), and that never blocks anything:
			// "Sync does not require a Devin binary."
			ok("doctor").
				out("devin repo=", "capabilities=compile,ingest,convert,tui", ".config/devin", "devin harness: selected (global)", "devin cli: not installed (sync does not require it)"),
			// A freshly synced root is clean: every generated entry is
			// proven owned, so there is nothing to adopt, and nothing is
			// undesired, so there is nothing to nominate.
			// Preview-only off a TTY: the reconciled repo aggregate still
			// holds both the handwritten entry and the generated one.
			ok("doctor", "--repair").
				out("repair: devin shared-aggregate ownership").
				notOut("adopt ", "orphan candidate ").
				has("./.devin/mcp_config.json", "user-owned-binary").
				lacks("./.devin/mcp_config.json", "_managed_by"),
			// FileContains is keyed by path, so the generated entry needs its own
			// step; a second preview-only repair also proves repair is idempotent.
			ok("doctor", "--repair").has("./.devin/mcp_config.json", "devin-doctor-mcp"),
			exits(1, "doctor", "--yes").out("--yes only applies to --repair"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "devin/mcp_key_conflict_is_previewed_as_replaceable",
		Desc: "Given a hand-written .devin/mcp_config.json entry whose key equals a selected MCP server's key but whose value differs, When sync runs, Then it reports the collision, preserves the hand-written entry and installs nothing; When doctor --repair runs off a terminal, Then it previews the entry as a replaceable conflict with both value digests and neither value, exits 0, and mutates nothing; And doctor --repair --yes still mutates nothing because replacing destroys user-written content",
		Registries: []RegistryDef{
			{Name: "devin-conflict-reg",
				MCPServers: []MCPServer{{Name: "genai-atlassian", Command: "generated-atlassian-bin"}},
			},
		},
		// The registry must be repo-scope so materialization targets
		// /workspace/.devin, which is where the hand-written aggregate below
		// lives; harnesses is still required on the global layer.
		GlobalCfg: v4(
			"harnesses:",
			"  - devin",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:devin-conflict-reg}}', ref: main}}",
			"resources: {team: {include: [mcp/genai-atlassian]}}",
		),
		Steps: []Step{
			// The tester's exact starting state: same KEY, different VALUE, and
			// no ownership state anywhere because nothing ever wrote one.
			writeFile("./.devin/mcp_config.json", "{\n  \"mcpServers\": {\n    \"genai-atlassian\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"),
			// Collide: the hand-written entry is preserved and only the
			// generated entry fails. This is the loop the user was stuck in.
			// The code is the stable handle; its catalogued hint in
			// diag/codes.md now names both repair paths (adopt when it
			// matches exactly, replace when the value differs).
			ok("sync").
				out("ownership.unmanaged-key", "already exists and is not proven owned").
				has("./.devin/mcp_config.json", "user-owned-binary").
				lacks("./.devin/mcp_config.json", "generated-atlassian-bin"),
			// Repair PREVIEWS the third class instead of answering "nothing to
			// repair": the offer, the two digests, and the reason it needs a
			// terminal.
			// Neither value is ever printed, and this is not adoption.
			ok("doctor", "--repair").
				out("repair: devin shared-aggregate ownership", "replace mcp-server \"genai-atlassian\" (resource \"genai-atlassian\"): the existing entry has the same key but a different value", "existing value digest sha256:", "generated value digest sha256:", "doctor.repair.conflict-replaceable", "replacing DELETES the existing value; it needs an interactive per-entry confirmation and is never done by --yes", "preview only: not a terminal").
				notOut("user-owned-binary", "generated-atlassian-bin", "nothing to repair", "adopt mcp-server").
				has("./.devin/mcp_config.json", "user-owned-binary"),
			// --yes covers adoption only. It reports the conflict and leaves the
			// user's value exactly where it was.
			ok("doctor", "--repair", "--yes").
				out("left unchanged: replacing it deletes user-written content and requires an interactive per-entry confirmation").
				has("./.devin/mcp_config.json", "user-owned-binary").
				lacks("./.devin/mcp_config.json", "generated-atlassian-bin"),
		},
	})
}
