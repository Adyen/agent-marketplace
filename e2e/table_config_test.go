package e2e_test

func init() {
	testTable = append(testTable, TestCase{
		Name:     "format/startup_refuses_unsupported_repo_layers",
		Desc:     "Given a v4 user root and an unsupported repository root, When listing, Then startup refuses without conversion or edits",
		RepoCfg:  "apiVersion: agent-marketplace/v2\nsources: {}\n",
		LocalCfg: v4(),
		Steps: []Step{
			exits(1, "list").out("v4.api-version", "apiVersion").notOut("Migrate these configuration files").unchanged("./.agents"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "config_layer/global_repo_local_merge",
		Desc: "Given global config with global-skill, repo config with repo-skill, and local config with local-skill, When sync is run, Then skills materialize at their respective scopes",
		Registries: []RegistryDef{
			{Name: "merge-reg", Skills: []Skill{
				{Name: "global-skill"},
				{Name: "repo-skill"},
				{Name: "local-skill"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:merge-reg}}', ref: main}}",
			"resources: {team: {include: [skill/global-skill]}}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:merge-reg}}', ref: main}}",
			"resources: {team: {include: [skill/repo-skill]}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:merge-reg}}', ref: main}}",
			"resources: {team: {include: [skill/local-skill]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/global-skill/SKILL.md", "./.factory/skills/repo-skill/SKILL.md", "./.factory/skills/local-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_user/inspect_lock_and_backup",
		Desc:       "Given a v4 user config that selects a Git skill, When status, doctor, lock, and backup run, Then each succeeds against the v4 layer, backup restore recovers a changed user-owned skill file, and the workspace stays in sync",
		Registries: []RegistryDef{{Name: "v4-user-ops", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  tools: {git: '{{REG:v4-user-ops}}', ref: main}",
			"resources:",
			"  tools: {include: [skill/review]}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/review/SKILL.md", "~/.agents/lock.yaml"),
			ok("status").out("SYNCED RESOURCES", "review", "USER"),
			ok("doctor").out("agent-marketplace doctor"),
			ok("lock", "--scope", "user").out("Locks: 0 changed"),
			ok("lock", "--check"),
			writeFile("~/.factory/skills/handwritten/SKILL.md", "backed up\n").then("backup", "create").pass().out("Backup created for: factory"),
			ok("backup", "list").out("factory (1)"),
			writeFile("~/.factory/skills/handwritten/SKILL.md", "changed after backup\n").
				then("backup", "restore", "--target", "factory").
				pass().
				exists("~/.factory/skills/review/SKILL.md").
				has("~/.factory/skills/handwritten/SKILL.md", "backed up"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_user/gc_and_deinit",
		Desc:       "Given a v4 user config that selects a Git skill, When deinit runs, the source is dropped, and gc runs, Then deinit keeps the config, gc removes only the orphaned cache, and deinit --purge-config removes the v4 config and its lock",
		Registries: []RegistryDef{{Name: "v4-user-gc", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  tools: {git: '{{REG:v4-user-gc}}', ref: main}",
			"resources:",
			"  tools: {include: [skill/review]}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/review/SKILL.md"),
			ok("gc", "--dry-run").out("Would remove 0 cache entries"),
			ok("deinit", "--dry-run").out("Would remove 1 managed artifact(s)").exists("~/.factory/skills/review/SKILL.md"),
			ok("deinit").out("Removed 1 managed artifact(s)").exists("~/.agents/config.yaml").absent("~/.factory/skills/review"),
			ok("sync").exists("~/.factory/skills/review/SKILL.md"),
			writeGlobalCfg(v4("harnesses: [factory]")).then("sync").pass().absent("~/.factory/skills/review"),
			ok("gc", "--dry-run").notOut("Would remove 0 cache entries"),
			ok("gc").notOut("Removed 0 cache entries"),
			ok("gc", "--dry-run").out("Would remove 0 cache entries").exists("~/.agents/config.yaml", "~/.agents/lock.yaml"),
			ok("deinit", "--purge-config").absent("~/.agents/config.yaml", "~/.agents/lock.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "v4_user/telemetry_preferences",
		Desc: "Given a v4 user config, When telemetry is inspected, enabled with an endpoint, previewed, reset, purged, and disabled, Then each succeeds and the user config stays v4",
		Steps: []Step{
			ok("telemetry").out("Usage:"),
			ok("telemetry", "status").out("Effective level"),
			ok("telemetry", "enable", "--level", "basic", "--endpoint", "http://127.0.0.1:9").has("~/.agents/config.yaml", "endpoint: http://127.0.0.1:9"),
			ok("telemetry", "status").out("basic"),
			ok("telemetry", "preview", "--level", "basic"),
			ok("telemetry", "reset-id"),
			ok("telemetry", "purge"),
			ok("telemetry", "disable").has("~/.agents/config.yaml", "apiVersion: agent-marketplace/v4").lacks("~/.agents/config.yaml", "level: basic"),
			ok("backup").out("Usage:"),
			ok("init", "--harnesses", "factory").out("already initialized").unchanged("~/.agents/config.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_user/populated_telemetry_preferences_and_backup",
		Desc:       "Given a v4 user config that selects a Git skill, When telemetry is managed and a backup is created, listed, and restored, Then each succeeds, the config stays v4, and restore recovers a changed user-owned skill file",
		Registries: []RegistryDef{{Name: "v4-user-backup", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:v4-user-backup}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/review/SKILL.md"),
			ok("telemetry").out("Usage:"),
			ok("telemetry", "status").out("Effective level"),
			ok("telemetry", "enable", "--level", "basic", "--endpoint", "http://127.0.0.1:9").has("~/.agents/config.yaml", "endpoint: http://127.0.0.1:9"),
			ok("telemetry", "preview", "--level", "basic"),
			ok("telemetry", "reset-id"),
			ok("telemetry", "purge"),
			ok("telemetry", "disable").has("~/.agents/config.yaml", "apiVersion: agent-marketplace/v4").lacks("~/.agents/config.yaml", "level: basic"),
			ok("backup").out("Usage:"),
			writeFile("~/.factory/skills/handwritten/SKILL.md", "backed up\n").then("backup", "create").pass().out("Backup created for: factory"),
			ok("backup", "list").out("factory (1)"),
			writeFile("~/.factory/skills/handwritten/SKILL.md", "changed after backup\n").
				then("backup", "restore", "--target", "factory").
				pass().
				has("~/.factory/skills/handwritten/SKILL.md", "backed up"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_repo/install_selectors_list_and_maintenance_flags",
		Desc:       "Given a v4 user config and a v4 repository config that declares a Git source with nothing selected, When listing the registry ad hoc and installing by tag, by kind with --all and --registry, and by name with --kind and --verbose, Then every selection lands in the v4 repository config, and the sync and deinit flags succeed against it",
		Registries: []RegistryDef{{Name: "v4-selectors", Skills: []Skill{{Name: "review", Description: "Review changes", Tags: []string{"k8s"}}, {Name: "deploy", Description: "Deploy"}}, Commands: []Command{{Name: "cmd-one", Description: "Command one", Command: "echo one"}}}},
		RepoCfg: v4(
			"sources:",
			"  tools: {git: '{{REG:v4-selectors}}', ref: main}",
		),
		Steps: []Step{
			ok("list", "--from", "{{REG:v4-selectors}}", "--ref", "main", "--path", "skills", "--kind", "skill").out("review", "deploy").notOut("cmd-one"),
			ok("install", "--tag", "k8s", "--scope", "repo").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/deploy").has("./.agents/config.yaml", "k8s"),
			ok("install", "--all", "--kind", "command", "--registry", "{{REG:v4-selectors}}", "--scope", "repo").exists("./.factory/commands/cmd-one.md"),
			ok("install", "deploy", "--kind", "skill", "--verbose", "--scope", "repo").exists("./.factory/skills/deploy/SKILL.md"),
			ok("list", "--kind", "command").out("cmd-one").notOut("deploy"),
			ok("sync", "--verbose", "--no-backup"),
			ok("sync", "--check").has("./.agents/config.yaml", "apiVersion: agent-marketplace/v4").lacks("./.agents/config.yaml", "registries:"),
			ok("deinit", "--scope", "repo", "--dry-run").out("Would remove").exists("./.factory/skills/review/SKILL.md"),
			ok("deinit", "--scope", "repo").exists("./.agents/config.yaml").absent("./.factory/skills/review"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "v4_repo/adhoc_path_and_registry_flags",
		Desc: "Given a v4 user and repository config, When installing everything from one configured registry and installing, listing, and removing a resource from an ad hoc registry path, Then each step succeeds and the repository config stays v4",
		Registries: []RegistryDef{
			{Name: "v4-flags", Skills: []Skill{{Name: "review", Description: "Review changes"}, {Name: "deploy", Description: "Deploy"}}},
			{Name: "v4-adhoc", Skills: []Skill{{Name: "adhoc-skill", Description: "Ad hoc skill"}}},
		},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4("sources: {team: {git: '{{REG:v4-flags}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--all", "--registry", "{{REG:v4-flags}}", "--scope", "repo").exists("./.factory/skills/review/SKILL.md", "./.factory/skills/deploy/SKILL.md"),
			ok("list", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills").out("adhoc-skill"),
			ok("install", "adhoc-skill", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills", "--scope", "repo").
				exists("./.factory/skills/adhoc-skill/SKILL.md").
				has("./.agents/config.yaml", "path: skills"),
			ok("remove", "adhoc-skill", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills").absent("./.factory/skills/adhoc-skill"),
			ok("sync", "--verbose").exists("./.factory/skills/review/SKILL.md").absent("./.factory/skills/adhoc-skill"),
			ok("sync", "--check").has("./.agents/config.yaml", "apiVersion: agent-marketplace/v4"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_repo/populated_prune_stale_strict",
		Desc:       "Given a v4 repository config that selects a skill the registry no longer has, When syncing with --prune-stale --strict --verbose, Then the stale selection is pruned from the config and the remaining skill materializes",
		Registries: []RegistryDef{{Name: "v4-prune", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:v4-prune}}', ref: main}}",
			"resources: {team: {include: [skill/review, skill/deleted-skill]}}",
		),
		Steps: []Step{
			ok("sync", "--prune-stale", "--strict", "--verbose").
				out(`pruned stale skill "deleted-skill"`).
				exists("./.factory/skills/review/SKILL.md").
				has("./.agents/config.yaml", "review").
				lacks("./.agents/config.yaml", "deleted-skill"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "v4_repo/prune_stale_strict",
		Desc:       "Given a v4 repository config that selects a skill the registry no longer has, When syncing with --prune-stale --strict --verbose, Then the stale selection is pruned from the config and the remaining skill materializes",
		Registries: []RegistryDef{{Name: "v4-prune", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		RepoCfg: v4(
			"sources:",
			"  tools: {git: '{{REG:v4-prune}}', ref: main}",
			"resources:",
			"  tools: {include: [skill/review, skill/deleted-skill]}",
		),
		Steps: []Step{
			ok("sync", "--prune-stale", "--strict", "--verbose").
				out(`pruned stale skill "deleted-skill"`).
				exists("./.factory/skills/review/SKILL.md").
				has("./.agents/config.yaml", "skill/review").
				lacks("./.agents/config.yaml", "deleted-skill"),
			ok("sync", "--check"),
		},
	})
}
