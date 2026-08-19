package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestDeinitTable(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		seedCache       bool
		wantErr         bool
		wantErrContains string
		wantContains    []string
		wantNotContains []string
		cacheRemoved    bool
		configScope     string // "user", "repo", "local" — which config to check deletion of
		configDeleted   bool
	}{
		{
			name:         "user scope removes cache",
			args:         []string{"deinit"},
			seedCache:    true,
			wantContains: []string{"Removed cache"},
			cacheRemoved: true,
		},
		{
			name:         "user scope dry-run preserves cache",
			args:         []string{"deinit", "--dry-run"},
			seedCache:    true,
			wantContains: []string{"Would remove cache"},
			cacheRemoved: false,
		},
		{
			name:            "repo scope does not touch cache",
			args:            []string{"deinit", "--scope", "repo"},
			seedCache:       true,
			wantNotContains: []string{"cache"},
			cacheRemoved:    false,
		},
		{
			name:            "local scope does not touch cache",
			args:            []string{"deinit", "--scope", "local"},
			seedCache:       true,
			wantNotContains: []string{"cache"},
			cacheRemoved:    false,
		},
		{
			name:          "user purge-config removes global config",
			args:          []string{"deinit", "--purge-config"},
			wantContains:  []string{"Removed config"},
			configScope:   "user",
			configDeleted: true,
			cacheRemoved:  false,
		},
		{
			name:          "repo purge-config removes project config",
			args:          []string{"deinit", "--scope", "repo", "--purge-config"},
			wantContains:  []string{"Removed config"},
			configScope:   "repo",
			configDeleted: true,
		},
		{
			name:          "local purge-config removes local config",
			args:          []string{"deinit", "--scope", "local", "--purge-config"},
			wantContains:  []string{"Removed config"},
			configScope:   "local",
			configDeleted: true,
		},
		{
			name:          "dry-run purge-config does not delete config",
			args:          []string{"deinit", "--purge-config", "--dry-run"},
			wantContains:  []string{"Would remove config"},
			configScope:   "user",
			configDeleted: false,
			cacheRemoved:  false,
		},
		{
			name:            "invalid scope returns error",
			args:            []string{"deinit", "--scope", "galaxy"},
			wantErr:         true,
			wantErrContains: "unknown scope",
		},
		{
			name:         "no artifacts reports zero",
			args:         []string{"deinit", "--scope", "repo"},
			wantContains: []string{"No managed artifacts to remove"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)

			repoRoot := filepath.Join(t.TempDir(), "repo")
			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `registries: []`)
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)
			// Create local config for local scope tests.
			localCfg := filepath.Join(repoRoot, ".agents", "config.local.yaml")
			writeFile(t, localCfg, "registries: []\n")

			if tt.seedCache {
				cacheEntry := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/skills.git"), "main")
				writeFile(t, filepath.Join(cacheEntry, ".gitkeep"), "cached\n")
			}

			withWorkingDir(t, repoRoot)

			output, err := executeCommand(tt.args...)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErrContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, output)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, output)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(output, notWant) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", notWant, output)
				}
			}

			if tt.seedCache {
				// GarbageCollect removes orphaned cache subdirectories, not the root.
				seededHash := cache.HashURL("ssh://git@example.com/skills.git")
				seededPath := filepath.Join(cache.CacheDir(), seededHash)
				_, seededErr := os.Stat(seededPath)
				if tt.cacheRemoved && seededErr == nil {
					t.Error("expected seeded cache subdirectory to be removed")
				}
				if !tt.cacheRemoved && seededErr != nil {
					t.Errorf("expected seeded cache subdirectory to remain: %v", seededErr)
				}
			}

			if tt.configScope != "" {
				var cfgPath string
				switch tt.configScope {
				case "user":
					cfgPath = filepath.Join(homeDir, ".agents", "config.yaml")
				case "repo":
					cfgPath = filepath.Join(repoRoot, ".agents", "config.yaml")
				case "local":
					cfgPath = localCfg
				}
				_, statErr := os.Stat(cfgPath)
				if tt.configDeleted && statErr == nil {
					t.Errorf("expected config %s to be deleted", cfgPath)
				}
				if !tt.configDeleted && statErr != nil {
					t.Errorf("expected config %s to remain: %v", cfgPath, statErr)
				}
			}
		})
	}
}

func TestDeinitDryRunDoesNotRemoveEmptyAgentsDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	globalCfg := filepath.Join(homeDir, ".agents", "config.yaml")
	writeConfig(t, globalCfg, `registries: []`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--purge-config", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, output)
	}

	// The .agents directory must remain in dry-run mode.
	agentsDir := filepath.Join(homeDir, ".agents")
	if _, statErr := os.Stat(agentsDir); statErr != nil {
		t.Fatalf("expected .agents directory to remain after dry-run: %v", statErr)
	}
}

func TestDeinitRemovesManagedMcpEntries(t *testing.T) {
	// Verify that deinit strips managed MCP entries from .factory/mcp.json
	// while preserving user-owned entries.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries: []
`)

	// Simulate a .factory/mcp.json with both managed and user entries.
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpContent := `{
  "mcpServers": {
    "managed-srv": {
      "command": "managed",
      "_managed_by": "agent-marketplace"
    },
    "personal-srv": {
      "command": "personal"
    }
  }
}`
	writeFile(t, filepath.Join(factoryDir, "mcp.json"), mcpContent)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--scope", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, output)
	}

	// The managed entry should be gone; user entry preserved.
	mcpPath := filepath.Join(factoryDir, "mcp.json")
	content, readErr := os.ReadFile(mcpPath)
	if readErr != nil {
		t.Fatalf("mcp.json should still exist (has user entry): %v", readErr)
	}
	got := string(content)
	if strings.Contains(got, `"managed-srv"`) {
		t.Errorf("expected managed MCP entry removed, got:\n%s", got)
	}
	if strings.Contains(got, `"_managed_by"`) {
		t.Errorf("expected no _managed_by markers, got:\n%s", got)
	}
	if !strings.Contains(got, `"personal-srv"`) {
		t.Errorf("expected user MCP entry preserved, got:\n%s", got)
	}

	_ = output
}

func TestDeinitKeepsEmptyMcpFileIfAllManaged(t *testing.T) {
	// Verify that deinit keeps mcp.json with an empty mcpServers object when
	// it only contains managed entries.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries: []
`)

	// Simulate a .factory/mcp.json with only managed entries.
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpContent := `{
  "mcpServers": {
    "managed-srv": {
      "command": "managed",
      "_managed_by": "agent-marketplace"
    }
  }
}`
	writeFile(t, filepath.Join(factoryDir, "mcp.json"), mcpContent)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--scope", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, output)
	}

	// The file should remain with an empty mcpServers object.
	mcpPath := filepath.Join(factoryDir, "mcp.json")
	content, readErr := os.ReadFile(mcpPath)
	if readErr != nil {
		t.Fatalf("expected mcp.json to remain: %v", readErr)
	}
	got := string(content)
	if strings.Contains(got, `"managed-srv"`) {
		t.Fatalf("expected managed MCP entry removed, got:\n%s", got)
	}
	// The prune path patches concrete syntax instead of re-marshaling (bead
	// agent-marketplace-5exj.18), so the emptied container keeps the user's own
	// inner whitespace rather than collapsing to "{}". Assert the shape.
	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("unmarshal pruned mcp.json: %v\n%s", err, got)
	}
	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers to remain an object, got %T in:\n%s", parsed["mcpServers"], got)
	}
	if len(servers) != 0 {
		t.Fatalf("expected empty mcpServers object, got:\n%s", got)
	}

	_ = output
}

func TestDeinitUserPreservesOrgOwnedArtifacts(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")

	output, err = executeCommand("deinit")
	if err != nil {
		t.Fatalf("expected deinit to succeed: %v\n%s", err, output)
	}

	assertSkillLinkAbsent(t, homeDir, "user-skill")
	assertSkillLinkPresent(t, homeDir, "org-skill")
}

func TestDeinitUserDryRunReportsUserOwnedChanges(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)

	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}

	output, err := executeCommand("deinit", "--dry-run")
	if err != nil {
		t.Fatalf("expected deinit --dry-run to succeed: %v\n%s", err, output)
	}

	// The dry-run should report user-owned artifacts would be removed while
	// preserving the org-owned skill directory. A dry run removes nothing,
	// so both symlinks must still exist on disk.
	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")
}

func TestDeinitUserPurgeConfigDoesNotRemoveOrgConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries: []
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--purge-config")
	if err != nil {
		t.Fatalf("expected deinit to succeed: %v\n%s", err, output)
	}

	if _, err := os.Stat(orgCfgPath); err != nil {
		t.Fatalf("expected org config to remain untouched: %v", err)
	}
	if strings.Contains(output, orgCfgPath) {
		t.Fatalf("expected output not to mention org config path, got:\n%s", output)
	}
}

func TestDeinitUserDryRunUsesCachedCatalog(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nregistries: []\n")

	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")

	// Remove the origin so a refresh would fail; cached mode must still work.
	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("remove origin: %v", err)
	}

	output, err := executeCommand("deinit", "--dry-run")
	if err != nil {
		t.Fatalf("expected deinit --dry-run to succeed without fetching: %v\n%s", err, output)
	}

	// Dry run removes nothing; both the user-owned and org-owned skill
	// symlinks must still exist.
	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")
}

func TestDeinitUserDryRunDoesNotFetchColdCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: ssh://git@example.com/should-not-be-fetched.git
    ref: main
    skills:
      - org-skill
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nregistries: []\n")

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--dry-run")
	if err != nil {
		t.Fatalf("expected deinit --dry-run to succeed without fetching: %v\n%s", err, output)
	}

	if !strings.Contains(output, "No managed artifacts to remove") {
		t.Fatalf("expected dry-run output to report no managed artifacts, got:\n%s", output)
	}
}

func TestDeinitUserWithoutHarnessSetDoesNotCreateOutput(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nregistries: []\n")

	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")

	// Remove the target harness set from both configs. Without a harness set
	// there is no organization-owned output to preserve, so deinit should
	// remove existing output without defaulting to an unwanted target.
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)

	output, err := executeCommand("deinit")
	if err != nil {
		t.Fatalf("expected deinit to succeed: %v\n%s", err, output)
	}

	if _, statErr := os.Lstat(filepath.Join(homeDir, ".claude", "skills")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no .claude/skills to be created, but it exists")
	}

	assertSkillLinkAbsent(t, homeDir, "user-skill")
	// With no target harness set there is no org-owned output to preserve.
	assertSkillLinkAbsent(t, homeDir, "org-skill")
}

func assertSkillLinkPresent(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(root, ".factory", "skills", name)
	if info, err := os.Lstat(path); err != nil {
		t.Fatalf("expected skill symlink to exist at %s: %v", path, err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected skill symlink at %s to be a symlink", path)
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Fatalf("expected preserved skill symlink target to be reachable at %s: %v", path, err)
	}
}

func assertSkillLinkAbsent(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(root, ".factory", "skills", name)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected skill symlink to be absent at %s", path)
	}
}
