package e2e_test

import (
	"testing"
)

func init() {
	// spec: deployed-acquisition-locks/check-reports-every-selected-unrecorded-registration-scope
	testTable = append(testTable, TestCase{
		Name: "lock/check_all_unrecorded_registration_scopes",
		GlobalCfg: v4(
			"harnesses: [factory]",
			"sources: {hosted: {marketplace: https://unrecorded.invalid}}",
		),
		RepoCfg:  v4("sources: {hosted: {marketplace: https://unrecorded.invalid}}"),
		LocalCfg: v4("sources: {hosted: {marketplace: https://unrecorded.invalid}}"),
		Steps: []Step{
			exits(1, "lock", "--check").
				out(`lock.change-required: user lock "`, `lock.change-required: repo lock "`, `lock.change-required: local lock "`, "https://unrecorded.invalid").
				absent("~/.agents/lock.yaml", "./.agents/lock.yaml", "./.agents/lock.local.yaml").
				unchanged("~/.agents", "./.agents"),
			exits(1, "lock", "--check", "--scope", "repo").
				out(`lock.change-required: repo lock "`).
				notOut(`lock.change-required: user lock "`, `lock.change-required: local lock "`).
				absent("~/.agents/lock.yaml", "./.agents/lock.yaml", "./.agents/lock.local.yaml").
				unchanged("~/.agents", "./.agents"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "locks/unlocked_organization_sync_parity",
		Desc:       "Given an organization registry without a lock, when syncing and checking repeatedly, then resources stay synced without creating an organization lock.",
		Registries: []RegistryDef{{Name: "org-parity", Skills: []Skill{{Name: "review"}}}},
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:org-parity}}', ref: main}}",
			"resources: {team: {all: true, required: true}}",
		),
		Steps: []Step{
			ok("sync").symlinks("~/.factory/skills/review"),
			ok("sync", "--check").unchanged("~/.factory/skills"),
			ok("status").unchanged("~/.factory/skills"),
			ok("sync", "--locked").unchanged("~/.factory/skills"),
			ok("sync", "--offline").unchanged("~/.factory/skills"),
			ok("sync").absent("/etc/agent-marketplace/lock.yaml").unchanged("~/.factory/skills"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "locks/equivalent_auto_explicit_inspection",
		Desc:       "Given equivalent auto and explicit registry roots, When lock check, list and status run in either entry order, Then inspection agrees with reconciliation without changing config or locks",
		Registries: []RegistryDef{{Name: "equivalent-roots", Skills: []Skill{{Name: "review", Description: "Review changes"}}, Files: map[string]string{".claude-plugin/plugin.json": `{"name":"equivalent-roots"}`}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  auto: {git: '{{REG:equivalent-roots}}', ref: main}",
			"  explicit: {git: '{{REG:equivalent-roots}}', ref: main, source_harness: claude}",
		),
		Steps: []Step{
			ok("lock", "--scope", "repo"),
			ok("lock", "--check", "--scope", "repo").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
			ok("list").out("review").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
			ok("status").out("EXACT REGISTRY LOCKS", "available").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
			writeRepoCfg(v4(
				"harnesses: [factory]",
				"sources:",
				"  explicit: {git: '{{REG:equivalent-roots}}', ref: main, source_harness: claude}",
				"  auto: {git: '{{REG:equivalent-roots}}', ref: main}",
			)),
			ok("lock", "--check", "--scope", "repo").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
			ok("list").out("review").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
			ok("status").out("EXACT REGISTRY LOCKS", "available").unchanged("./.agents/config.yaml", "./.agents/lock.yaml"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "update/verbose_registry_summary_repo_only",
		Desc:       "Given an unchanged Git registry selected only by repository config, When updating with verbose output, Then the CLI reports the configured entry without claiming its revision changed",
		Registries: []RegistryDef{{Name: "update-output", Skills: []Skill{{Name: "review"}}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {team: {git: '{{REG:update-output}}', ref: main}}",
			"resources: {team: {include: [skill/review]}}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.factory/skills/review"),
			ok("update", "--verbose").
				out("Configured registry entries: 1", "Marketplace pins updated: 0").
				notOut("Organization-managed Marketplace pins:").
				symlinks("./.factory/skills/review"),
		},
	})

	testTable = append(testTable, TestCase{
		Name:       "update/verbose_registry_summary_user_and_repo",
		Desc:       "Given v4 user config with a harness target and v4 repository config with an unchanged Git source, When updating with verbose output, Then the CLI reports the configured entry without claiming its revision changed",
		Registries: []RegistryDef{{Name: "update-output-v4", Skills: []Skill{{Name: "review"}}}},
		RepoCfg: v4(
			"sources:",
			"  tools: {git: '{{REG:update-output-v4}}', ref: main}",
			"resources:",
			"  tools: {include: [skill/review]}",
		),
		Steps: []Step{
			ok("sync").symlinks("./.factory/skills/review"),
			ok("update", "--verbose").
				out("Configured registry entries: 1", "Marketplace pins updated: 0").
				notOut("Organization-managed Marketplace pins:").
				symlinks("./.factory/skills/review"),
		},
	})

	// spec: status-reporting/synced-marketplace-selection-appears-in-synced-resources
	testTable = append(testTable, TestCase{
		Name:         "marketplace/install_sync_status",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "selected", Description: "Selected Marketplace skill"})}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {hub: {marketplace: '{{MKT:hub}}'}}",
		),
		Steps: []Step{
			ok("install", "hub:hub/skill/selected", "--yes"),
			ok("sync").exists("./.factory/skills/selected/SKILL.md"),
			ok("sync", "--check"),
			ok("status").out("SYNCED RESOURCES", "selected").notOut("orphan", "pending removal"),
		},
	})

	// spec: update-output/default-organization-pin-output-omits-owner-guidance
	testTable = append(testTable, TestCase{
		Name: "marketplace/push_reindex_updates_consumer",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(
			Skill{Name: "current", Description: "User-managed pin"},
			Skill{Name: "organization", Description: "Organization-managed pin"},
			Skill{Name: "git-http", Description: "Git-over-HTTP source"},
		)}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  hub: {marketplace: '{{MKT:hub}}'}",
			"  git-origin: {git: '{{GITHTTP:hub}}', ref: main}",
			"resources:",
			"  git-origin: {include: [skill/git-http]}",
		),
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  organization: {marketplace: '{{MKT:hub}}'}",
			"resources:",
			"  organization: {include: [hub/skill/organization]}",
		),
		Steps: []Step{
			ok("sync").has("./.factory/skills/git-http/SKILL.md", "# git-http").has("~/.factory/skills/organization/SKILL.md", "# organization"),
			ok("install", "hub:hub/skill/current", "--yes").has("./.factory/skills/current/SKILL.md", "# current"),
			push("hub", map[string]string{
				"skills/current/SKILL.md":      "---\nname: current\ndescription: User-managed pin\nlicense: MIT\n---\nRepository update\n",
				"skills/organization/SKILL.md": "---\nname: organization\ndescription: Organization-managed pin\nlicense: MIT\n---\nOrganization update\n",
			}),
			reindex("hub"),
			ok("update").
				out("Organization-managed Marketplace pins: 1 (not checked for newer revisions; pins unchanged)", "Use `agent-marketplace update --verbose` to list organization-managed pins.").
				notOut("Ask your organization config owner").
				absent("/etc/agent-marketplace/lock.yaml").
				unchanged("/etc/agent-marketplace/config.yaml").
				has("./.factory/skills/current/SKILL.md", "Repository update"),
			ok("sync", "--check"),
		},
	})

	// spec: resource-removal/remove-a-marketplace-pin-by-its-endpoint
	testTable = append(testTable, TestCase{
		Name:         "marketplace/remove_pin_by_endpoint_after_ambiguity",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "shared", Description: "Marketplace pin"})}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  hub: {marketplace: '{{MKT:hub}}'}",
			"  missing: {directory: ./missing, source_harness: factory}",
			"resources:",
			"  missing: {include: [skill/shared, skill/keep]}",
		),
		Steps: []Step{
			writeFile("./missing/skills/keep/SKILL.md", "---\nname: keep\ndescription: Healthy sibling\n---\nKeep selected\n"),
			ok("sync").
				out(`skill "shared" was selected but not found`).
				absent("./.factory/skills/shared/SKILL.md").
				lacks("./.agents/config.yaml", "hub/skill/shared").
				lacks("./.agents/lock.yaml", "name: shared"),
			ok("sync", "--check"),
			ok("install", "hub:hub/skill/shared", "--yes").
				unchanged("~/.agents/config.yaml").
				has("./.agents/config.yaml", "hub/skill/shared").
				has("./.agents/lock.yaml", "name: shared").
				has("./.factory/skills/shared/SKILL.md", "Marketplace pin"),
			ok("sync", "--check").exists("./.factory/skills/shared/SKILL.md"),
			exits(1, "remove", "shared").
				out(`--scope repo --from "{{MKT:hub}}"   (REPO)`).
				unchanged("./.agents/config.yaml", "./.agents/lock.yaml", "./.factory/skills/shared/SKILL.md"),
			ok("remove", "shared", "--scope", "repo", "--from", "{{MKT:hub}}").
				absent("./.factory/skills/shared/SKILL.md", "/etc/agent-marketplace/config.yaml", "/etc/agent-marketplace/lock.yaml").
				unchanged("~/.agents/config.yaml").
				has("./.agents/config.yaml", "skill/shared").
				has("./.agents/lock.yaml", "instance_id: hub-marketplace").
				lacks("./.agents/config.yaml", "hub/skill/shared").
				lacks("./.agents/lock.yaml", "name: shared"),
			ok("sync", "--check").absent("./.factory/skills/shared/SKILL.md"),
		},
	})

	// spec: resource-listing/selected-marketplace-collection-listed-enabled
	testTable = append(testTable, TestCase{
		Name: "marketplace/install_list_remove_collection",
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: RegistryDef{Files: map[string]string{
			"skills/member-skill/SKILL.md":            "---\nname: member-skill\ndescription: Collection skill\nlicense: MIT\n---\nCollection skill body\n",
			"commands/member-command.md":              "---\nname: member-command\ndescription: Collection command\n---\necho collection\n",
			"collections/team-bundle/COLLECTION.yaml": "name: team-bundle\nversion: \"1.0\"\ndescription: Marketplace collection\nresources:\n  - name: member-skill\n    kind: skill\n  - name: member-command\n    kind: command\n",
		}}}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {hub: {marketplace: '{{MKT:hub}}'}}",
		),
		Steps: []Step{
			ok("install", "hub:hub/collection/team-bundle", "--yes").
				exists("./.factory/commands/member-command.md").
				has("./.agents/config.yaml", "hub/collection/team-bundle").
				has("./.factory/skills/member-skill/SKILL.md", "Collection skill body"),
			ok("sync", "--check"),
			ok("list", "--kind", "collection").out("team-bundle", "enabled").notOut("available"),
			ok("remove", "team-bundle", "--kind", "collection").
				absent("./.factory/skills/member-skill", "./.factory/commands/member-command.md").
				lacks("./.agents/config.yaml", "hub/collection/team-bundle"),
			ok("sync", "--check"),
		},
	})

	// spec: deployed-acquisition-locks/existing-lock-gains-only-the-missing-registration-identity
	testTable = append(testTable, TestCase{
		Name: "marketplace/append_resource_free_registration_identity",
		Marketplaces: []MarketplaceDef{
			{Name: "first", Registry: tableMarketplaceRegistry(Skill{Name: "pinned", Description: "Original locked Marketplace skill"})},
			{Name: "second", Registry: tableMarketplaceRegistry(Skill{Name: "unselected", Description: "Unselected Marketplace skill"})},
		},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  first: {marketplace: '{{MKT:first}}'}",
			"resources:",
			"  first: {include: [first/skill/pinned]}",
		),
		Steps: []Step{
			ok("sync").has("./.agents/lock.yaml", "instance_id: first-marketplace"),
			Step{}.has("./.agents/lock.yaml", "name: pinned"),
			writeRepoCfg(v4(
				"harnesses: [factory]",
				"sources:",
				"  first: {marketplace: '{{MKT:first}}'}",
				"  second: {marketplace: '{{MKT:second}}'}",
				"resources:",
				"  first: {include: [first/skill/pinned]}",
			)).
				then("lock").
				pass().
				unchanged("./.agents/config.yaml").
				has("./.agents/lock.yaml", "instance_id: first-marketplace"),
			Step{}.has("./.agents/lock.yaml", "instance_id: second-marketplace"),
			Step{}.has("./.agents/lock.yaml", "name: pinned"),
			Step{}.lacks("./.agents/lock.yaml", "name: unselected"),
			ok("lock").unchanged("./.agents/lock.yaml", "./.agents/config.yaml"),
			ok("sync").unchanged("./.agents/lock.yaml", "./.agents/config.yaml"),
			ok("sync", "--locked").unchanged("./.agents/lock.yaml", "./.agents/config.yaml"),
			ok("sync", "--check").unchanged("./.agents/lock.yaml", "./.agents/config.yaml"),
		},
	})

	// spec: update-output/missing-resource-is-kept-while-other-pins-update
	// spec: update-output/lock-and-outputs-agree-after-a-partial-update
	testTable = append(testTable, TestCase{
		Name: "marketplace/update_other_pin_when_coordinate_disappears",
		Marketplaces: []MarketplaceDef{
			{Name: "changed-hub", Registry: tableMarketplaceRegistry(Skill{Name: "current", Description: "Current pin"})},
			{Name: "gone-hub", Registry: tableMarketplaceRegistry(Skill{Name: "gone", Description: "Missing pin"}, Skill{Name: "survivor", Description: "Keep registry indexed"})},
		},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources: {changed: {marketplace: '{{MKT:changed-hub}}'}}",
		),
		LocalCfg: v4("sources: {gone: {marketplace: '{{MKT:gone-hub}}'}}"),
		Steps: []Step{
			ok("install", "changed:changed-hub/skill/current", "--scope", "repo", "--yes").has("./.factory/skills/current/SKILL.md", "# current"),
			ok("install", "gone:gone-hub/skill/gone", "--scope", "local", "--yes").
				has("./.factory/skills/gone/SKILL.md", "# gone").
				has("./.agents/lock.local.yaml", "name: gone"),
			push("changed-hub", map[string]string{
				"skills/current/SKILL.md": "---\nname: current\ndescription: Changed current pin\nlicense: MIT\n---\nChanged pin content\n",
			}),
			push("gone-hub", map[string]string{
				"skills/gone/SKILL.md": "---\nname: renamed\ndescription: Renamed resource\nlicense: MIT\n---\nRenamed resource content\n",
			}),
			reindex("changed-hub"),
			reindex("gone-hub"),
			ok("sync").
				unchanged("./.agents/lock.local.yaml").
				has("./.factory/skills/current/SKILL.md", "# current").
				has("./.factory/skills/gone/SKILL.md", "# gone"),
			exits(1, "update").
				out("Marketplace pins updated: 1", "{{MKT:gone-hub}}", "gone-hub-id/skill/gone", "agent-marketplace remove gone", "install the new name").
				unchanged("./.agents/lock.local.yaml", "./.agents/config.local.yaml", "./.agents/config.yaml").
				has("./.factory/skills/current/SKILL.md", "Changed pin content").
				has("./.factory/skills/gone/SKILL.md", "# gone").
				has("./.agents/lock.local.yaml", "name: gone").
				has("./.agents/lock.yaml", "name: current").
				lacks("./.factory/skills/current/SKILL.md", "Current pin"),
			ok("sync", "--check").
				unchanged("./.agents/lock.local.yaml").
				has("./.factory/skills/current/SKILL.md", "Changed pin content").
				has("./.factory/skills/gone/SKILL.md", "# gone"),
		},
	})

	// spec: status-reporting/organization-collection-members-are-not-orphaned-without-an-organization-lock
	testTable = append(testTable, TestCase{
		Name: "marketplace/organization_collection_status_without_lock",
		Marketplaces: []MarketplaceDef{{Name: "org-hub", Registry: RegistryDef{Files: map[string]string{
			"skills/org-member/SKILL.md":             "---\nname: org-member\ndescription: Organization collection member\nlicense: MIT\n---\nOrganization member body\n",
			"collections/org-bundle/COLLECTION.yaml": "name: org-bundle\nversion: \"1.0\"\ndescription: Organization collection\nresources:\n  - name: org-member\n    kind: skill\n",
		}}}},
		RepoCfg: v4("harnesses: [factory]"),
		OrgCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  organization: {marketplace: '{{MKT:org-hub}}'}",
			"resources:",
			"  organization: {include: [org-hub/collection/org-bundle]}",
		),
		Steps: []Step{
			ok("sync").absent("/etc/agent-marketplace/lock.yaml").has("~/.factory/skills/org-member/SKILL.md", "Organization member body"),
			ok("sync", "--check").absent("/etc/agent-marketplace/lock.yaml"),
			ok("status").
				out("org-member").
				notOut("orphan", "pending removal").
				absent("/etc/agent-marketplace/lock.yaml").
				unchanged("~/.agents/config.yaml", "./.agents/config.yaml", "/etc/agent-marketplace/config.yaml"),
		},
	})

	// spec: deployed-acquisition-locks/git-only-lock-gains-marketplace-evidence
	testTable = append(testTable, TestCase{
		Name:         "marketplace/git_lock_gains_marketplace_evidence",
		Registries:   []RegistryDef{{Name: "git-only", Skills: []Skill{{Name: "git-skill", Description: "Existing Git record"}}}},
		Marketplaces: []MarketplaceDef{{Name: "hub", Registry: tableMarketplaceRegistry(Skill{Name: "marketplace-skill", Description: "Marketplace evidence"})}},
		RepoCfg: v4(
			"harnesses: [factory]",
			"sources:",
			"  git: {git: '{{GITHTTP:git-only}}', ref: main}",
			"resources:",
			"  git: {include: [skill/git-skill]}",
		),
		Steps: []Step{
			ok("sync").has("./.agents/lock.yaml", "git-only").lacks("./.agents/lock.yaml", "instance_id: hub-marketplace"),
			Step{}.has("./.agents/lock.yaml", "ref: main"),
			writeRepoCfg(v4(
				"harnesses: [factory]",
				"sources:",
				"  git: {git: '{{GITHTTP:git-only}}', ref: main}",
				"  hub: {marketplace: '{{MKT:hub}}'}",
				"resources:",
				"  git: {include: [skill/git-skill]}",
				"  hub: {include: [hub/skill/marketplace-skill]}",
			)).
				then("sync").
				pass().
				exists("./.factory/skills/git-skill/SKILL.md", "./.factory/skills/marketplace-skill/SKILL.md").
				has("./.agents/lock.yaml", "instance_id: hub-marketplace"),
			Step{}.has("./.agents/lock.yaml", "git-only"),
			Step{}.has("./.agents/lock.yaml", "name: marketplace-skill"),
		},
	})
}

func TestE2ETableRevokeMarketplace(t *testing.T) {
	var firstErr string
	runTableCase(t, TestCase{
		Name:         "marketplace/revoke_preserves_consumer_output",
		Marketplaces: []MarketplaceDef{{Name: "revoke", Registry: tableMarketplaceRegistry(Skill{Name: "locked", Description: "Revocable skill"})}},
		RepoCfg:      v4("harnesses: [factory]", "sources: {revoke: {marketplace: '{{MKT:revoke}}'}}"),
		Steps: []Step{
			ok("install", "revoke:revoke/skill/locked", "--yes"),
			ok("sync").has("./.factory/skills/locked/SKILL.md", "Revocable skill"),
			revoke("revoke", MarketplaceRevocations{Coordinates: []string{"revoke-marketplace/revoke-id/skill/locked"}}),
			exits(1, "sync").out("revoked").
				unchanged("./.agents/config.yaml", "./.agents/lock.yaml", "./.factory/skills/locked/SKILL.md").
				has("./.factory/skills/locked/SKILL.md", "Revocable skill"),
		},
	}, &firstErr)
}
