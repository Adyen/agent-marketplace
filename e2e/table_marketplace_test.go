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
			{Args: []string{"install", "--scope", "repo", "hub:hub/skill/selected", "--yes"}, MustPass: true},
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
			{Args: []string{"install", "--scope", "repo", "hub:hub/skill/current", "--yes"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/current/SKILL.md": "# current"}},
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
}

func TestE2ETableRevokeMarketplace(t *testing.T) {
	var firstErr string
	runTableCase(t, TestCase{
		Name:         "marketplace/revoke_preserves_consumer_output",
		Marketplaces: []MarketplaceDef{{Name: "revoke", Registry: tableMarketplaceRegistry(Skill{Name: "locked", Description: "Revocable skill"})}},
		GlobalCfg:    v4Cfg("factory"),
		RepoCfg:      v4Cfg("factory") + "sources: {revoke: {marketplace: '{{MKT:revoke}}'}}\n",
		Steps: []Step{
			{Args: []string{"install", "--scope", "repo", "revoke:revoke/skill/locked", "--yes"}, MustPass: true},
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
