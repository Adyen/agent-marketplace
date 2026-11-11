package e2e_test

func init() {
	// ===== MCP server parity =====
	testTable = append(testTable, TestCase{
		Name: "mcp/multi_server_merge",
		Desc: "Given a registry with two MCP servers explicitly selected, When sync is run, Then mcp.json contains both server entries merged into a single file",
		Registries: []RegistryDef{
			{Name: "mcp-multi", MCPServers: []MCPServer{
				{Name: "server-a", Command: "npx", Args: []string{"-y", "@a/server"}},
				{Name: "server-b", Command: "node", Args: []string{"server-b.js"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-multi}}', ref: main}}",
			"resources: {team: {include: [mcp/server-a, mcp/server-b]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "server-a"),
			// Verify second server in same file against the same post-sync state.
			Step{}.has("~/.factory/mcp.json", "server-b"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/multi_registry_merge",
		Desc: "Given two registries each providing an MCP server, When both are selected in config, Then mcp.json contains both servers without clobbering",
		Registries: []RegistryDef{
			{Name: "mcp-reg-a", MCPServers: []MCPServer{
				{Name: "server-alpha", Command: "npx", Args: []string{"-y", "@alpha/server"}},
			}},
			{Name: "mcp-reg-b", MCPServers: []MCPServer{
				{Name: "server-beta", Command: "node", Args: []string{"beta.js"}},
			}},
		},
		GlobalCfg: v4(
			"sources:",
			"  first: {git: '{{REG:mcp-reg-a}}', ref: main}",
			"  second: {git: '{{REG:mcp-reg-b}}', ref: main}",
			"resources:",
			"  first: {include: [mcp/server-alpha]}",
			"  second: {include: [mcp/server-beta]}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "server-alpha"),
			ok("sync").has("~/.factory/mcp.json", "server-beta"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/preserve_existing_entries",
		Desc: "Given a pre-existing user-authored mcp.json entry, When sync adds a managed MCP server, Then the pre-existing entry is preserved alongside the managed one",
		Registries: []RegistryDef{
			{Name: "mcp-preserve", MCPServers: []MCPServer{
				{Name: "managed-server", Command: "npx", Args: []string{"-y", "@my/mcp-server"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-preserve}}', ref: main}}",
			"resources: {team: {include: [mcp/managed-server]}}",
		),
		Steps: []Step{
			// Pre-seed mcp.json with a user-authored server.
			writeFile("~/.factory/mcp.json", `{"mcpServers":{"user-server":{"command":"node","args":["user-server.js"]}}}`),
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "managed-server"),
			// User entry must survive the sync.
			ok("sync").has("~/.factory/mcp.json", "user-server"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/args_env_roundtrip",
		Desc: "Given an MCP server with args and env, When sync is run, Then mcp.json contains the exact args and env values",
		Registries: []RegistryDef{
			{Name: "mcp-roundtrip", MCPServers: []MCPServer{
				{Name: "env-server", Command: "npx", Args: []string{"-y", "@my/env-server"}, Env: map[string]string{
					"API_KEY":    "test-value-placeholder",
					"DEBUG_MODE": "true",
				}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-roundtrip}}', ref: main}}",
			"resources: {team: {include: [mcp/env-server]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "env-server"),
			ok("sync").has("~/.factory/mcp.json", "test-value-placeholder"),
			ok("sync").has("~/.factory/mcp.json", "@my/env-server"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/install_kind_mcp",
		Desc: "Given a registry with a skill and an MCP server sharing no name conflict, When install --kind mcp --scope user is run, Then the MCP server is installed and the skill is not",
		Registries: []RegistryDef{
			{Name: "mcp-kind-filter", Skills: []Skill{
				{Name: "not-this-skill"},
			}, MCPServers: []MCPServer{
				{Name: "only-this-mcp", Command: "npx", Args: []string{"-y", "@my/only-mcp"}},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:mcp-kind-filter}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--kind", "mcp", "--scope", "user", "only-this-mcp").out("only-this-mcp"),
			ok("sync").exists("~/.factory/mcp.json").absent("~/.factory/skills/not-this-skill"),
		},
	})

	// spec: resource-prerequisites/install-finds-an-unmet-prerequisite
	// spec: resource-prerequisites/commands-succeed-with-unmet-prerequisites
	testTable = append(testTable, TestCase{
		Name: "mcp/install_skill_prerequisite",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite",
				Skills: []Skill{
					{
						Name:        "smoke-test-kb",
						Description: "Answer smoke test questions from Confluence",
						Requires:    []string{"mcp:genai-atlassian"},
					},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite}}', ref: main}}",
		),
		Steps: []Step{
			ok("install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite}}", "--ref", "main", "--scope", "user").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`).
				exists("~/.factory/skills/smoke-test-kb/SKILL.md").
				absent("~/.factory/mcp.json").
				lacks("~/.agents/config.yaml", "genai-atlassian"),
		},
	})

	// spec: resource-prerequisites/prerequisite-is-explicitly-excluded
	testTable = append(testTable, TestCase{
		Name: "mcp/install_skill_preserves_excluded_prerequisite",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-excluded",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite-excluded}}', ref: main}}",
			"resources: {team: {exclude: [mcp/genai-atlassian]}}",
		),
		Steps: []Step{
			ok("install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite-excluded}}", "--ref", "main", "--scope", "user").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`).
				exists("~/.factory/skills/smoke-test-kb/SKILL.md").
				absent("~/.factory/mcp.json").
				has("~/.agents/config.yaml", "genai-atlassian"),
		},
	})

	// spec: resource-prerequisites/sync-reports-without-mutation
	// spec: resource-prerequisites/status-and-doctor-diagnostics
	// spec: resource-prerequisites/commands-succeed-with-unmet-prerequisites
	// spec: resource-prerequisites/strict-sync-rejects-unmet-prerequisites
	testTable = append(testTable, TestCase{
		Name: "mcp/sync_reports_unmet_prerequisite",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-unselected",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite-unselected}}', ref: main}}",
			"resources: {team: {include: [skill/smoke-test-kb]}}",
		),
		Steps: []Step{
			ok("sync").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`).
				exists("~/.factory/skills/smoke-test-kb/SKILL.md").
				absent("~/.factory/mcp.json"),
			ok("status").out("UNMET REQUIREMENTS", "skill:smoke-test-kb", "mcp:genai-atlassian", `agent-marketplace install genai-atlassian --kind mcp`),
			ok("sync", "--check").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`),
			exits(1, "sync", "--strict").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`),
			exits(1, "sync", "--strict", "--check").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`),
			ok("doctor").
				out(`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`),
		},
	})

	// spec: resource-prerequisites/strict-sync-succeeds-when-satisfied
	testTable = append(testTable, TestCase{
		Name: "mcp/sync_strict_succeeds_when_prerequisite_selected",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-satisfied",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite-satisfied}}', ref: main}}",
			"resources: {team: {include: [skill/smoke-test-kb, mcp/genai-atlassian]}}",
		),
		Steps: []Step{
			ok("sync", "--strict").notOut("but it is not selected").exists("~/.factory/skills/smoke-test-kb/SKILL.md", "~/.factory/mcp.json"),
		},
	})

	// spec: resource-prerequisites/invalid-item-among-valid-items
	// spec: resource-prerequisites/non-list-requires-value
	testTable = append(testTable, TestCase{
		Name: "mcp/invalid_prerequisite_items_warn_in_inspection",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-invalid",
				Files: map[string]string{
					"skills/broken-prereq/SKILL.md": "---\nname: broken-prereq\ndescription: Mixed prerequisites\nmetadata:\n  requires:\n    - mcp:docs-server\n    - kind: all\n      name: everything\n    - kind: 42\n      name: secret-user:secret-token@example.com\n---\nbody\n",
					"skills/scalar-prereq/SKILL.md": "---\nname: scalar-prereq\ndescription: Non-list prerequisites\nmetadata:\n  requires: secret-user:secret-token@example.com\n---\nbody\n",
				},
				Skills: []Skill{{Name: "sibling"}},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite-invalid}}', ref: main}}",
			"resources: {team: {include: [skill/sibling]}}",
		),
		Steps: []Step{
			ok("list", "--from", "{{REG:mcp-prerequisite-invalid}}", "--ref", "main").
				out("catalog.requires.invalid", "broken-prereq", "scalar-prereq", "SKILL.md", "sibling").
				notOut("secret-token", "secret-user"),
			ok("sync").exists("~/.factory/skills/sibling/SKILL.md"),
			ok("doctor").out("catalog.requires.invalid", "broken-prereq", "scalar-prereq", "SKILL.md").notOut("secret-token", "secret-user"),
		},
	})

	// spec: resource-prerequisites/malformed-item-stays-silent
	testTable = append(testTable, TestCase{
		Name: "mcp/sync_malformed_prerequisite_stays_silent",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-malformed",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"weird:thing", `"mcp: "`, "collection:team-bundle"}},
				},
			},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
			"sources: {team: {git: '{{REG:mcp-prerequisite-malformed}}', ref: main}}",
			"resources: {team: {include: [skill/smoke-test-kb]}}",
		),
		Steps: []Step{
			ok("sync").notOut("but it is not selected").exists("~/.factory/skills/smoke-test-kb/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/install_all_kind_mcp",
		Desc: "Given a registry with skills and MCP servers, When install --all --kind mcp is run and synced, Then MCP servers are selected in the registry entry",
		Registries: []RegistryDef{
			{Name: "mcp-all-kind", Skills: []Skill{
				{Name: "not-this-skill"},
			}, MCPServers: []MCPServer{
				{Name: "batch-mcp-a", Command: "npx", Args: []string{"-y", "@a/mcp"}},
				{Name: "batch-mcp-b", Command: "node", Args: []string{"b.js"}},
			}},
		},
		GlobalCfg: v4("sources: {team: {git: '{{REG:mcp-all-kind}}', ref: main}}"),
		Steps: []Step{
			ok("install", "--all", "--kind", "mcp").out("batch-mcp-a"),
			ok("sync"),
			ok("status").out("batch-mcp-a"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/remove_roundtrip",
		Desc: "Given an MCP server synced to machine scope, When remove --scope user is run and re-synced, Then the MCP server is gone from mcp.json",
		Registries: []RegistryDef{
			{Name: "mcp-roundtrip-rm", MCPServers: []MCPServer{
				{Name: "roundtrip-mcp", Command: "npx", Args: []string{"-y", "@rm/mcp"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-roundtrip-rm}}', ref: main}}",
			"resources: {team: {include: [mcp/roundtrip-mcp]}}",
		),
		Steps: []Step{
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "roundtrip-mcp"),
			ok("remove", "--scope", "user", "roundtrip-mcp"),
			ok("sync").
				notOut("roundtrip-mcp").
				exists("~/.factory/mcp.json").
				has("~/.factory/mcp.json", `"mcpServers": {}`).
				lacks("~/.factory/mcp.json", "_managed_by"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/remove_default_true_adds_exclude",
		Desc: "Given all:true auto-selected MCP servers, When remove --scope user is run for one MCP server and re-synced, Then that server is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "mcp-default-rm", MCPServers: []MCPServer{
				{Name: "keep-mcp", Command: "npx", Args: []string{"-y", "@keep/mcp"}},
				{Name: "drop-mcp", Command: "node", Args: []string{"drop.js"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-default-rm}}', ref: main}}",
			"resources: {team: {all: true}}",
		),
		Steps: []Step{
			ok("sync").has("~/.factory/mcp.json", "keep-mcp"),
			ok("sync").has("~/.factory/mcp.json", "drop-mcp"),
			ok("remove", "--scope", "user", "drop-mcp"),
			ok("sync").notOut("drop-mcp").has("~/.factory/mcp.json", "keep-mcp"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/install_from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from and --kind mcp, Then the MCP server appears in list output and sync materializes it",
		Registries: []RegistryDef{
			{Name: "mcp-adhoc", MCPServers: []MCPServer{
				{Name: "adhoc-mcp", Command: "npx", Args: []string{"-y", "@adhoc/mcp"}},
			}},
		},
		GlobalCfg: v4(
			"harnesses:",
			"  - factory",
		),
		Steps: []Step{
			ok("install", "adhoc-mcp", "--from", "{{REG:mcp-adhoc}}", "--ref", "main", "--kind", "mcp", "--scope", "user").out("adhoc-mcp"),
			ok("sync").exists("~/.factory/mcp.json").has("~/.factory/mcp.json", "adhoc-mcp"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/collision_same_name_different_registries",
		Desc: "Given two registries each providing an MCP server named 'shared-mcp', When sync is run, Then a collision warning is emitted",
		Registries: []RegistryDef{
			{Name: "mcp-reg-a", MCPServers: []MCPServer{
				{Name: "shared-mcp", Command: "npx", Args: []string{"-y", "@a/mcp"}},
			}},
			{Name: "mcp-reg-b", MCPServers: []MCPServer{
				{Name: "shared-mcp", Command: "node", Args: []string{"b.js"}},
			}},
		},
		GlobalCfg: v4(
			"sources:",
			"  first: {git: '{{REG:mcp-reg-a}}', ref: main}",
			"  second: {git: '{{REG:mcp-reg-b}}', ref: main}",
			"resources:",
			"  first: {include: [mcp/shared-mcp]}",
			"  second: {include: [mcp/shared-mcp]}",
		),
		Steps: []Step{
			ok("sync").out("collision"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/malformed_degradation",
		Desc: "Given a registry with a malformed MCP file and a valid skill, When sync is run, Then the skill still materializes and the malformed MCP produces an error diagnostic",
		Registries: []RegistryDef{
			{Name: "mcp-malformed",
				Skills: []Skill{{Name: "survivor-skill"}},
				Files: map[string]string{
					// Add Factory plugin marker so Factory ingester handles MCP gracefully.
					".factory-plugin/plugin.json": `{"skills":[{"name":"survivor-skill"}]}`,
					"mcp.json":                    `{"mcpServers":{bad-json`,
				}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-malformed}}', ref: main}}",
			"resources: {team: {include: [skill/survivor-skill]}}",
		),
		Steps: []Step{
			ok("sync").out("Malformed").exists("~/.factory/skills/survivor-skill/SKILL.md"),
		},
	})

	testTable = append(testTable, TestCase{
		Name: "mcp/sync_check_detects_drift",
		Desc: "Given a config with one MCP server, When sync --check is run before any sync, Then it exits 1; after sync exits 0; after overwriting mcp.json exits 1 again",
		Registries: []RegistryDef{
			{Name: "mcp-drift", MCPServers: []MCPServer{
				{Name: "drift-mcp", Command: "npx", Args: []string{"-y", "@drift/mcp"}},
			}},
		},
		GlobalCfg: v4(
			"sources: {team: {git: '{{REG:mcp-drift}}', ref: main}}",
			"resources: {team: {include: [mcp/drift-mcp]}}",
		),
		Steps: []Step{
			exits(1, "sync", "--check"),
			ok("sync"),
			exits(0, "sync", "--check"),
			// Overwrite mcp.json to introduce drift.
			writeFile("~/.factory/mcp.json", `{"mcpServers":{}}`),
			exits(1, "sync", "--check"),
		},
	})
}
