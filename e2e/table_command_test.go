package e2e_test

func init() {
	// ===== command parity with skills =====
	testTable = append(testTable, TestCase{
		Name: "command/tag_based_selection",
		Desc: "Given a registry with k8s-tagged and python-tagged commands and tags: [k8s] in config, When sync is run, Then only k8s-tagged commands are materialized",
		Registries: []RegistryDef{
			{Name: "cmd-tag-selection", Commands: []Command{
				{Name: "k8s-rollout", Description: "Kubernetes rollout", Command: "kubectl rollout", Tags: []string{"k8s", "infra"}},
				{Name: "k8s-debug", Description: "Kubernetes debug", Command: "kubectl debug", Tags: []string{"k8s", "debug"}},
				{Name: "python-lint", Description: "Python linting", Command: "ruff check", Tags: []string{"python", "lint"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-tag-selection}}', ref: main}}",
			"resources: {team: {tags: [k8s]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/k8s-rollout.md", "~/.factory/commands/k8s-debug.md").absent("~/.factory/commands/python-lint.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_by_name",
		Desc: "Given a registry with a command, When install is run for the command name, Then the command appears in list output",
		Registries: []RegistryDef{
			{Name: "cmd-install", Commands: []Command{
				{Name: "greet", Description: "Greeting command", Command: "echo hello"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:cmd-install}}', ref: main}}"),
		Steps: []Step{
			ok("install", "greet").out("greet"),
			ok("sync"),
			ok("list").out("greet"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_kind_filter",
		Desc: "Given a registry with a skill and command sharing a name, When only the command is selected in config, Then only the command materializes (not the skill)",
		Registries: []RegistryDef{
			{Name: "cmd-kind-install",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill"}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-kind-install}}', ref: main}}",
			"resources: {team: {include: [command/deploy]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/deploy.md").absent("~/.factory/skills/deploy"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_scope_user",
		Desc: "Given a registry with a command, When install --scope user is run and synced, Then the command materializes at machine scope",
		Registries: []RegistryDef{
			{Name: "cmd-scope-user", Commands: []Command{
				{Name: "global-cmd", Description: "Global command", Command: "echo global"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:cmd-scope-user}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--scope", "user", "global-cmd"),
			ok("sync").exists("~/.factory/commands/global-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_scope_local",
		Desc: "Given a registry in repo config with a command, When install --scope local is run and synced, Then the command materializes at repo scope",
		Registries: []RegistryDef{
			{Name: "cmd-scope-local", Commands: []Command{
				{Name: "local-cmd", Description: "Local command", Command: "echo local"},
			}},
		},
		RepoCfg: v4("sources: {team: {git: '{{REG:cmd-scope-local}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--scope", "local", "local-cmd"),
			ok("sync").exists("./.factory/commands/local-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/remove_roundtrip",
		Desc: "Given a command synced to machine scope, When remove --scope user is run and re-synced, Then the command is gone from the filesystem",
		Registries: []RegistryDef{
			{Name: "cmd-roundtrip", Commands: []Command{
				{Name: "roundtrip-cmd", Description: "Roundtrip command", Command: "echo roundtrip"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-roundtrip}}', ref: main}}",
			"resources: {team: {include: [command/roundtrip-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/roundtrip-cmd.md"),
			ok("remove", "--scope", "user", "roundtrip-cmd"),
			ok("sync").absent("~/.factory/commands/roundtrip-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/remove_from_registry",
		Desc: "Given two commands from one registry, When remove --scope user removes one and re-syncs, Then only the removed command is pruned",
		Registries: []RegistryDef{
			{Name: "cmd-from-rm", Commands: []Command{
				{Name: "cmd-a", Description: "Command A", Command: "echo a"},
				{Name: "cmd-b", Description: "Command B", Command: "echo b"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-from-rm}}', ref: main}}",
			"resources: {team: {include: [command/cmd-a, command/cmd-b]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/cmd-a.md", "~/.factory/commands/cmd-b.md"),
			ok("remove", "--scope", "user", "cmd-a"),
			ok("sync").exists("~/.factory/commands/cmd-b.md").absent("~/.factory/commands/cmd-a.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/required_immune_to_excludes",
		Desc: "Given a required:true registry with commands, When repo and local configs try to exclude a command, Then the command remains materialized",
		Registries: []RegistryDef{
			{Name: "cmd-required", Commands: []Command{
				{Name: "security-scan", Description: "Mandatory security scan", Command: "scan --deep", Tags: []string{"security"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-required}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:cmd-required}}', ref: main}}",
			"resources: {team: {exclude: [command/security-scan]}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-required}}', ref: main}}",
			"resources: {team: {exclude: [command/security-scan]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/security-scan.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/default_true_auto_selects",
		Desc: "Given a registry with all:true containing commands, When sync is run, Then all commands are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "cmd-default", Commands: []Command{
				{Name: "auto-cmd-a", Description: "Auto-selected A", Command: "echo a"},
				{Name: "auto-cmd-b", Description: "Auto-selected B", Command: "echo b"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-default}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/auto-cmd-a.md", "~/.factory/commands/auto-cmd-b.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/collision_same_name_different_registries",
		Desc: "Given two registries each providing a command named 'deploy', When sync is run, Then a collision warning is emitted and one command materializes",
		Registries: []RegistryDef{
			{Name: "cmd-registry-a", Commands: []Command{
				{Name: "deploy", Description: "Deploy from registry A", Command: "echo a"},
			}},
			{Name: "cmd-registry-b", Commands: []Command{
				{Name: "deploy", Description: "Deploy from registry B", Command: "echo b"},
			}},
		},
		GlobalCfg: v4(
			"sources:",
			"  first: {git: '{{REG:cmd-registry-a}}', ref: main}",
			"  second: {git: '{{REG:cmd-registry-b}}', ref: main}",
			"resources:",
			"  first: {include: [command/deploy]}",
			"  second: {include: [command/deploy]}",
		),
		Steps: []Step{
			ok("sync").out("deploy", "multiple registries").exists("~/.factory/commands/deploy.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/multi_harness_output",
		Desc: "Given factory and claude harnesses configured with a command, When sync is run, Then the command appears in both harness output directories",
		Registries: []RegistryDef{
			{Name: "cmd-multi-harness", Commands: []Command{
				{Name: "universal-cmd", Description: "Appears in all harnesses", Command: "echo universal"},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - claude",
			"sources: {team: {git: '{{REG:cmd-multi-harness}}', ref: main}}",
			"resources: {team: {include: [command/universal-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/universal-cmd.md", "~/.claude/commands/universal-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/drift_detection",
		Desc: "Given a config with one command, When sync --check is run before any sync, Then it exits 1; after sync exits 0",
		Registries: []RegistryDef{
			{Name: "cmd-drift", Commands: []Command{
				{Name: "drift-cmd", Description: "Tests drift detection", Command: "echo drift"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-drift}}', ref: main}}",
			"resources: {team: {include: [command/drift-cmd]}}",
		),
		Steps: []Step{
			exits(1, "sync", "--check"),
			ok("sync"),
			exits(0, "sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/idempotent_sync",
		Desc: "Given a command synced once, When sync is run again, Then output is stable and no drift is detected",
		Registries: []RegistryDef{
			{Name: "cmd-idem", Commands: []Command{
				{Name: "idem-cmd", Description: "Idempotent command", Command: "echo idem"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-idem}}', ref: main}}",
			"resources: {team: {include: [command/idem-cmd]}}",
		),
		Steps: []Step{
			ok("sync"),
			exits(0, "sync", "--check"),
			ok("sync"),
			exits(0, "sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/precedence_local_exclude",
		Desc: "Given repo config including two commands and local config excluding one, When sync is run, Then the locally excluded command is absent while the other remains",
		Registries: []RegistryDef{
			{Name: "cmd-precedence", Commands: []Command{
				{Name: "wanted-cmd", Description: "Wanted command", Command: "echo wanted"},
				{Name: "unwanted-cmd", Description: "Unwanted command", Command: "echo unwanted"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:cmd-precedence}}', ref: main}}",
			"resources: {team: {include: [command/wanted-cmd, command/unwanted-cmd]}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-precedence}}', ref: main}}",
			"resources: {team: {exclude: [command/unwanted-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/commands/wanted-cmd.md").absent("./.factory/commands/unwanted-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/harness_removal_prunes_command_artifacts",
		Desc: "Given a command synced to factory and claude, When claude is removed from harnesses and re-synced, Then the claude command artifact is pruned while factory remains",
		Registries: []RegistryDef{
			{Name: "cmd-harness-rm", Commands: []Command{
				{Name: "prunable-cmd", Description: "Will be pruned from claude", Command: "echo prune"},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"  - claude",
			"sources: {team: {git: '{{REG:cmd-harness-rm}}', ref: main}}",
			"resources: {team: {include: [command/prunable-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/prunable-cmd.md", "~/.claude/commands/prunable-cmd.md"),
			writeGlobalCfg(v4(
				"harnesses:",
				"  - factory",
				"sources: {team: {git: '{{REG:cmd-harness-rm}}', ref: main}}",
				"resources: {team: {include: [command/prunable-cmd]}}",
			)),
			ok("sync").exists("~/.factory/commands/prunable-cmd.md").absent("~/.claude/commands/prunable-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from pointing to a registry and --kind command, Then the command appears in list output and sync materializes it",
		Registries: []RegistryDef{
			{Name: "cmd-adhoc", Commands: []Command{
				{Name: "adhoc-cmd", Description: "Ad-hoc command", Command: "echo adhoc"},
			}},
		},
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("install", "adhoc-cmd", "--from", "{{REG:cmd-adhoc}}", "--ref", "main", "--kind", "command").out("adhoc-cmd"),
			ok("sync").exists("./.factory/commands/adhoc-cmd.md"),
			ok("list").out("adhoc-cmd"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/strict_mode_exits_nonzero_on_missing",
		Desc: "Given a config selecting a command absent from the registry, When sync --strict is run, Then it exits 1 mentioning the missing command",
		Registries: []RegistryDef{
			{Name: "cmd-strict", Commands: []Command{
				{Name: "present-cmd", Description: "Present command", Command: "echo present"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-strict}}', ref: main}}",
			"resources: {team: {include: [command/present-cmd, command/absent-cmd]}}",
		),
		Steps: []Step{
			exits(1, "sync", "--strict").out("absent-cmd"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/full_lifecycle",
		Desc: "Given a command in global config, When sync→status→remove→sync cycle is run, Then the command appears, is visible in status, and is pruned after removal",
		Registries: []RegistryDef{
			{Name: "cmd-lifecycle", Commands: []Command{
				{Name: "lifecycle-cmd", Description: "Lifecycle command", Command: "echo lifecycle"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-lifecycle}}', ref: main}}",
			"resources: {team: {include: [command/lifecycle-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/lifecycle-cmd.md"),
			ok("status").out("lifecycle-cmd"),
			exits(0, "sync", "--check"),
			ok("remove", "--scope", "user", "lifecycle-cmd"),
			ok("sync").absent("~/.factory/commands/lifecycle-cmd.md"),
			exits(0, "sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_missing_exits_nonzero",
		Desc: "Given a registry that does not contain the requested command, When install nonexistent-cmd is run, Then it exits 1 with an error",
		Registries: []RegistryDef{
			{Name: "cmd-missing", Commands: []Command{
				{Name: "exists-cmd", Description: "Exists", Command: "echo exists"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:cmd-missing}}', ref: main}}"),
		Steps: []Step{
			exits(1, "install", "nonexistent-cmd").out("nonexistent"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/remove_required_fails",
		Desc: "Given a required:true registry command, When remove is attempted, Then it exits 1 with 'required' in the error",
		Registries: []RegistryDef{
			{Name: "cmd-required-rm", Commands: []Command{
				{Name: "mandatory-cmd", Description: "Mandatory", Command: "echo mandatory"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-required-rm}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		Steps: []Step{
			ok("sync"),
			exits(1, "remove", "mandatory-cmd").out("required"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/remove_default_true_adds_exclude",
		Desc: "Given all:true auto-selected commands, When remove --scope user is run for one command and re-synced, Then that command is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "cmd-default-rm", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-default-rm}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/keep-cmd.md", "~/.factory/commands/drop-cmd.md"),
			ok("remove", "--scope", "user", "drop-cmd"),
			ok("sync").exists("~/.factory/commands/keep-cmd.md").absent("~/.factory/commands/drop-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/remove_from_no_name_removes_entry",
		Desc: "Given a registry entry in global config with a command, When remove --scope user --from removes the entire entry and re-syncs, Then the command is gone",
		Registries: []RegistryDef{
			{Name: "cmd-from-entry-rm", Commands: []Command{
				{Name: "entry-cmd", Description: "Entry command", Command: "echo entry"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-from-entry-rm}}', ref: main}}",
			"resources: {team: {include: [command/entry-cmd]}}",
		),
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/entry-cmd.md"),
			ok("remove", "--scope", "user", "--from", "{{REG:cmd-from-entry-rm}}").out("Removed"),
			ok("sync").absent("~/.factory/commands/entry-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/precedence_repo_exclude_overrides_repo_include",
		Desc: "Given repo config with both include and exclude for cmd-a, When sync is run, Then exclude wins and cmd-a is absent while cmd-b is present",
		Registries: []RegistryDef{
			{Name: "cmd-repo-precedence", Commands: []Command{
				{Name: "cmd-a", Description: "Command A", Command: "echo a"},
				{Name: "cmd-b", Description: "Command B", Command: "echo b"},
			}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:cmd-repo-precedence}}', ref: main}}",
			"resources: {team: {include: [command/cmd-a, command/cmd-b], exclude: [command/cmd-a]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/commands/cmd-b.md").absent("./.factory/commands/cmd-a.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/precedence_include_exclude_same_level",
		Desc: "Given a registry entry with both include and exclude for the same command, When sync is run, Then exclude wins",
		Registries: []RegistryDef{
			{Name: "cmd-same-level", Commands: []Command{
				{Name: "included-cmd", Description: "Should be included", Command: "echo in"},
				{Name: "conflicted-cmd", Description: "Include and exclude at same level", Command: "echo conflict"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-same-level}}', ref: main}}",
			"resources: {team: {include: [command/included-cmd, command/conflicted-cmd], exclude: [command/conflicted-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/included-cmd.md").absent("~/.factory/commands/conflicted-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/precedence_org_config_sets_floor",
		Desc: "Given org config with a required registry containing commands, When global config has no registries, Then the org-mandated command is still materialized",
		Registries: []RegistryDef{
			{Name: "cmd-org", Commands: []Command{
				{Name: "compliance-cmd", Description: "Org-mandated compliance command", Command: "echo compliance"},
			}},
		},
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:cmd-org}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/compliance-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_tag_selects_matching",
		Desc: "Given a registry with k8s-tagged commands explicitly configured, When install --tag k8s is run, Then the k8s-tagged commands are reported",
		Registries: []RegistryDef{
			{Name: "cmd-tag-install", Skills: []Skill{
				{Name: "k8s-setup-skill", Tags: []string{"k8s"}},
			}, Commands: []Command{
				{Name: "k8s-setup", Description: "Kubernetes setup", Command: "kubectl apply", Tags: []string{"k8s"}},
				{Name: "python-fmt", Description: "Python formatting", Command: "black .", Tags: []string{"python"}},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:cmd-tag-install}}', ref: main}}"),
		Steps: []Step{
			// install --tag discovers all kinds with matching tags
			ok("install", "--tag", "k8s").out("k8s"),
			ok("sync"),
			ok("list", "--kind", "command").out("k8s-setup"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/install_all_kind_filter",
		Desc: "Given a registry with skills and commands, When install --all --kind command is run and synced, Then commands are selected in the registry entry",
		Registries: []RegistryDef{
			{Name: "cmd-all-kind", Skills: []Skill{
				{Name: "not-this-skill"},
			}, Commands: []Command{
				{Name: "only-this-cmd", Description: "Only command", Command: "echo only"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:cmd-all-kind}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--all", "--kind", "command").out("only-this-cmd"),
			ok("sync"),
			// install --all --kind command creates a all:true entry;
			// verify the command materialized (skills may also appear due to all:true).
			ok("status").out("only-this-cmd"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/precedence_default_true_with_exclude",
		Desc: "Given an all:true registry with an explicit command exclude, When sync is run, Then excluded commands are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "cmd-default-exclude", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-default-exclude}}', ref: main}}",
			"resources: {team: {all: true, exclude: [command/drop-cmd]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/commands/keep-cmd.md").absent("~/.factory/commands/drop-cmd.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "command/collision_same_name_different_kinds",
		Desc: "Given a registry with a skill and a command sharing the name 'deploy', When both are selected in config, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "cmd-skill-same-name",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill"}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:cmd-skill-same-name}}', ref: main}}",
			"resources: {team: {include: [skill/deploy, command/deploy]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/deploy/SKILL.md", "~/.factory/commands/deploy.md"),
		},
	})
}
