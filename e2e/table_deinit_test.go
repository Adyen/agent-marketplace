package e2e_test

import (
	"crypto/sha256"
	"fmt"
)

func init() {
	// ===== deinit command =====
	testTable = append(testTable, TestCase{
		Name:       "deinit/windsurf_manifest_cannot_nominate_repo_files",
		Desc:       "Given actual portable Windsurf output and a manifest nominating an unrelated repository file, When sync checking or config-purging deinit runs, Then refusal preserves repository bytes and metadata until the manifest is repaired and real native teardown succeeds",
		Registries: []RegistryDef{{Name: "windsurf-manifest-scope", Skills: []Skill{{Name: "teardown-skill", Description: "Scoped native control"}}}},
		GlobalCfg:  v4("harnesses: [windsurf]"),
		RepoCfg: v4(
			"harnesses: [windsurf]",
			"sources:",
			"  fixture:",
			"    git: {{REG:windsurf-manifest-scope}}",
			"    ref: main",
			"resources:",
			"  fixture: [skill/teardown-skill]",
			"materialization:",
			"  mode: file",
		),
		Steps: []Step{
			ok("sync").regular("./.windsurf/skills/teardown-skill/SKILL.md"),
			ok("deinit", "--scope", "repo").absent("./.windsurf/skills/teardown-skill/SKILL.md"),
			writeFile("./README.md", "body"),
			writeFile("./.agents/materialization-manifest.yaml", fmt.Sprintf("version: 1\nentries:\n  - target: windsurf\n    path: README.md\n    digest: sha256:%x\n", sha256.Sum256([]byte("file\x00\x000644\x004\x00body")))),
			exits(1, "sync", "--check").
				out("materialize.manifest.error").
				absent("./.windsurf").
				unchanged("./README.md", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			exits(1, "deinit", "--scope", "repo", "--purge-config", "--dry-run").
				out("materialize.manifest.error").
				notOut("Would remove config", "Would remove lock", "No managed artifacts to remove").
				absent("./.windsurf").
				unchanged("./README.md", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			exits(1, "deinit", "--scope", "repo", "--purge-config").
				out("materialize.manifest.error").
				notOut("Removed config", "Removed lock", "No managed artifacts to remove").
				absent("./.windsurf").
				unchanged("./README.md", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			writeFile("./.agents/materialization-manifest.yaml", "version: 1\nentries: []\n").
				then("sync").
				pass().
				unchanged("./README.md").
				regular("./.windsurf/skills/teardown-skill/SKILL.md"),
			ok("deinit", "--scope", "repo", "--purge-config").
				absent("./.agents/config.yaml", "./.agents/lock.yaml", "./.windsurf/skills/teardown-skill/SKILL.md").
				unchanged("./README.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "deinit/portable_manifest_cannot_nominate_shared_hooks_file",
		Desc:       "Given actual portable Factory output and a manifest nominating the handwritten shared hooks file inside the hooks component, When sync checking or config-purging deinit runs, Then refusal preserves the hooks file bytes and metadata until the manifest is repaired and real native teardown succeeds",
		Registries: []RegistryDef{{Name: "factory-manifest-scope", Skills: []Skill{{Name: "teardown-skill", Description: "Scoped native control"}}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  fixture:",
			"    git: {{REG:factory-manifest-scope}}",
			"    ref: main",
			"resources:",
			"  fixture: [skill/teardown-skill]",
			"materialization:",
			"  mode: file",
		),
		Steps: []Step{
			ok("sync").regular("./.factory/skills/teardown-skill/SKILL.md"),
			ok("deinit", "--scope", "repo").absent("./.factory/skills/teardown-skill/SKILL.md"),
			writeFile("./.factory/hooks/hooks.json", handwrittenFactoryHooks),
			writeFile("./.agents/materialization-manifest.yaml", fmt.Sprintf("version: 1\nentries:\n  - target: factory\n    path: hooks/hooks.json\n    digest: sha256:%x\n", sha256.Sum256(fmt.Appendf(nil, "file\x00\x000644\x00%d\x00%s", len(handwrittenFactoryHooks), handwrittenFactoryHooks)))),
			exits(1, "sync", "--check").
				out("materialize.manifest.error").
				absent("./.factory/skills/teardown-skill").
				unchanged("./.factory/hooks/hooks.json", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			exits(1, "deinit", "--scope", "repo", "--purge-config", "--dry-run").
				out("materialize.manifest.error").
				notOut("Would remove config", "Would remove lock", "No managed artifacts to remove").
				unchanged("./.factory/hooks/hooks.json", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			exits(1, "deinit", "--scope", "repo", "--purge-config").
				out("materialize.manifest.error").
				notOut("Removed config", "Removed lock", "No managed artifacts to remove").
				unchanged("./.factory/hooks/hooks.json", "./.agents/materialization-manifest.yaml", "./.agents/config.yaml", "./.agents/lock.yaml"),
			writeFile("./.agents/materialization-manifest.yaml", "version: 1\nentries: []\n").
				then("sync").
				pass().
				unchanged("./.factory/hooks/hooks.json").
				regular("./.factory/skills/teardown-skill/SKILL.md"),
			ok("deinit", "--scope", "repo", "--purge-config").
				absent("./.agents/config.yaml", "./.agents/lock.yaml", "./.factory/skills/teardown-skill/SKILL.md").
				unchanged("./.factory/hooks/hooks.json"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "deinit/manifest_failure_preserves_config_and_output",
		Desc:       "Given installed output and an invalid manifest, When deinit attempts config purge, Then it reports failure and preserves state until the manifest is repaired",
		Registries: []RegistryDef{{Name: "deinit-invalid-manifest", Skills: []Skill{{Name: "teardown-skill", Description: "Deinit preservation"}}}},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:deinit-invalid-manifest}}', ref: main}}",
			"resources: {team: {include: [skill/teardown-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/teardown-skill/SKILL.md", "./.agents/config.yaml", "./.agents/lock.yaml"),
			writeFile("./.agents/materialization-manifest.yaml", "version: 99\nentries: []\n"),
			exits(1, "deinit", "--scope", "repo", "--purge-config", "--dry-run").
				out("materialize.manifest.error").
				notOut("Would remove config", "Would remove lock", "No managed artifacts to remove").
				unchanged("./.agents/config.yaml", "./.agents/lock.yaml", "./.agents/materialization-manifest.yaml", "./.factory"),
			exits(1, "deinit", "--scope", "repo", "--purge-config").
				out("materialize.manifest.error").
				notOut("Removed config", "Removed lock", "No managed artifacts to remove").
				unchanged("./.agents/config.yaml", "./.agents/lock.yaml", "./.agents/materialization-manifest.yaml", "./.factory"),
			writeFile("./.agents/materialization-manifest.yaml", "version: 1\nentries: []\n").
				then("deinit", "--scope", "repo", "--purge-config").
				pass().
				out("Removed config", "Removed lock").
				absent("./.agents/config.yaml", "./.agents/lock.yaml", "./.factory/skills/teardown-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "deinit/repo_scope_removes_repo_artifacts",
		Desc: "Given a synced repo with artifacts, When deinit --scope repo is run, Then repo-scoped artifacts are removed and machine-scope artifacts are untouched",
		Registries: []RegistryDef{
			{Name: "deinit-reg", Skills: []Skill{
				{Name: "teardown-skill", Description: "Skill for deinit test"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:deinit-reg}}', ref: main}}",
			"resources: {team: {include: [skill/teardown-skill]}}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:deinit-reg}}', ref: main}}",
			"resources: {team: {include: [skill/teardown-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/teardown-skill/SKILL.md"),
			ok("deinit", "--scope", "repo").out("managed artifact").absent("./.factory/skills/teardown-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "deinit/user_scope_removes_artifacts",
		Desc: "Given a synced environment with an exact locked cache, When deinit is run, Then machine-scoped artifacts are removed while reachable cache remains managed by GC",
		Registries: []RegistryDef{
			{Name: "deinit-cache-reg", Skills: []Skill{
				{Name: "cached-skill", Description: "Skill to populate cache"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:deinit-cache-reg}}', ref: main}}",
			"resources: {team: {include: [skill/cached-skill]}}",
		),
		Steps: []Step{
			ok("sync"),
			ok("deinit").out("managed artifact").absent("~/.factory/skills/cached-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "deinit/dry_run_preserves_artifacts",
		Desc: "Given a synced repo with artifacts, When deinit --dry-run is run, Then artifacts remain on disk",
		Registries: []RegistryDef{
			{Name: "deinit-dry-reg", Skills: []Skill{
				{Name: "dry-skill", Description: "Skill for dry-run test"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:deinit-dry-reg}}', ref: main}}",
			"resources: {team: {include: [skill/dry-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/dry-skill/SKILL.md"),
			ok("deinit", "--scope", "repo", "--dry-run").out("Would remove").exists("./.factory/skills/dry-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "deinit/purge_config_removes_repo_config",
		Desc: "Given a synced repo with config and a generated lock, When deinit --scope repo --purge-config is run, Then both files are deleted without transaction coordination files in the repository",
		Registries: []RegistryDef{
			{Name: "deinit-purge-reg", Skills: []Skill{
				{Name: "purge-skill", Description: "Skill for purge test"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:deinit-purge-reg}}', ref: main}}",
			"resources: {team: {include: [skill/purge-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.agents/config.yaml", "./.agents/lock.yaml"),
			ok("deinit", "--scope", "repo", "--purge-config").
				out("Removed config", "Removed lock").
				absent("./.agents/config.yaml", "./.agents/lock.yaml", "./.agents/.config.yaml.dependencytxn.lock", "./.agents/.lock.yaml.dependencytxn.lock"),
		},
	})

	// spec: materialization/deinit-removes-gemini-subagents
	testTable = append(testTable, TestCase{
		Name: "deinit/gemini_repo_subagent_output_is_removed",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-deinit", Subagents: []Subagent{
				{Name: "helper", Description: "Removed by deinit"},
			}},
		},
		GlobalCfg: v4("harnesses: [gemini]"),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:gemini-subagent-deinit}}', ref: main}}",
			"resources: {team: {include: [subagent/helper]}}",
		),
		Steps: []Step{
			writeFile("./.gemini/agents/worker.md", `---
name: worker
description: my hand-written worker subagent
---

# worker (user-owned)
`),
			ok("sync").symlinks("./.gemini/agents/helper.md").exists("./.gemini/agents/worker.md"),
			ok("deinit", "--scope", "repo").absent("./.gemini/agents/helper.md").exists("./.gemini/agents/worker.md").has("./.gemini/agents/worker.md", "# worker (user-owned)"),
		},
	})

	// spec: materialization/deinit-removes-gemini-subagents
	testTable = append(testTable, TestCase{
		Name: "deinit/gemini_user_subagent_output_is_removed",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-deinit-user", Subagents: []Subagent{
				{Name: "helper", Description: "Removed by deinit"},
			}},
		},
		GlobalCfg: v4(
			"harnesses: [gemini]",
			"sources: {team: {git: '{{REG:gemini-subagent-deinit-user}}', ref: main}}",
			"resources: {team: {include: [subagent/helper]}}",
		),
		Steps: []Step{
			writeFile("~/.gemini/agents/worker.md", `---
name: worker
description: my hand-written worker subagent
---

# worker (user-owned)
`),
			ok("sync").symlinks("~/.gemini/agents/helper.md").exists("~/.gemini/agents/worker.md"),
			ok("deinit", "--scope", "user").absent("~/.gemini/agents/helper.md").exists("~/.gemini/agents/worker.md").has("~/.gemini/agents/worker.md", "# worker (user-owned)"),
		},
	})
}
