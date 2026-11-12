package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestDeinitPurgeLayerPair(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scope := range []string{"user", "repo", "local"} {
		for _, mode := range []string{"purge", "orphan", "dry-run", "no-purge"} {
			t.Run(scope+"/"+mode, func(t *testing.T) {
				home, repo := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				org := filepath.Join(home, "etc", "config.yaml")
				config.SetTestV3OrganizationConfigPath(t, org)
				configs := map[string]string{
					"user":         filepath.Join(home, ".agents", "config.yaml"),
					"repo":         filepath.Join(repo, ".agents", "config.yaml"),
					"local":        filepath.Join(repo, ".agents", "config.local.yaml"),
					"organization": org,
				}
				locks := map[string]string{
					"user":         filepath.Join(home, ".agents", "lock.yaml"),
					"repo":         filepath.Join(repo, ".agents", "lock.yaml"),
					"local":        filepath.Join(repo, ".agents", "lock.local.yaml"),
					"organization": filepath.Join(home, "etc", "lock.yaml"),
				}
				preserved := map[string][]byte{}
				for layer, path := range configs {
					writeConfig(t, path, "apiVersion: agent-marketplace/v4\ninclude: [fragment.yaml]\n")
					fragment := filepath.Join(filepath.Dir(path), "fragment.yaml")
					writeFile(t, fragment, "apiVersion: agent-marketplace/v4\n")
					preserved[fragment] = []byte("apiVersion: agent-marketplace/v4\n")
					writeFile(t, locks[layer], "apiVersion: agent-marketplace-lock/v1\nscope: "+layer+"\nregistries: []\n")
					for _, name := range []string{path, locks[layer]} {
						data, err := os.ReadFile(name)
						if err != nil {
							t.Fatal(err)
						}
						preserved[name] = data
					}
				}
				sidecars := map[string]os.FileInfo{}
				for _, path := range []string{configs[scope], locks[scope]} {
					sidecar := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".dependencytxn.lock")
					writeFile(t, sidecar, "stable coordination")
					info, err := os.Stat(sidecar)
					if err != nil {
						t.Fatal(err)
					}
					sidecars[sidecar] = info
					preserved[sidecar] = []byte("stable coordination")
				}
				marker := filepath.Join(home, "state", "agent-marketplace", "dependencies", "unrelated.json")
				writeFile(t, marker, "shared state")
				preserved[marker] = []byte("shared state")
				if mode == "orphan" {
					if err := os.Remove(configs[scope]); err != nil {
						t.Fatal(err)
					}
				}
				withWorkingDir(t, repo)
				args := []string{"deinit", "--scope", scope}
				if mode != "no-purge" {
					args = append(args, "--purge-config")
				}
				if mode == "dry-run" {
					args = append(args, "--dry-run")
				}
				out, err := executeCommand(args...)
				if err != nil {
					t.Fatalf("deinit: %v\n%s", err, out)
				}
				if mode == "purge" || mode == "orphan" {
					for _, path := range []string{configs[scope], locks[scope]} {
						delete(preserved, path)
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Errorf("purged file remains: %s (%v)", path, err)
						}
					}
				}
				if mode == "dry-run" && !strings.Contains(out, "Would remove lock ") {
					t.Errorf("missing lock preview: %s", out)
				}
				for path, want := range preserved {
					got, err := os.ReadFile(path)
					if err != nil || string(got) != string(want) {
						t.Errorf("changed preserved file %s: %q, %v", path, got, err)
					}
				}
				for path, before := range sidecars {
					after, err := os.Stat(path)
					if err != nil || !os.SameFile(before, after) {
						t.Errorf("coordination inode changed: %s (%v)", path, err)
					}
				}
			})
		}
	}
}

func TestDeinitPurgePreservesPendingPublication(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\n")
	writeFile(t, lockPath, "old-lock")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	stateDir, err := lifecycle.DependencyStateDir()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := dependencytxn.ValidateDestination(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := dependencytxn.ReadGeneration(canonical)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("interrupted lock publication")
	_, err = dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{{Path: canonical, Expected: generation, Bytes: []byte("new-lock"), Mode: 0o644}}, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultAfterJournal {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	out, err := executeCommand("deinit", "--scope", "repo", "--purge-config")
	if !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("pending publication: %v\n%s", err, out)
	}
	for path, want := range map[string]string{configPath: string(before), lockPath: "old-lock"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("purge changed %s: %q, %v", path, got, err)
		}
	}
	if err := dependencytxn.CheckPending(stateDir); !errors.Is(err, dependencytxn.ErrRecoveryRequired) {
		t.Fatalf("lost pending journal: %v", err)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatal(err)
	}
	if out, err := executeCommand("deinit", "--scope", "repo", "--purge-config"); err != nil {
		t.Fatalf("purge after recovery: %v\n%s", err, out)
	}
	for _, path := range []string{configPath, lockPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("purge after recovery retained %s: %v", path, err)
		}
	}
}

func TestDeinitTable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
			writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)
			writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)
			// Create local config for local scope tests.
			localCfg := filepath.Join(repoRoot, ".agents", "config.local.yaml")
			writeFile(t, localCfg, "apiVersion: agent-marketplace/v4\n")

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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	globalCfg := filepath.Join(homeDir, ".agents", "config.yaml")
	writeConfig(t, globalCfg, `apiVersion: agent-marketplace/v4`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)

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
	if isolateCommandTest(t) {
		return
	}
	// Verify that deinit strips managed MCP entries from .factory/mcp.json
	// while preserving user-owned entries.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources: {}
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
	if isolateCommandTest(t) {
		return
	}
	// Verify that deinit keeps mcp.json with an empty mcpServers object when
	// it only contains managed entries.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources: {}
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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/user-skill]
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)

	withWorkingDir(t, repoRoot)
	requireOrganizationLock(t, orgCfgPath)

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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/user-skill]
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)

	withWorkingDir(t, repoRoot)
	requireOrganizationLock(t, orgCfgPath)

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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4`)

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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/user-skill]
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")

	withWorkingDir(t, repoRoot)
	requireOrganizationLock(t, orgCfgPath)

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

func TestDeinitUserDryRunFailsClosedWhenOrgCatalogCacheIsCold(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: ssh://git@example.com/should-not-be-fetched.git, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--dry-run")
	if err == nil {
		t.Fatalf("expected deinit --dry-run to refuse a cold organization cache, output:\n%s", output)
	}
	if !strings.Contains(output, "refusing teardown") {
		t.Fatalf("unexpected deinit --dry-run output: %v\n%s", err, output)
	}
}

func TestDeinitUserDryRunFailsClosedWhenOrgCatalogUnavailable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir, repoRoot, cacheEntry := setupUnavailableOrganization(t)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit", "--dry-run")
	if err == nil {
		t.Fatalf("expected deinit --dry-run to refuse unavailable organization catalog, output:\n%s", output)
	}
	if !strings.Contains(output, "refusing teardown") {
		t.Fatalf("unexpected deinit --dry-run output: %v\n%s", err, output)
	}
	assertSkillLinkPresent(t, homeDir, "org-skill")
	if _, statErr := os.Stat(cacheEntry); statErr != nil {
		t.Fatalf("expected cache to remain after failed dry-run: %v", statErr)
	}
}

func TestDeinitUserFailsClosedWhenOrgCatalogUnavailable(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir, repoRoot, cacheEntry := setupUnavailableOrganization(t)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("deinit")
	if err == nil {
		t.Fatalf("expected deinit to refuse unavailable organization catalog, output:\n%s", output)
	}
	if !strings.Contains(output, "refusing teardown") {
		t.Fatalf("unexpected deinit output: %v\n%s", err, output)
	}
	assertSkillLinkPresent(t, homeDir, "org-skill")
	if _, statErr := os.Stat(cacheEntry); statErr != nil {
		t.Fatalf("expected cache to remain after failed teardown: %v", statErr)
	}
}

func setupUnavailableOrganization(t *testing.T) (homeDir, repoRoot, cacheEntry string) {
	t.Helper()
	homeDir = t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: ssh://git@example.com/should-not-be-fetched.git, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot = filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")

	cacheEntry = filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/should-not-be-fetched.git"), "main")
	writeFile(t, filepath.Join(cacheEntry, ".gitkeep"), "cached\n")

	externalSkill := filepath.Join(t.TempDir(), "org-skill")
	writeFile(t, filepath.Join(externalSkill, "SKILL.md"), "---\nname: org-skill\ndescription: Org skill\n---\n")
	orgOutput := filepath.Join(homeDir, ".factory", "skills", "org-skill")
	if err := os.MkdirAll(filepath.Dir(orgOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalSkill, orgOutput); err != nil {
		t.Fatal(err)
	}
	return homeDir, repoRoot, cacheEntry
}

func TestDeinitUserWithoutHarnessSetDoesNotCreateOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/org-skill]
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/user-skill]
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")

	withWorkingDir(t, repoRoot)
	requireOrganizationLock(t, orgCfgPath)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected sync to succeed: %v", err)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")

	// Remove the target harness set from both configs. Without a harness set
	// there is no organization-owned output to preserve, so deinit should
	// remove existing output without defaulting to an unwanted target.
	writeFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/user-skill]
`)
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {git: file://`+origin+`, ref: main}
resources:
  hosted: [skill/org-skill]
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

func TestDeinitUserPreservesCachedOrganizationMarketplaceWithoutNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	orgPin, orgRevision, orgRaw := deinitMarketplaceFixture(t, "org-review", "org-source")
	userPin, userRevision, userRaw := deinitMarketplaceFixture(t, "user-review", "user-source")
	var requests atomic.Int32
	var failEndpoint atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failEndpoint.Load() {
			requests.Add(1)
			http.Error(w, "Marketplace endpoint must not be contacted during deinit", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("deinit-marketplace", "https://example.com"))
		case r.URL.Path == "/api/v1/registries/org-source" || r.URL.Path == "/api/v1/registries/user-source":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/registries/")
			_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{ID: store.RegistryID(id), Slug: id, Origins: []string{"https://example.com/" + id + ".git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case strings.Contains(r.URL.Path, "/revisions/"+orgPin.Digest):
			_ = json.NewEncoder(w).Encode(orgRevision)
		case strings.Contains(r.URL.Path, "/revisions/"+userPin.Digest):
			_ = json.NewEncoder(w).Encode(userRevision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") && strings.HasSuffix(r.URL.Path, strings.TrimPrefix(string(orgRevision.ArchiveDigest), "sha256:")):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(orgRaw)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") && strings.HasSuffix(r.URL.Path, strings.TrimPrefix(string(userRevision.ArchiveDigest), "sha256:")):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(userRaw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted:
    include:
      - org-source/skill/org-review: {revision: `+orgPin.Digest+`}
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted:
    include:
      - user-source/skill/user-review: {revision: `+userPin.Digest+`}
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	withWorkingDir(t, repoRoot)

	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected Marketplace sync to seed materialized output: %v", err)
	}
	assertFactorySkillExists(t, homeDir, "org-review")
	assertFactorySkillExists(t, homeDir, "user-review")

	// The organization artifact remains warm, while the optional user artifact
	// is removed to prove it cannot block preservation. Deinit must not need the
	// endpoint for either pin, and must preserve the cached organization output.
	mpCache := marketplace.DefaultCache()
	if err := os.RemoveAll(filepath.Dir(mpCache.ArtifactPath(string(userRevision.ArchiveDigest)))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mpCache.MetadataPath(server.URL, userPin.RegistryID, userPin.Kind, userPin.Name, userPin.Digest)); err != nil {
		t.Fatal(err)
	}
	failEndpoint.Store(true)

	output, err := executeCommand("deinit")
	if err != nil {
		t.Fatalf("expected cached organization Marketplace preservation: %v\n%s", err, output)
	}
	if requests.Load() != 0 {
		t.Fatalf("deinit contacted Marketplace endpoint %d time(s)", requests.Load())
	}
	assertSkillLinkAbsent(t, homeDir, "user-review")
	assertFactorySkillExists(t, homeDir, "org-review")
	if _, err := os.Stat(mpCache.ArtifactPath(string(orgRevision.ArchiveDigest))); err != nil {
		t.Fatalf("organization Marketplace artifact cache was removed: %v", err)
	}
}

func TestDeinitUserFailsClosedForInvalidOrganizationMarketplaceCache(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name       string
		breakCache func(t *testing.T, c *marketplace.Cache, revision store.ResourceRevision) string
	}{
		{
			name: "missing artifact",
			breakCache: func(t *testing.T, c *marketplace.Cache, revision store.ResourceRevision) string {
				if err := os.Remove(c.ArtifactPath(string(revision.ArchiveDigest))); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
		{
			name: "symlinked artifact directory",
			breakCache: func(t *testing.T, c *marketplace.Cache, revision store.ResourceRevision) string {
				artifactDir := filepath.Dir(c.ArtifactPath(string(revision.ArchiveDigest)))
				external := t.TempDir()
				marker := filepath.Join(external, "must-survive")
				writeFile(t, marker, "external\n")
				if err := os.RemoveAll(artifactDir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, artifactDir); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(artifactDir) })
				return marker
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := setupDeinitOrganizationMarketplace(t)
			externalMarker := tc.breakCache(t, scenario.cache, scenario.revision)
			scenario.failEndpoint.Store(true)

			output, err := executeCommand("deinit")
			if err == nil {
				t.Fatalf("expected deinit to refuse invalid organization cache, output:\n%s", output)
			}
			if !strings.Contains(output, "refusing teardown") {
				t.Fatalf("unexpected deinit output: %v\n%s", err, output)
			}
			if scenario.requests.Load() != 0 {
				t.Fatalf("deinit contacted Marketplace endpoint %d time(s)", scenario.requests.Load())
			}
			assertFactorySkillExists(t, scenario.homeDir, "org-review")
			if externalMarker != "" {
				if _, statErr := os.Stat(externalMarker); statErr != nil {
					t.Fatalf("deinit touched symlink target: %v", statErr)
				}
			}
		})
	}
}

type deinitMarketplaceScenario struct {
	homeDir      string
	cache        *marketplace.Cache
	revision     store.ResourceRevision
	requests     *atomic.Int32
	failEndpoint *atomic.Bool
}

func setupDeinitOrganizationMarketplace(t *testing.T) deinitMarketplaceScenario {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	pin, revision, raw := deinitMarketplaceFixture(t, "org-review", "org-source")
	var requests atomic.Int32
	var failEndpoint atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failEndpoint.Load() {
			requests.Add(1)
			http.Error(w, "Marketplace endpoint must not be contacted during deinit", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("deinit-marketplace", "https://example.com"))
		case r.URL.Path == "/api/v1/registries/org-source":
			_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{ID: "org-source", Slug: "org-source", Origins: []string{"https://example.com/org-source.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case strings.Contains(r.URL.Path, "/revisions/"+pin.Digest):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") && strings.HasSuffix(r.URL.Path, strings.TrimPrefix(string(revision.ArchiveDigest), "sha256:")):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	orgCfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted:
    include:
      - org-source/skill/org-review: {revision: `+pin.Digest+`}
`)
	config.SetTestOrganizationConfigPath(t, orgCfgPath)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	withWorkingDir(t, repoRoot)
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("expected Marketplace sync to seed materialized output: %v", err)
	}
	assertFactorySkillExists(t, homeDir, "org-review")
	return deinitMarketplaceScenario{
		homeDir: homeDir, cache: marketplace.DefaultCache(), revision: revision.ResourceRevision,
		requests: &requests, failEndpoint: &failEndpoint,
	}
}

func deinitMarketplaceFixture(t *testing.T, name, sourceID string) (config.MarketplaceResourcePin, store.AssessedResourceRevision, []byte) {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(name, name+" description", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.RegistryID = sourceID
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	pin := config.MarketplaceResourcePin{
		RegistryID: sourceID, Kind: "skill", Name: name,
		Revision: "refs/heads/main", Digest: contentDigest,
	}
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "deinit-marketplace", Registry: store.RegistryID(sourceID), Kind: "skill", Name: name},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	return pin, revision, raw
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
