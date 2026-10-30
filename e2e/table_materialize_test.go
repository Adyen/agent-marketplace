package e2e_test

import (
	"encoding/json"
	"strings"
)

func init() {
	// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
	testTable = append(testTable, TestCase{
		Name: "hooks/additive_cross_registry",
		Registries: []RegistryDef{
			{Name: "first", Files: map[string]string{".factory/hooks.json": `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo first"}]}]}`}},
			{Name: "second", Files: map[string]string{".factory/hooks.json": `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo second"}]}]}`}},
		},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  first: {git: '{{REG:first}}', ref: main, source_harness: factory}",
			"  second: {git: '{{REG:second}}', ref: main, source_harness: factory}",
		),
		Steps: []Step{
			ok("install", "--scope", "repo", "--all", "--kind", "hook").
				out("PreToolUse-command").
				notOut("multiple registries").
				regular("./.factory/hooks.json").
				has("./.factory/hooks.json", "echo first"),
			ok("list", "--kind", "hook").out("PreToolUse-command").has("./.factory/hooks.json", "echo second"),
			ok("sync", "--check").unchanged("./.factory/hooks.json", "./.agents/lock.yaml"),
			ok("sync").unchanged("./.factory/hooks.json"),
			ok("remove", "PreToolUse-command", "--kind", "hook", "--from", "{{REG:first}}").
				has("./.factory/hooks.json", "echo second").
				lacks("./.factory/hooks.json", "echo first"),
			ok("sync", "--check").unchanged("./.factory/hooks.json", "./.agents/lock.yaml"),
		},
	})

	// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
	testTable = append(testTable, TestCase{
		Name:       "hooks/additive_session_start",
		Registries: []RegistryDef{{Name: "session-start", Files: map[string]string{".factory/hooks.json": `{"SessionStart":[{"hooks":[{"type":"command","command":"echo first"}]},{"hooks":[{"type":"command","command":"echo second"}]}]}`}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:session-start}}', ref: main, source_harness: factory}}",
			"resources: {team: {include: [hook/SessionStart-command, hook/SessionStart-command-2]}}",
		),
		Steps: []Step{
			ok("sync").has("./.factory/hooks.json", "echo first"),
			ok("sync", "--check").unchanged("./.factory/hooks.json", "./.agents/lock.yaml").has("./.factory/hooks.json", "echo second"),
			ok("sync").unchanged("./.factory/hooks.json"),
		},
	})

	// ===== materialize =====
	testTable = append(testTable, TestCase{
		Name: "materialize/multi_harness_output",
		Desc: "Given factory and windsurf harnesses configured, When sync is run, Then skills appear in both harness output directories",
		Registries: []RegistryDef{
			{Name: "multi-harness", Skills: []Skill{
				{Name: "universal-skill", Description: "Appears in all harnesses", Tags: []string{"core"}},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - windsurf",
			"sources: {team: {git: '{{REG:multi-harness}}', ref: main}}",
			"resources: {team: {include: [skill/universal-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/universal-skill/SKILL.md", "~/.codeium/windsurf/skills/universal-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/preserves_structured_skill_metadata",
		Desc: "Given a skill with mapping-valued metadata, When sync materializes it for every harness, Then each SKILL.md retains valid nested YAML metadata",
		Registries: []RegistryDef{
			{Name: "metadata-registry", Files: map[string]string{
				"skills/owned-skill/SKILL.md": `---
name: owned-skill
description: A skill with ownership metadata
metadata:
  author: example-maintainer
  tags:
    - platform
---

# Owned skill
`,
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - claude",
			"  - factory",
			"  - cursor",
			"  - windsurf",
			"  - gemini",
			"sources: {team: {git: '{{REG:metadata-registry}}', ref: main}}",
			"resources: {team: {include: [skill/owned-skill]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("~/.claude/skills/owned-skill/SKILL.md", "~/.factory/skills/owned-skill/SKILL.md", "~/.cursor/skills/owned-skill/SKILL.md", "~/.codeium/windsurf/skills/owned-skill/SKILL.md", "~/.gemini/skills/owned-skill/SKILL.md").
				has("~/.claude/skills/owned-skill/SKILL.md", "metadata:\n  author: example-maintainer").
				has("~/.factory/skills/owned-skill/SKILL.md", "metadata:\n  author: example-maintainer").
				has("~/.cursor/skills/owned-skill/SKILL.md", "metadata:\n  author: example-maintainer").
				has("~/.codeium/windsurf/skills/owned-skill/SKILL.md", "metadata:\n  author: example-maintainer").
				has("~/.gemini/skills/owned-skill/SKILL.md", "metadata:\n  author: example-maintainer").
				lacks("~/.claude/skills/owned-skill/SKILL.md", "map[").
				lacks("~/.factory/skills/owned-skill/SKILL.md", "map[").
				lacks("~/.cursor/skills/owned-skill/SKILL.md", "map[").
				lacks("~/.codeium/windsurf/skills/owned-skill/SKILL.md", "map[").
				lacks("~/.gemini/skills/owned-skill/SKILL.md", "map["),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/harness_removal_prunes_artifacts",
		Desc: "Given a skill synced to factory and windsurf, When windsurf is removed from harnesses and re-synced, Then the windsurf artifact is pruned while factory remains",
		Registries: []RegistryDef{
			{Name: "harness-removal", Skills: []Skill{
				{Name: "prunable-skill", Description: "Will be pruned from windsurf"},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - windsurf",
			"sources: {team: {git: '{{REG:harness-removal}}', ref: main}}",
			"resources: {team: {include: [skill/prunable-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/prunable-skill/SKILL.md", "~/.codeium/windsurf/skills/prunable-skill/SKILL.md"),
			writeGlobalCfg(v4(
				"harnesses:",
				"  - factory",
				"sources: {team: {git: '{{REG:harness-removal}}', ref: main}}",
				"resources: {team: {include: [skill/prunable-skill]}}",
			)),
			ok("sync").exists("~/.factory/skills/prunable-skill/SKILL.md").absent("~/.codeium/windsurf/skills/prunable-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/skills_are_symlinks",
		Desc: "Given a skill synced from a registry, When the materialized path is inspected, Then it is a symlink pointing to the cache directory",
		Registries: []RegistryDef{
			{Name: "symlink-test", Skills: []Skill{
				{Name: "linked-skill", Description: "Should be a symlink"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:symlink-test}}', ref: main}}",
			"resources: {team: {include: [skill/linked-skill]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("~/.factory/skills/linked-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/file_mode_portable_project_output",
		Desc: "Given project file-mode configuration, When sync materializes a skill, Then it writes a regular portable tree and a commit-ready project manifest",
		Registries: []RegistryDef{
			{Name: "file-mode-test", Skills: []Skill{{Name: "portable-skill", Description: "Portable skill"}}},
		},
		RepoCfg: v4(
			"materialization: {mode: file}",
			"sources: {team: {git: '{{REG:file-mode-test}}', ref: main}}",
			"resources: {team: {include: [skill/portable-skill]}}",
		),
		Steps: []Step{
			ok("sync").
				exists("./.agents/materialization-manifest.yaml").
				regular("./.factory/skills/portable-skill/SKILL.md").
				has("./.agents/materialization-manifest.yaml", "skills/portable-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/target_mode_override_supports_devin_with_file_mode",
		Desc: "Given file mode with a Devin symlink override, When the same skill targets Factory and Devin, Then Factory writes files and Devin writes a directory symlink",
		Registries: []RegistryDef{
			{Name: "mixed-mode-test", Skills: []Skill{{Name: "mixed-mode-skill", Description: "Mixed mode skill"}}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - devin",
		),
		RepoCfg: v4(
			"materialization:",
			"  mode: file",
			"  targets:",
			"    devin: symlink",
			"sources: {team: {git: '{{REG:mixed-mode-test}}', ref: main}}",
			"resources: {team: {include: [skill/mixed-mode-skill]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.devin/skills/mixed-mode-skill").regular("./.factory/skills/mixed-mode-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/manual_skills_survive_sync",
		Desc: "Given a managed skill and a manually created skill, When sync is re-run, Then the manual skill survives and the managed skill stays",
		Registries: []RegistryDef{
			{Name: "manual-survive", Skills: []Skill{
				{Name: "managed-skill", Description: "Managed by agent-marketplace"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:manual-survive}}', ref: main}}",
			"resources: {team: {include: [skill/managed-skill]}}",
		),
		Steps: []Step{
			ok("sync"),
			writeFile("~/.factory/skills/manual-skill/SKILL.md", `---
name: manual-skill
description: Manually created
---

# manual-skill
`),
			ok("sync").exists("~/.factory/skills/managed-skill/SKILL.md", "~/.factory/skills/manual-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/manual_droids_survive_sync",
		Desc: "Given pre-existing user-authored droids in .factory/droids, When a managed droid is synced, Then the user droids survive and the managed droid is a symlink to the cache",
		Registries: []RegistryDef{
			{Name: "droid-survive", Subagents: []Subagent{
				{Name: "reviewer", Description: "Managed by agent-marketplace", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:droid-survive}}', ref: main}}",
			"resources: {team: {include: [subagent/reviewer]}}",
		),
		Steps: []Step{
			// Seed three user-authored droids (regular files, no ownership marker).
			writeFile("~/.factory/droids/worker.md", `---
name: worker
description: my hand-written worker droid
---

# worker (user-owned)
`),
			writeFile("~/.factory/droids/planner.md", `---
name: planner
description: my hand-written planner droid
---

# planner (user-owned)
`),
			writeFile("~/.factory/droids/explorer.md", `---
name: explorer
description: my hand-written explorer droid
---

# explorer (user-owned)
`),
			// User droids must remain unmanaged (no ownership marker injected).
			ok("sync").
				exists("~/.factory/droids/worker.md", "~/.factory/droids/planner.md", "~/.factory/droids/explorer.md", "~/.factory/droids/reviewer.md").
				symlinks("~/.factory/droids/reviewer.md").
				has("~/.factory/droids/worker.md", "# worker (user-owned)").
				has("~/.factory/droids/planner.md", "# planner (user-owned)").
				has("~/.factory/droids/explorer.md", "# explorer (user-owned)").
				lacks("~/.factory/droids/worker.md", "_managed_by"),
			// Re-sync must be idempotent: user droids still survive.
			ok("sync").exists("~/.factory/droids/worker.md", "~/.factory/droids/planner.md", "~/.factory/droids/explorer.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/manual_droid_survives_managed_droid_removal",
		Desc: "Given a synced managed droid alongside a user droid, When the managed droid is removed from config and sync re-runs, Then the managed droid is pruned but the user droid survives",
		Registries: []RegistryDef{
			{Name: "droid-remove-survive", Subagents: []Subagent{
				{Name: "reviewer", Description: "Managed by agent-marketplace", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:droid-remove-survive}}', ref: main}}",
			"resources: {team: {include: [subagent/reviewer]}}",
		),
		Steps: []Step{
			writeFile("~/.factory/droids/worker.md", `---
name: worker
description: my hand-written worker droid
---

# worker (user-owned)
`),
			ok("sync").exists("~/.factory/droids/worker.md").symlinks("~/.factory/droids/reviewer.md"),
			// Drop the managed droid from config, then re-sync.
			writeGlobalCfg(v4("sources: {team: {git: '{{REG:droid-remove-survive}}', ref: main}}")),
			ok("sync").
				exists("~/.factory/droids/worker.md").
				absent("~/.factory/droids/reviewer.md").
				has("~/.factory/droids/worker.md", "# worker (user-owned)"),
		},
	})

	testTable = append(testTable, TestCase{
		// spec: materialization/cli-check-refuses-stale-linked-content-without-repairing-it
		Name:      "materialize/sync_check_detects_linked_content_drift",
		Desc:      "Given linked resources from a live directory source, When their command, skill and support content changes, Then check exits 1 without repairing caches; sync repairs them and check exits 0",
		GlobalCfg: v4("harnesses: [claude, opencode]"),
		RepoCfg: v4(
			"sources: {local: {directory: ./resources, source_harness: claude}}",
			"resources: {local: {include: [command/check, skill/review]}}",
		),
		Steps: []Step{
			writeFile("./resources/commands/check.md", (Command{Name: "check"}).CommandFile()+"\noriginal command\n"),
			writeFile("./resources/skills/review/SKILL.md", (Skill{Name: "review", Description: "Original skill"}).SkillFile()),
			writeFile("./resources/skills/review/reference.txt", "original support"),
			ok("sync").symlinks("./.claude/commands/check.md", "./.claude/skills/review", "./.opencode/commands/check.md", "./.opencode/skills/review"),
			ok("sync", "--check").unchanged("./.agents", "./.claude", "./.opencode", "~/.cache/agent-marketplace"),
			writeFile("./resources/commands/check.md", (Command{Name: "check"}).CommandFile()+"\nupdated command\n"),
			writeFile("./resources/skills/review/SKILL.md", (Skill{Name: "review", Description: "Updated skill"}).SkillFile()),
			writeFile("./resources/skills/review/reference.txt", "updated support"),
			exits(1, "sync", "--check").
				absent("./.agents/lock.yaml").
				unchanged("./.agents", "./.claude", "./.opencode", "~/.cache/agent-marketplace").
				has("./.claude/commands/check.md", "original command").
				has("./.opencode/commands/check.md", "original command").
				has("./.claude/skills/review/SKILL.md", "Original skill").
				has("./.opencode/skills/review/SKILL.md", "Original skill").
				has("./.claude/skills/review/reference.txt", "original support").
				has("./.opencode/skills/review/reference.txt", "original support"),
			ok("sync").
				has("./.claude/commands/check.md", "updated command").
				has("./.opencode/commands/check.md", "updated command").
				has("./.claude/skills/review/SKILL.md", "Updated skill").
				has("./.opencode/skills/review/SKILL.md", "Updated skill").
				has("./.claude/skills/review/reference.txt", "updated support").
				has("./.opencode/skills/review/reference.txt", "updated support"),
			ok("sync", "--check").unchanged("./.agents", "./.claude", "./.opencode", "~/.cache/agent-marketplace"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/sync_check_detects_drift",
		Desc: "Given a config with one skill, When sync --check is run before any sync, Then it exits 1; after sync exits 0; after adding a skill to config exits 1 again",
		Registries: []RegistryDef{
			{Name: "drift-check", Skills: []Skill{
				{Name: "drift-skill", Description: "Tests drift detection"},
			}},
			{Name: "drift-check-2", Skills: []Skill{
				{Name: "new-skill", Description: "Causes drift"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:drift-check}}', ref: main}}",
			"resources: {team: {include: [skill/drift-skill]}}",
		),
		Steps: []Step{
			exits(1, "sync", "--check"),
			ok("sync"),
			exits(0, "sync", "--check"),
			writeGlobalCfg(v4(
				"sources:",
				"  first: {git: '{{REG:drift-check}}', ref: main}",
				"  second: {git: '{{REG:drift-check-2}}', ref: main}",
				"resources:",
				"  first: {include: [skill/drift-skill]}",
				"  second: {include: [skill/new-skill]}",
			)),
			exits(1, "sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/tag_based_selection",
		Desc: "Given a registry with k8s-tagged and python-tagged skills and tags: [k8s] in config, When sync is run, Then only k8s-tagged skills are materialized",
		Registries: []RegistryDef{
			{Name: "tag-selection", Skills: []Skill{
				{Name: "k8s-deploy", Description: "Kubernetes deployment", Tags: []string{"k8s", "infra"}},
				{Name: "k8s-debug", Description: "Kubernetes debugging", Tags: []string{"k8s", "debug"}},
				{Name: "python-lint", Description: "Python linting", Tags: []string{"python", "lint"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:tag-selection}}', ref: main}}",
			"resources: {team: {tags: [k8s]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/k8s-deploy/SKILL.md", "~/.factory/skills/k8s-debug/SKILL.md").absent("~/.factory/skills/python-lint"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/empty_sync",
		Desc: "Given no registries configured, When sync is run, Then it exits cleanly with 'Installed 0'",
		Steps: []Step{
			ok("sync").out("Installed 0"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "materialize/commands_materialize_as_files",
		Desc: "Given a registry with a command resource, When sync is run, Then the command is compiled as a .md file (not symlinked)",
		Registries: []RegistryDef{
			{Name: "commands-test",
				Commands: []Command{{Name: "greet", Description: "Greeting command", Command: "echo hello"}}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:commands-test}}', ref: main}}",
			"resources: {team: {include: [command/greet]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/greet.md"),
		},
	})

	for _, version := range []string{"agent-marketplace/v4"} {
		for _, prerequisite := range []struct {
			kind, name, sourcePath, source, output, code string
		}{
			{"mcp", "child", ".mcp.json", `{"mcpServers":{"child":{"command":"echo","args":["inert-child"],"oauthClientId":"fixture-client"}}}`, "/workspace/.codex/config.toml", "mcp.codex.security-unsupported"},
			{"hook", "PreToolUse-prompt", "hooks/hooks.json", `{"hooks":{"PreToolUse":[{"type":"prompt","prompt":"inert advisory prompt","matcher":"Bash"}]}}`, "/workspace/.codex/hooks.json", "hook.prompt.unsupported"},
		} {
			config := func(selected bool) string {
				include := "skill/parent"
				if selected {
					include += ", " + prerequisite.kind + "/" + prerequisite.name
				}
				return "apiVersion: " + version + "\nharnesses: [codex]\nsources:\n  tools: {git: '{{REG:advisory}}', ref: main}\nresources:\n  tools: {include: [" + include + "]}\n"
			}
			warning := `warning: skill "parent" requires ` + prerequisite.kind + ` "` + prerequisite.name + `", but it is not selected`
			unchanged := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/skills/parent"}
			testTable = append(testTable, TestCase{
				Name:       "requires/advisory_unsupported_" + prerequisite.kind + "/" + strings.TrimPrefix(version, "agent-marketplace/"),
				Desc:       "Given an unselected prerequisite that Codex cannot represent, When ordinary, locked and check synchronization run, Then the parent installs without compilation errors or prerequisite output; explicitly selecting the prerequisite exercises the refusal",
				Registries: []RegistryDef{{Name: "advisory", Skills: []Skill{{Name: "parent", Requires: []string{prerequisite.kind + ":" + prerequisite.name}}}, Files: map[string]string{prerequisite.sourcePath: prerequisite.source}}},
				GlobalCfg:  "apiVersion: " + version + "\nharnesses: [codex]\n",
				RepoCfg:    config(false),
				Steps: []Step{
					ok("sync").
						out(warning).
						notOut(prerequisite.code).
						absent(prerequisite.output).
						has("./.agents/skills/parent/SKILL.md", "# parent").
						lacks("./.agents/config.yaml", prerequisite.name),
					ok("sync").out(warning).notOut(prerequisite.code).absent(prerequisite.output).unchanged(unchanged...),
					ok("sync", "--locked").out(warning).notOut(prerequisite.code).absent(prerequisite.output).unchanged(unchanged...),
					ok("sync", "--check").out(warning).notOut(prerequisite.code).absent(prerequisite.output).unchanged(unchanged...),
					exits(1, "sync", "--strict", "--check").out(warning).notOut(prerequisite.code).absent(prerequisite.output).unchanged(unchanged...),
					writeRepoCfg(config(true)).
						then("sync").
						exit(1).
						out(prerequisite.code).
						absent(prerequisite.output).
						has("./.agents/skills/parent/SKILL.md", "# parent"),
					exits(1, "sync", "--locked").
						out(prerequisite.code).
						absent(prerequisite.output).
						unchanged(unchanged...).
						has("./.agents/skills/parent/SKILL.md", "# parent"),
					exits(1, "sync", "--check").
						out(prerequisite.code).
						absent(prerequisite.output).
						unchanged(unchanged...).
						has("./.agents/skills/parent/SKILL.md", "# parent"),
					exits(1, "sync", "--strict", "--check").
						out(prerequisite.code).
						absent(prerequisite.output).
						unchanged(unchanged...).
						has("./.agents/skills/parent/SKILL.md", "# parent"),
				},
			})
		}
	}
}

// bundleRootHandlers is the handler list for one hook group running command.
func bundleRootHandlers(command string) []any {
	return []any{map[string]any{"type": "command", "command": command}}
}

// bundleRootHooks returns a Claude-layout hooks/hooks.json with a PreToolUse hook and, when stop is set, a Stop hook.
// Stop has no matcher, so its group omits one.
func bundleRootHooks(pre, stop string) string {
	hooks := map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": bundleRootHandlers(pre)}}}
	if stop != "" {
		hooks["Stop"] = []any{map[string]any{"hooks": bundleRootHandlers(stop)}}
	}
	data, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// bundleRootFiles returns a bundle with hooks/hooks.json and an executable-looking support script at each path.
func bundleRootFiles(hooks string, scripts ...string) map[string]string {
	files := map[string]string{"hooks/hooks.json": hooks}
	for _, rel := range scripts {
		files[rel] = "#!/bin/sh\necho bundle support\n"
	}
	return files
}

// bundleRootNative returns the native hook file for harness in scope and the scope root that holds the harness directory.
func bundleRootNative(harness, scope string) (native, root string) {
	root, file := "/workspace", "hooks.json"
	if scope == "user" {
		root = "/root"
	}
	if harness == "claude" {
		file = "settings.json"
	}
	return root + "/." + harness + "/" + file, root
}

// bundleRootCase builds a case whose bundle comes from a Git registry or, when git is false, from a directory source written by the case's first steps.
func bundleRootCase(name, harness, scope string, git bool, files map[string]string, steps ...Step) TestCase {
	tc := TestCase{Name: "hooks/bundle_root/" + name}
	cfg := "apiVersion: agent-marketplace/v4\nharnesses: [" + harness + "]\n"
	var writes []Step
	if git {
		tc.Registries = []RegistryDef{{Name: "bundle", Files: files}}
		cfg += "sources: {team: {git: '{{REG:bundle}}', ref: main}}\n"
	} else {
		writeRoot := "/workspace/resources"
		if scope == "user" {
			writeRoot = "/root/.agents/resources"
		}
		cfg += "sources: {team: {directory: ./resources}}\n"
		for rel, body := range files {
			writes = append(writes, writeFile(writeRoot+"/"+rel, body))
		}
	}
	cfg += "resources: {team: {all: true}}\n"
	if scope == "user" {
		tc.GlobalCfg = cfg
	} else {
		tc.RepoCfg = cfg
		tc.GlobalCfg = "apiVersion: agent-marketplace/v4\nharnesses: [" + harness + "]\n"
	}
	tc.Steps = append(writes, steps...)
	return tc
}

func bundleRootSourceName(git bool) string {
	if git {
		return "git"
	}
	return "directory"
}

// spec: hook-resources/bundle-root-references-resolve-on-every-source-path
// spec: hook-resources/support-files-keep-their-bundle-relative-paths
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh"`, ""), "hooks/scripts/check.sh", "hooks/scripts/nested/deep.sh")
	for _, git := range []bool{true, false} {
		for _, harness := range []string{"claude", "factory"} {
			for _, scope := range []string{"repo", "user"} {
				native, root := bundleRootNative(harness, scope)
				support := root + "/." + harness + "/hook-support"
				testTable = append(testTable, bundleRootCase(
					bundleRootSourceName(git)+"/"+harness+"/"+scope,
					harness, scope, git, files,
					ok("sync").notOut("hook.support-reference").has(native, "/PreToolUse-command/hooks/scripts/check.sh").lacks(native, "CLAUDE_PLUGIN_ROOT").
						globs(support+"/*/PreToolUse-command/hooks/scripts/check.sh", support+"/*/PreToolUse-command/hooks/scripts/nested/deep.sh").
						noGlobs(support+"/*/PreToolUse-command/scripts"),
					ok("sync", "--check").unchanged(native),
					ok("sync").unchanged(native, support),
				))
			}
		}
	}
}

// spec: hook-resources/a-reference-to-a-missing-support-file-refuses-the-hook
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/absent.sh"`, "echo sibling-ok"), "hooks/scripts/check.sh")
	native, root := bundleRootNative("claude", "repo")
	for _, git := range []bool{true, false} {
		testTable = append(testTable, bundleRootCase(
			"missing-reference/"+bundleRootSourceName(git),
			"claude", "repo", git, files,
			exits(1, "sync").out("hook.support-reference-missing").notOut("absent.sh").has(native, "echo sibling-ok").lacks(native, "absent.sh").
				noGlobs(root+"/.claude/hook-support/*/PreToolUse-command"),
		))
	}
}

// spec: hook-resources/bare-and-dynamic-plugin-root-references-are-not-checked
func init() {
	files := bundleRootFiles(bundleRootHooks(`cd "${CLAUDE_PLUGIN_ROOT}" && ./hooks/scripts/check.sh`, `"${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT"`), "hooks/scripts/check.sh")
	native, _ := bundleRootNative("claude", "repo")
	testTable = append(testTable, bundleRootCase(
		"bare-and-dynamic/git",
		"claude", "repo", true, files,
		ok("sync").notOut("hook.support-reference-missing").has(native, "/Stop-command/hooks/$SCRIPT").lacks(native, "CLAUDE_PLUGIN_ROOT"),
	))
}

// spec: hook-resources/legacy-folder-relative-references-keep-working-with-a-warning
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"`, ""), "hooks/scripts/check.sh")
	native, root := bundleRootNative("claude", "repo")
	support := root + "/.claude/hook-support"
	for _, git := range []bool{true, false} {
		testTable = append(testTable, bundleRootCase(
			"legacy-reference/"+bundleRootSourceName(git),
			"claude", "repo", git, files,
			ok("sync").out("hook.support-reference-legacy").has(native, "/PreToolUse-command/hooks/scripts/check.sh").globs(support+"/*/PreToolUse-command/hooks/scripts/check.sh"),
			ok("sync", "--check"),
		))
	}
}

// spec: hook-resources/quoting-only-the-plugin-root-token-keeps-the-reference-checked
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}"/scripts/check.sh`, ""), "hooks/scripts/check.sh")
	native, root := bundleRootNative("claude", "repo")
	support := root + "/.claude/hook-support"
	testTable = append(testTable, bundleRootCase(
		"quoted-token-legacy/git",
		"claude", "repo", true, files,
		ok("sync").out("hook.support-reference-legacy").has(native, "/PreToolUse-command/hooks/scripts/check.sh").lacks(native, "CLAUDE_PLUGIN_ROOT").globs(support+"/*/PreToolUse-command/hooks/scripts/check.sh"),
		ok("sync", "--check"),
	))
}

// spec: hook-resources/references-that-resolve-under-both-meanings-use-the-bundle-root
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh"`, ""), "hooks/scripts/check.sh", "hooks/hooks/scripts/check.sh")
	native, _ := bundleRootNative("claude", "repo")
	testTable = append(testTable, bundleRootCase(
		"both-meanings/git",
		"claude", "repo", true, files,
		ok("sync").notOut("hook.support-reference-legacy").has(native, "/PreToolUse-command/hooks/scripts/check.sh").lacks(native, "/PreToolUse-command/hooks/hooks/scripts/check.sh"),
	))
}
