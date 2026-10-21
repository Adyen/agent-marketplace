package e2e_test

import "testing"

func init() {
	// spec: status-reporting/synced-marketplace-selection-appears-in-synced-resources
	testTable = append(testTable, TestCase{
		Name:         "marketplace/install_sync_status",
		Desc:         "Given a v4 repository Marketplace selection, When the skill is installed and synchronized, Then status lists it as synced and agrees with sync --check",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "selected", Description: "Selected Marketplace skill"})}},
		GlobalCfg:    v4Cfg("factory"),
		RepoCfg:      v4Cfg("factory") + "sources: {hub: {marketplace: '{{MKT:hub}}'}}\n",
		Steps: []Step{
			{Args: []string{"install", "hub:hub/skill/selected", "--yes"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/selected/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"SYNCED RESOURCES", "selected"}, OutNotContains: []string{"orphan", "pending removal"}},
		},
	})

	// spec: logging/environment-selects-the-log-file-and-debug-level
	testTable = append(testTable, TestCase{
		Name:       "logging/environment_selects_log_file_and_debug_level",
		Desc:       "Given a configured log file, When lock runs with environment overrides for the file and debug level, Then debug output uses the environment-selected file",
		Registries: []RegistryDef{{Name: "env-log", Skills: []Skill{{Name: "log-skill"}}}},
		GlobalCfg:  v4Cfg("factory") + "sources: {team: {git: '{{REG:env-log}}', ref: main}}\nresources: {team: {include: [skill/log-skill]}}\nlogging: {file: '$HOME/logs/configured.log'}\n",
		Steps: []Step{
			{
				Args:         []string{"lock"},
				Env:          map[string]string{"AGENT_MARKETPLACE_LOG_FILE": "$HOME/logs/environment.log", "AGENT_MARKETPLACE_DEBUG": "1"},
				MustPass:     true,
				FilesExist:   []string{"/root/logs/environment.log"},
				FilesAbsent:  []string{"/root/logs/configured.log"},
				FileContains: map[string]string{"/root/logs/environment.log": `"level":"DEBUG"`},
			},
		},
	})

	// spec: resource-installation/bulk-local-selection-honors-the-current-subdirectory
	testTable = append(testTable, TestCase{
		Name:    "install/bulk_local_selection_from_subdirectory",
		Desc:    "Given a repository configuration with a relative directory source, When bulk installation targets local scope from a nested directory, Then the selection and output stay in that directory without changing the parent installation",
		RepoCfg: v4Cfg("factory") + "sources: {team: {directory: ./resources}}\nresources: {team: {include: [skill/parent-only]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/local-probe/SKILL.md", Body: "---\nname: local-probe\ndescription: Local probe\n---\nParent copy\n"},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/parent-only/SKILL.md", Body: "---\nname: parent-only\ndescription: Parent selection\n---\nParent copy\n"},
			{Action: ActionWriteFile, Path: "/workspace/packages/app/.gitkeep", Body: ""},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/parent-only/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/local-probe"}},
			{
				Args:           []string{"install", "--all", "--scope", "local"},
				Cwd:            "/workspace/packages/app",
				MustPass:       true,
				OutContains:    []string{"local-probe"},
				PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.factory/skills/parent-only/SKILL.md"},
				FilesExist:     []string{"/workspace/packages/app/.agents/config.local.yaml", "/workspace/packages/app/.factory/skills/local-probe/SKILL.md", "/workspace/packages/app/.factory/skills/parent-only/SKILL.md"},
				FileContains:   map[string]string{"/workspace/packages/app/.agents/config.local.yaml": "local-probe"},
			},
		},
	})

	// spec: update-output/default-organization-pin-output-omits-owner-guidance
	testTable = append(testTable, TestCase{
		Name: "marketplace/push_reindex_updates_consumer",
		Desc: "Given repository and organization Marketplace selections, When the source is pushed, reindexed and updated, Then repository output advances while organization pins stay unchanged and update output omits owner guidance",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(
			Skill{Name: "current", Description: "User-managed pin"},
			Skill{Name: "organization", Description: "Organization-managed pin"},
			Skill{Name: "git-http", Description: "Git-over-HTTP source"},
		)}},
		GlobalCfg: v4Cfg("factory"),
		RepoCfg: v4Cfg("factory") + `sources:
  hub: {marketplace: '{{MKT:hub}}'}
  git-origin: {git: '{{GITHTTP:hub}}', ref: main}
resources:
  git-origin: {include: [skill/git-http]}
`,
		OrgCfg: v4Cfg("factory") + `sources:
  organization: {marketplace: '{{MKT:hub}}'}
resources:
  organization: {include: [hub/skill/organization]}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/git-http/SKILL.md": "# git-http", "/root/.factory/skills/organization/SKILL.md": "# organization"}},
			{Args: []string{"install", "hub:hub/skill/current", "--yes"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/current/SKILL.md": "# current"}},
			{
				Action: ActionPushRegistry,
				Target: "hub",
				Files: map[string]string{
					"skills/current/SKILL.md":      "---\nname: current\ndescription: User-managed pin\nlicense: MIT\n---\nRepository update\n",
					"skills/organization/SKILL.md": "---\nname: organization\ndescription: Organization-managed pin\nlicense: MIT\n---\nOrganization update\n",
				},
			},
			{Action: ActionReindexMarketplace, Target: "hub"},
			{
				Args:     []string{"update"},
				MustPass: true,
				OutContains: []string{
					"Organization-managed Marketplace pins: 1 (not checked for newer revisions; pins unchanged)",
					"Use `agent-marketplace update --verbose` to list organization-managed pins.",
				},
				OutNotContains: []string{"Ask your organization config owner"},
				FileContains: map[string]string{
					"/workspace/.factory/skills/current/SKILL.md": "Repository update",
				},
				PathsUnchanged: []string{"/etc/agent-marketplace/config.yaml"},
				FilesAbsent:    []string{"/etc/agent-marketplace/lock.yaml"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	})

	// spec: resource-removal/remove-a-marketplace-pin-by-its-endpoint
	testTable = append(testTable, TestCase{
		Name:         "marketplace/remove_pin_by_endpoint_after_ambiguity",
		Desc:         "Given a repository Marketplace pin and an undiscoverable same-name directory selection, When removal is ambiguous and the suggested endpoint removal runs, Then only the pin and its lock record are removed",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "shared", Description: "Marketplace pin"})}},
		GlobalCfg:    v4Cfg("factory"),
		RepoCfg: v4Cfg("factory") + `sources:
  hub: {marketplace: '{{MKT:hub}}'}
  missing: {directory: ./missing, source_harness: factory}
resources:
  missing: {include: [skill/shared, skill/keep]}
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/missing/skills/keep/SKILL.md", Body: "---\nname: keep\ndescription: Healthy sibling\n---\nKeep selected\n"},
			{
				Args:            []string{"sync"},
				MustPass:        true,
				OutContains:     []string{`skill "shared" was selected but not found`},
				FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "hub/skill/shared", "/workspace/.agents/lock.yaml": "name: shared"},
				FilesAbsent:     []string{"/workspace/.factory/skills/shared/SKILL.md"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{
				Args:           []string{"install", "hub:hub/skill/shared", "--yes"},
				MustPass:       true,
				FileContains:   map[string]string{"/workspace/.agents/config.yaml": "hub/skill/shared", "/workspace/.agents/lock.yaml": "name: shared", "/workspace/.factory/skills/shared/SKILL.md": "Marketplace pin"},
				PathsUnchanged: []string{"/root/.agents/config.yaml"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/shared/SKILL.md"}},
			{
				Args:           []string{"remove", "shared"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{`--scope repo --from "{{MKT:hub}}"   (REPO)`},
				PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.factory/skills/shared/SKILL.md"},
			},
			{
				Args:         []string{"remove", "shared", "--scope", "repo", "--from", "{{MKT:hub}}"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.agents/config.yaml": "skill/shared", "/workspace/.agents/lock.yaml": "instance_id: hub-marketplace"},
				FileNotContains: map[string]string{
					"/workspace/.agents/config.yaml": "hub/skill/shared",
					"/workspace/.agents/lock.yaml":   "name: shared",
				},
				FilesAbsent:    []string{"/workspace/.factory/skills/shared/SKILL.md", "/etc/agent-marketplace/config.yaml", "/etc/agent-marketplace/lock.yaml"},
				PathsUnchanged: []string{"/root/.agents/config.yaml"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/shared/SKILL.md"}},
		},
	})

	// spec: resource-listing/selected-marketplace-collection-listed-enabled
	testTable = append(testTable, TestCase{
		Name: "marketplace/install_list_remove_collection",
		Desc: "Given a Marketplace collection, When it is installed and listed before removal, Then it is enabled and its members are materialized and later pruned",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: RegistryDef{Files: map[string]string{
			"skills/member-skill/SKILL.md":            "---\nname: member-skill\ndescription: Collection skill\nlicense: MIT\n---\nCollection skill body\n",
			"commands/member-command.md":              "---\nname: member-command\ndescription: Collection command\n---\necho collection\n",
			"collections/team-bundle/COLLECTION.yaml": "name: team-bundle\nversion: \"1.0\"\ndescription: Marketplace collection\nresources:\n  - name: member-skill\n    kind: skill\n  - name: member-command\n    kind: command\n",
		}}}},
		GlobalCfg: v4Cfg("factory"),
		RepoCfg:   v4Cfg("factory") + "sources: {hub: {marketplace: '{{MKT:hub}}'}}\n",
		Steps: []Step{
			{
				Args:         []string{"install", "hub:hub/collection/team-bundle", "--yes"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.agents/config.yaml": "hub/collection/team-bundle", "/workspace/.factory/skills/member-skill/SKILL.md": "Collection skill body"},
				FilesExist:   []string{"/workspace/.factory/commands/member-command.md"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"list", "--kind", "collection"}, MustPass: true, OutContains: []string{"team-bundle", "enabled"}, OutNotContains: []string{"available"}},
			{
				Args:            []string{"remove", "team-bundle", "--kind", "collection"},
				MustPass:        true,
				FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "hub/collection/team-bundle"},
				FilesAbsent:     []string{"/workspace/.factory/skills/member-skill", "/workspace/.factory/commands/member-command.md"},
			},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	})

	// spec: deployed-acquisition-locks/existing-lock-gains-only-the-missing-registration-identity
	testTable = append(testTable, TestCase{
		Name: "marketplace/append_resource_free_registration_identity",
		Desc: "Given a repository Marketplace pin and its exact lock, When a second resource-free Marketplace registration is added and locked, Then only the new identity is added and later reads preserve lock bytes",
		Marketplaces: []MarketplaceDef{
			{Name: "first", Registry: tableMarketplaceRegistry(Skill{Name: "pinned", Description: "Original locked Marketplace skill"})},
			{Name: "second", Registry: tableMarketplaceRegistry(Skill{Name: "unselected", Description: "Unselected Marketplace skill"})},
		},
		GlobalCfg: v4Cfg("factory"),
		RepoCfg: v4Cfg("factory") + `sources:
  first: {marketplace: '{{MKT:first}}'}
resources:
  first: {include: [first/skill/pinned]}
`,
		Steps: []Step{
			{
				Args:         []string{"sync"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.agents/lock.yaml": "instance_id: first-marketplace"},
			},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "name: pinned"}},
			{
				Action: ActionWriteRepoConfig,
				Body: v4Cfg("factory") + `sources:
  first: {marketplace: '{{MKT:first}}'}
  second: {marketplace: '{{MKT:second}}'}
resources:
  first: {include: [first/skill/pinned]}
`,
				Args:           []string{"lock"},
				MustPass:       true,
				FileContains:   map[string]string{"/workspace/.agents/lock.yaml": "instance_id: first-marketplace"},
				PathsUnchanged: []string{"/workspace/.agents/config.yaml"},
			},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "instance_id: second-marketplace"}},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "name: pinned"}},
			{FileNotContains: map[string]string{"/workspace/.agents/lock.yaml": "name: unselected"}},
			{Args: []string{"lock"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/lock.yaml", "/workspace/.agents/config.yaml"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/lock.yaml", "/workspace/.agents/config.yaml"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/lock.yaml", "/workspace/.agents/config.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/lock.yaml", "/workspace/.agents/config.yaml"}},
		},
	})

	// spec: update-output/missing-resource-is-kept-while-other-pins-update
	// spec: update-output/lock-and-outputs-agree-after-a-partial-update
	testTable = append(testTable, TestCase{
		Name: "marketplace/update_other_pin_when_coordinate_disappears",
		Desc: "Given repo and local Marketplace pins, When one upstream pin changes and the other resource is renamed off its pinned coordinate, Then update advances the available pin and preserves the missing pin lock",
		Marketplaces: []MarketplaceDef{
			{Name: "changed-hub", Registry: tableMarketplaceRegistry(Skill{Name: "current", Description: "Current pin"})},
			{Name: "gone-hub", Registry: tableMarketplaceRegistry(Skill{Name: "gone", Description: "Missing pin"}, Skill{Name: "survivor", Description: "Keep registry indexed"})},
		},
		GlobalCfg: v4Cfg("factory"),
		RepoCfg:   v4Cfg("factory") + "sources: {changed: {marketplace: '{{MKT:changed-hub}}'}}\n",
		LocalCfg:  v4Cfg() + "sources: {gone: {marketplace: '{{MKT:gone-hub}}'}}\n",
		Steps: []Step{
			{
				Args:         []string{"install", "changed:changed-hub/skill/current", "--scope", "repo", "--yes"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.factory/skills/current/SKILL.md": "# current"},
			},
			{
				Args:         []string{"install", "gone:gone-hub/skill/gone", "--scope", "local", "--yes"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.factory/skills/gone/SKILL.md": "# gone", "/workspace/.agents/lock.local.yaml": "name: gone"},
			},
			{
				Action: ActionPushRegistry,
				Target: "changed-hub",
				Files: map[string]string{
					"skills/current/SKILL.md": "---\nname: current\ndescription: Changed current pin\nlicense: MIT\n---\nChanged pin content\n",
				},
			},
			{
				Action: ActionPushRegistry,
				Target: "gone-hub",
				Files: map[string]string{
					"skills/gone/SKILL.md": "---\nname: renamed\ndescription: Renamed resource\nlicense: MIT\n---\nRenamed resource content\n",
				},
			},
			{Action: ActionReindexMarketplace, Target: "changed-hub"},
			{Action: ActionReindexMarketplace, Target: "gone-hub"},
			{
				Args:           []string{"sync"},
				MustPass:       true,
				FileContains:   map[string]string{"/workspace/.factory/skills/current/SKILL.md": "# current", "/workspace/.factory/skills/gone/SKILL.md": "# gone"},
				PathsUnchanged: []string{"/workspace/.agents/lock.local.yaml"},
			},
			{
				Args:           []string{"update"},
				AssertExitCode: intPtr(1),
				OutContains: []string{
					"Marketplace pins updated: 1",
					"{{MKT:gone-hub}}",
					"gone-hub-id/skill/gone",
					"agent-marketplace remove gone",
					"install the new name",
				},
				FileContains: map[string]string{
					"/workspace/.factory/skills/current/SKILL.md": "Changed pin content",
					"/workspace/.factory/skills/gone/SKILL.md":    "# gone",
					"/workspace/.agents/lock.local.yaml":          "name: gone",
					"/workspace/.agents/lock.yaml":                "name: current",
				},
				FileNotContains: map[string]string{
					"/workspace/.factory/skills/current/SKILL.md": "Current pin",
				},
				PathsUnchanged: []string{"/workspace/.agents/lock.local.yaml", "/workspace/.agents/config.local.yaml", "/workspace/.agents/config.yaml"},
			},
			{
				Args:           []string{"sync", "--check"},
				MustPass:       true,
				FileContains:   map[string]string{"/workspace/.factory/skills/current/SKILL.md": "Changed pin content", "/workspace/.factory/skills/gone/SKILL.md": "# gone"},
				PathsUnchanged: []string{"/workspace/.agents/lock.local.yaml"},
			},
		},
	})

	// spec: status-reporting/organization-collection-members-are-not-orphaned-without-an-organization-lock
	testTable = append(testTable, TestCase{
		Name: "marketplace/organization_collection_status_without_lock",
		Desc: "Given an organization Marketplace collection without an organization lock, When its members are synchronized and status runs, Then the members are not orphaned, no organization lock is created, and user and repository configuration are unchanged",
		Marketplaces: []MarketplaceDef{{Name: "org-hub", Registry: RegistryDef{Files: map[string]string{
			"skills/org-member/SKILL.md":             "---\nname: org-member\ndescription: Organization collection member\nlicense: MIT\n---\nOrganization member body\n",
			"collections/org-bundle/COLLECTION.yaml": "name: org-bundle\nversion: \"1.0\"\ndescription: Organization collection\nresources:\n  - name: org-member\n    kind: skill\n",
		}}}},
		GlobalCfg: v4Cfg("factory"),
		RepoCfg:   v4Cfg("factory"),
		OrgCfg: v4Cfg("factory") + `sources:
  organization: {marketplace: '{{MKT:org-hub}}'}
resources:
  organization: {include: [org-hub/collection/org-bundle]}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/root/.factory/skills/org-member/SKILL.md": "Organization member body"}, FilesAbsent: []string{"/etc/agent-marketplace/lock.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FilesAbsent: []string{"/etc/agent-marketplace/lock.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"org-member"}, OutNotContains: []string{"orphan", "pending removal"}, PathsUnchanged: []string{"/root/.agents/config.yaml", "/workspace/.agents/config.yaml", "/etc/agent-marketplace/config.yaml"}, FilesAbsent: []string{"/etc/agent-marketplace/lock.yaml"}},
		},
	})

	// spec: deployed-acquisition-locks/git-only-lock-gains-marketplace-evidence
	testTable = append(testTable, TestCase{
		Name:         "marketplace/git_lock_gains_marketplace_evidence",
		Desc:         "Given a repository Git-only lock, When a Marketplace skill is selected in the same layer and sync runs, Then the lock retains Git records and adds Marketplace evidence",
		Registries:   []RegistryDef{{Name: "git-only", Skills: []Skill{{Name: "git-skill", Description: "Existing Git record"}}}},
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "marketplace-skill", Description: "Marketplace evidence"})}},
		GlobalCfg:    v4Cfg("factory"),
		RepoCfg: v4Cfg("factory") + `sources:
  git: {git: '{{GITHTTP:git-only}}', ref: main}
resources:
  git: {include: [skill/git-skill]}
`,
		Steps: []Step{
			{
				Args:         []string{"sync"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.agents/lock.yaml": "git-only"},
				FileNotContains: map[string]string{
					"/workspace/.agents/lock.yaml": "instance_id: hub-marketplace",
				},
			},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "ref: main"}},
			{
				Action: ActionWriteRepoConfig,
				Body: v4Cfg("factory") + `sources:
  git: {git: '{{GITHTTP:git-only}}', ref: main}
  hub: {marketplace: '{{MKT:hub}}'}
resources:
  git: {include: [skill/git-skill]}
  hub: {include: [hub/skill/marketplace-skill]}
`,
				Args:         []string{"sync"},
				MustPass:     true,
				FileContains: map[string]string{"/workspace/.agents/lock.yaml": "instance_id: hub-marketplace"},
				FilesExist:   []string{"/workspace/.factory/skills/git-skill/SKILL.md", "/workspace/.factory/skills/marketplace-skill/SKILL.md"},
			},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "git-only"}},
			{FileContains: map[string]string{"/workspace/.agents/lock.yaml": "name: marketplace-skill"}},
		},
	})
}

func TestE2ETableRevokeMarketplace(t *testing.T) {
	var firstErr string
	runTableCase(t, TestCase{
		Name:         "marketplace/revoke_preserves_consumer_output",
		Marketplaces: []MarketplaceDef{{Name: "revoke", Registry: tableMarketplaceRegistry(Skill{Name: "locked", Description: "Revocable skill"})}},
		GlobalCfg:    v4Cfg("factory"),
		RepoCfg:      v4Cfg("factory") + "sources: {revoke: {marketplace: '{{MKT:revoke}}'}}\n",
		Steps: []Step{
			{Args: []string{"install", "revoke:revoke/skill/locked", "--yes"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/locked/SKILL.md": "Revocable skill"}},
			{Action: ActionRevokeMarketplace, Target: "revoke", Revoke: MarketplaceRevocations{Coordinates: []string{"revoke-marketplace/revoke-id/skill/locked"}}},
			{
				Args:           []string{"sync"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{"revoked"},
				PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.factory/skills/locked/SKILL.md"},
				FileContains:   map[string]string{"/workspace/.factory/skills/locked/SKILL.md": "Revocable skill"},
			},
		},
	}, &firstErr)
}
