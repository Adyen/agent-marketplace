package e2e_test

func init() {
	testTable = append(testTable, TestCase{
		Name:       "install/missing_repo_config_is_v4",
		Desc:       "Given a v4 user registry and no repo config, When installing a skill to repo scope, Then the newly created repo root uses v4 and selects the skill",
		Registries: []RegistryDef{{Name: "new-repo-config", Skills: []Skill{{Name: "new-skill"}}}},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {shared: {git: '{{REG:new-repo-config}}', ref: main}}",
		),
		Steps: []Step{
			ok("install", "new-skill", "--scope", "repo").
				has("./.agents/config.yaml", "apiVersion: agent-marketplace/v4").
				has("./.factory/skills/new-skill/SKILL.md", "name: new-skill").
				lacks("./.agents/config.yaml", "apiVersion: agent-marketplace/v2"),
			ok("sync", "--check").has("./.agents/config.yaml", "skill/new-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/collision_warning_scoped",
		Desc: "Given two selected registries with skill and command collisions, When installing one skill, Then its collision is reported without unrelated warnings and the existing winner is installed",
		Registries: []RegistryDef{
			{Name: "registry-a", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry A"}, {Name: "other", Description: "Other from registry A"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry A", Command: "echo COMMAND A"}}},
			{Name: "registry-b", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry B"}, {Name: "other", Description: "Other from registry B"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry B", Command: "echo COMMAND B"}}},
		},
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {}",
		),
		RepoCfg: v4(
			"sources:",
			"  first: {git: '{{REG:registry-a}}', ref: main}",
			"  second: {git: '{{REG:registry-b}}', ref: main}",
			"resources:",
			"  first: {all: true}",
			"  second: {all: true}",
		),
		Steps: []Step{
			ok("install", "deploy", "--kind", "skill", "--from", "{{REG:registry-a}}", "--ref", "main", "--scope", "repo").
				out(`skill "deploy" is provided by multiple registries`, "registry-a", "registry-b", "agent-marketplace remove", "--from").
				notOut(`skill "other" is provided by multiple registries`, `command "deploy" is provided by multiple registries`).
				has("./.factory/skills/deploy/SKILL.md", "Deploy from registry A"),
			ok("sync").
				out(`skill "deploy"`, `skill "other"`, `command "deploy"`, "multiple registries").
				has("./.factory/skills/deploy/SKILL.md", "Deploy from registry A"),
		},
	})

	// ===== install command =====
	testTable = append(testTable, TestCase{
		Name: "install/basic_skill_then_list",
		Desc: "Given a registry with a skill, When install and sync are run, Then list and status show the skill as enabled",
		Registries: []RegistryDef{
			{Name: "install-reg", Skills: []Skill{
				{Name: "code-review", Tags: []string{"review"}},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:install-reg}}', ref: main}}"),
		Steps: []Step{
			ok("install", "code-review").out("code-review"),
			ok("sync"),
			run("list").out("code-review"),
			run("status").out("code-review"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/tag_selects_matching",
		Desc: "Given a registry with k8s-tagged skills, When install --tag k8s is run, Then list shows the k8s-tagged skills",
		Registries: []RegistryDef{
			{Name: "tag-install", Skills: []Skill{
				{Name: "k8s-setup", Tags: []string{"k8s"}},
				{Name: "k8s-teardown", Tags: []string{"k8s"}},
				{Name: "python-fmt", Tags: []string{"python"}},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:tag-install}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--tag", "k8s").out("k8s"),
			// Install writes to repo config; re-read config to verify tag was added.
			ok("list").out("k8s-setup", "k8s-teardown"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/all_with_multi_kind",
		Desc: "Given a registry with skills, commands, and subagents explicitly selected in config, When sync is run, Then all three kinds materialize",
		Registries: []RegistryDef{
			{Name: "multi-kind", Skills: []Skill{
				{Name: "deploy-skill", Tags: []string{"infra"}},
			}, Commands: []Command{
				{Name: "greet", Command: "echo hello"},
			}, Subagents: []Subagent{
				{Name: "worker", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:multi-kind}}', ref: main}}",
			"resources: {team: {include: [skill/deploy-skill, command/greet, subagent/worker]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/skills/deploy-skill/SKILL.md", "~/.factory/commands/greet.md", "~/.factory/droids/worker.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from pointing to a registry, Then the skill appears in list output",
		Registries: []RegistryDef{
			{Name: "adhoc-reg", Skills: []Skill{
				{Name: "adhoc-skill"},
			}},
		},
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			ok("install", "adhoc-skill", "--from", "{{REG:adhoc-reg}}", "--ref", "main").out("adhoc-skill"),
			// Verify the ad-hoc entry was written to repo config.
			ok("list").out("adhoc-skill"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/multiple_names_from_git_source",
		// spec: resource-installation/install-several-names-from-one-explicit-source
		Desc: "Given a git registry with a bundle path and an unrelated configured selection, When install names two resources with shared --from/--ref/--path, Then both materialize, unrelated state is retained, and sync --check passes",
		Registries: []RegistryDef{
			{Name: "multi-src", Files: map[string]string{
				"bundles/skills/first/SKILL.md":  (Skill{Name: "first", Description: "First"}).SkillFile(),
				"bundles/skills/second/SKILL.md": (Skill{Name: "second", Description: "Second"}).SkillFile(),
				"skills/root-only/SKILL.md":      (Skill{Name: "root-only", Description: "Root only"}).SkillFile(),
			}},
			{Name: "multi-other", Skills: []Skill{{Name: "keep"}}},
		},
		RepoCfg: v4(
			"sources: {team: {git: '{{REG:multi-other}}', ref: main}}",
			"resources: {team: {include: [skill/keep]}}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/keep/SKILL.md"),
			ok("install", "first", "second", "--from", "{{REG:multi-src}}", "--ref", "main", "--path", "bundles").
				out("first", "second").
				exists("./.factory/skills/first/SKILL.md", "./.factory/skills/second/SKILL.md", "./.factory/skills/keep/SKILL.md").
				absent("./.factory/skills/root-only").
				has("./.agents/config.yaml", "bundles"),
			ok("sync", "--check"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/missing_skill_exits_nonzero",
		Desc: "Given a registry that does not contain the requested skill, When install nonexistent-skill is run, Then it exits 1 with an error",
		Registries: []RegistryDef{
			{Name: "missing-reg", Skills: []Skill{
				{Name: "exists-skill"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:missing-reg}}', ref: main}}"),
		Steps: []Step{
			exits(1, "install", "nonexistent-skill").out("nonexistent"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:      "install/scope_user_rejects_repo_relative_registry",
		Desc:      "Given a repo-relative local registry, When install --scope user is run, Then it fails without changing the user config",
		GlobalCfg: v4("sources: {}"),
		RepoCfg:   v4("sources: {team: {directory: ./plugins/local}}"),
		Steps: []Step{
			writeFile("./plugins/local/skills/local-skill/SKILL.md", "---\nname: local-skill\ndescription: Local skill\n---\n"),
			exits(1, "install", "--scope", "user", "local-skill").out("repo-relative", "global config").lacks("~/.agents/config.yaml", "plugins/local"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:    "install/from_local_missing_path_does_not_persist",
		Desc:    "Given an existing local registry root with a missing bundle path, When ad-hoc install runs, Then it fails before changing repo config",
		RepoCfg: v4("sources: {}"),
		Steps: []Step{
			writeFile("./local-registry/README.md", "local registry\n"),
			exits(1, "install", "some-skill", "--from", "./local-registry", "--path", "missing").
				out("local registry path does not exist").
				lacks("./.agents/config.yaml", "local-registry"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/scope_user_writes_to_global",
		Desc: "Given a registry in global config, When install --scope user is run and synced, Then the skill materializes at machine scope",
		Registries: []RegistryDef{
			{Name: "scope-reg", Skills: []Skill{
				{Name: "global-skill"},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:scope-reg}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--scope", "user", "global-skill"),
			ok("sync").exists("~/.factory/skills/global-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/scope_local_writes_to_local",
		Desc: "Given a registry in repo config, When install --scope local is run and synced, Then the skill materializes at repo scope",
		Registries: []RegistryDef{
			{Name: "local-reg", Skills: []Skill{
				{Name: "local-skill"},
			}},
		},
		RepoCfg: v4("sources: {team: {git: '{{REG:local-reg}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--scope", "local", "local-skill"),
			ok("sync").exists("./.factory/skills/local-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/v4_reinstall_clears_only_provider_local_exclude",
		Desc: "Given a v4 default-selected repo skill removed into a local source exclusion, When configured install reinstalls it, Then only that provider's exclusion is cleared and sibling exclusions remain",
		Registries: []RegistryDef{
			{Name: "v4-reinstall-main", Skills: []Skill{{Name: "foo"}, {Name: "keep"}}},
			{Name: "v4-reinstall-other", Skills: []Skill{{Name: "other"}}},
		},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  main: {git: \"{{REG:v4-reinstall-main}}\", ref: main}",
			"resources:",
			"  main: {all: true}",
		),
		LocalCfg: v4(
			"sources:",
			"  other: {git: \"{{REG:v4-reinstall-other}}\", ref: main}",
			"resources:",
			"  other: {exclude: [skill/foo]}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/foo/SKILL.md", "./.factory/skills/keep/SKILL.md"),
			ok("remove", "foo").exists("./.factory/skills/keep/SKILL.md").absent("./.factory/skills/foo").has("./.agents/config.local.yaml", "skill/foo"),
			ok("install", "foo").exists("./.factory/skills/foo/SKILL.md", "./.factory/skills/keep/SKILL.md").has("./.agents/config.local.yaml", "skill/foo"),
			ok("sync").exists("./.factory/skills/foo/SKILL.md", "./.factory/skills/keep/SKILL.md").unchanged("./.agents/config.local.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "install/from_named_reinstall_clears_legacy_local_exclude",
		Desc:       "Given a default-selected skill disabled by a provider-scoped v4 local exclude, When named ad-hoc install reinstalls it, Then it materializes and only that exclude is cleared",
		Registries: []RegistryDef{{Name: "adhoc-reinstall-named", Skills: []Skill{{Name: "foo"}, {Name: "keep"}}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:adhoc-reinstall-named}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		LocalCfg: v4(
			"sources: {team: {git: '{{REG:adhoc-reinstall-named}}', ref: main}}",
			"resources: {team: {exclude: [skill/foo, skill/keep]}}",
		),
		Steps: []Step{
			ok("sync").absent("./.factory/skills/foo", "./.factory/skills/keep"),
			ok("install", "foo", "--from", "{{REG:adhoc-reinstall-named}}", "--ref", "main").
				exists("./.factory/skills/foo/SKILL.md").
				absent("./.factory/skills/keep").
				has("./.agents/config.local.yaml", "keep").
				lacks("./.agents/config.local.yaml", "foo"),
			ok("sync").exists("./.factory/skills/foo/SKILL.md").unchanged("./.agents/config.local.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/from_inferred_reinstall_clears_provider_local_exclude",
		Desc: "Given a v4 default-selected nested skill removed into a provider-scoped local exclude, When inferred-name ad-hoc install reinstalls it, Then only its provider exclusion is cleared",
		Registries: []RegistryDef{
			{Name: "adhoc-reinstall-inferred", Skills: []Skill{{Name: "foo"}}},
			{Name: "adhoc-reinstall-other", Skills: []Skill{{Name: "other"}}},
		},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  main: {git: \"{{REG:adhoc-reinstall-inferred}}\", ref: main, path: skills/foo}",
			"resources:",
			"  main: {all: true}",
		),
		LocalCfg: v4(
			"sources:",
			"  other: {git: \"{{REG:adhoc-reinstall-other}}\", ref: main}",
			"resources:",
			"  other: {exclude: [skill/foo]}",
		),
		Steps: []Step{
			ok("sync").exists("./.factory/skills/foo/SKILL.md"),
			ok("remove", "foo").absent("./.factory/skills/foo"),
			ok("install", "--from", "{{REG:adhoc-reinstall-inferred}}", "--ref", "main", "--path", "skills/foo").
				exists("./.factory/skills/foo/SKILL.md").
				has("./.agents/config.local.yaml", "skill/foo"),
			ok("sync").exists("./.factory/skills/foo/SKILL.md").unchanged("./.agents/config.local.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "install/reinstall_preserves_default_auto_select",
		Desc: "Given an all:true registry with one skill excluded, When that skill is re-installed, Then the exclude is dropped WITHOUT adding an explicit include so default auto-select keeps every other skill enabled",
		Registries: []RegistryDef{
			{Name: "reinstall-default", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "cohort-analysis"},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:reinstall-default}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			// all:true auto-selects every skill.
			ok("sync").exists("~/.factory/skills/keep-skill/SKILL.md", "~/.factory/skills/cohort-analysis/SKILL.md"),
			// Remove one skill → recorded as an exclude; others remain.
			ok("remove", "--scope", "user", "cohort-analysis"),
			ok("sync").exists("~/.factory/skills/keep-skill/SKILL.md").absent("~/.factory/skills/cohort-analysis"),
			// Re-install the excluded skill. This must simply drop the exclude
			// and keep all:true — NOT add skills.include (which would flip
			// the entry out of auto-select-all and silently deselect keep-skill).
			ok("install", "--scope", "user", "cohort-analysis").has("~/.agents/config.yaml", "all: true").lacks("~/.agents/config.yaml", "include"),
			// Both skills materialize again: proof that default auto-select
			// survived the re-install instead of collapsing to just one skill.
			ok("sync").exists("~/.factory/skills/keep-skill/SKILL.md", "~/.factory/skills/cohort-analysis/SKILL.md"),
		},
	})

	// spec: resource-installation/bulk-selectors-install-into-each-writable-layer
	for _, scope := range []string{"user", "local"} {
		for _, selector := range []string{"tag", "all"} {
			for _, sourceScope := range []string{"repo", "user"} {
				globalCfg := v4("harnesses: [factory]")
				repoCfg := v4("sources: {team: {git: '{{REG:bulk-scopes}}', ref: main}}")
				unchanged := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}
				if sourceScope == "user" {
					globalCfg += "sources: {team: {git: '{{REG:bulk-scopes}}', ref: main}}\n"
					repoCfg, unchanged = "", nil
					if scope == "local" {
						unchanged = []string{"/root/.agents/config.yaml", "/root/.agents/lock.yaml"}
					}
				}
				targetConfig, targetLock, outputRoot, otherRoot, label := "/root/.agents/config.yaml", "/root/.agents/lock.yaml", "/root", "/workspace", "user-wide"
				if scope == "local" {
					targetConfig, targetLock, outputRoot, otherRoot, label = "/workspace/.agents/config.local.yaml", "/workspace/.agents/lock.local.yaml", "/workspace", "/root", "locally"
				}
				args := []string{"install", "--scope", scope}
				selection := "ops"
				absent := []string{otherRoot + "/.factory/skills/review"}
				if sourceScope == "user" {
					absent = append(absent, "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml")
				}
				if selector == "tag" {
					args = append(args, "--tag", "ops")
					absent = append(absent, outputRoot+"/.factory/skills/unselected")
				} else {
					args = append(args, "--all", "--kind", "skill", "--registry", "{{REG:bulk-scopes}}")
					selection = "skill/review"
				}
				testTable = append(testTable, TestCase{
					Name: "install/bulk_scope_" + scope + "_" + selector + "_from_" + sourceScope,
					Registries: []RegistryDef{{Name: "bulk-scopes", Skills: []Skill{
						{Name: "review", Description: "Scoped review", Tags: []string{"ops"}},
						{Name: "unselected", Description: "Untagged skill"},
					}}},
					GlobalCfg: globalCfg,
					RepoCfg:   repoCfg,
					Steps: []Step{
						ok("lock", "--scope", sourceScope),
						ok(args...).
							out(label).
							absent(absent...).
							unchanged(unchanged...).
							has(targetConfig, selection).
							has(targetLock, "scope: "+scope).
							has(outputRoot+"/.factory/skills/review/SKILL.md", "Scoped review"),
						ok("sync", "--check"),
					},
				})
			}
		}
	}

	// spec: resource-installation/bulk-local-selection-honors-the-current-subdirectory
	testTable = append(testTable, TestCase{
		Name: "install/bulk_local_selection_from_subdirectory",
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {directory: ./resources}}",
			"resources: {team: {include: [skill/parent-only]}}",
		),
		Steps: []Step{
			writeFile("./resources/skills/local-probe/SKILL.md", "---\nname: local-probe\ndescription: Local probe\n---\nParent copy\n"),
			writeFile("./resources/skills/parent-only/SKILL.md", "---\nname: parent-only\ndescription: Parent selection\n---\nParent copy\n"),
			writeFile("./packages/app/.gitkeep", ""),
			ok("sync").exists("./.factory/skills/parent-only/SKILL.md").absent("./.factory/skills/local-probe"),
			ok("install", "--all", "--scope", "local").
				in("./packages/app").
				out("local-probe").
				exists("./packages/app/.agents/config.local.yaml", "./packages/app/.factory/skills/local-probe/SKILL.md", "./packages/app/.factory/skills/parent-only/SKILL.md").
				unchanged("./.agents/config.yaml", "./.factory/skills/parent-only/SKILL.md").
				has("./packages/app/.agents/config.local.yaml", "local-probe"),
		},
	})
}
