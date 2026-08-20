package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeV2Config(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(content))
}

func readConfigString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config %q: %v", path, err)
	}
	return string(data)
}

func TestMigrateV2ToV3UserAndRepo(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - cursor
registries:
  - url: git@example.com:user.git
    ref: main
`)

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}
	if !result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("expected both user and repo to be migrated: %+v", result)
	}

	userData := readConfigString(t, userPath)
	if !strings.Contains(userData, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected user config to be v3, got:\n%s", userData)
	}
	if !strings.Contains(userData, "harnesses:") {
		t.Fatalf("expected user harnesses to be preserved, got:\n%s", userData)
	}

	repoData := readConfigString(t, repoPath)
	if !strings.Contains(repoData, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected repo config to be v3, got:\n%s", repoData)
	}
	if strings.Contains(repoData, "harnesses:") {
		t.Fatalf("expected repo harnesses to be removed, got:\n%s", repoData)
	}
}

func TestMigratePreservesUserHarnessesAndRemovesRepoHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - codex
`)

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
harnesses:
  - claude
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}

	userCfg, errs := loadV3Config([]byte(readConfigString(t, userPath)), userLayer, true)
	if len(errs) > 0 {
		t.Fatalf("expected valid migrated user config: %v", errors.Join(errs...))
	}
	if !equalStrings(userCfg.Harnesses, []string{"factory", "codex"}) {
		t.Fatalf("expected user harnesses preserved as [factory codex], got %v", userCfg.Harnesses)
	}

	repoCfg, errs := loadV3Config([]byte(readConfigString(t, repoPath)), repoLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected valid migrated repo config: %v", errors.Join(errs...))
	}
	if len(repoCfg.Harnesses) != 0 {
		t.Fatalf("expected repo harnesses removed, got %v", repoCfg.Harnesses)
	}
}

func TestMigrateUsesRepoHarnessesWhenUserMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
harnesses:
  - windsurf
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}
	if !result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("expected user created and repo migrated: %+v", result)
	}

	userCfg, errs := loadV3Config([]byte(readConfigString(t, userPath)), userLayer, true)
	if len(errs) > 0 {
		t.Fatalf("expected valid user config: %v", errors.Join(errs...))
	}
	if !equalStrings(userCfg.Harnesses, []string{"windsurf"}) {
		t.Fatalf("expected user harnesses [windsurf] from repo, got %v", userCfg.Harnesses)
	}
}

func TestMigrateRequiresHarnessesNonInteractive(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if !errors.Is(err, ErrHarnessesRequired) {
		t.Fatalf("expected ErrHarnessesRequired, got %v", err)
	}
}

func TestMigrateRejectsV1(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err == nil {
		t.Fatalf("expected migration to fail for v1 repo config")
	}
	if !strings.Contains(err.Error(), "v1") {
		t.Fatalf("expected error to mention v1, got %q", err.Error())
	}

	// Verify repo file is unchanged.
	if strings.Contains(readConfigString(t, repoPath), "apiVersion") {
		t.Fatalf("expected v1 repo file to remain unmodified")
	}
	// Verify user file is unchanged.
	if !strings.Contains(readConfigString(t, userPath), "agent-marketplace/v2") {
		t.Fatalf("expected user file to remain unmodified")
	}
}

func TestMigrateRejectsIdentity(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
identity:
  provider: ldap
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if !errors.Is(err, ErrLegacyIdentity) {
		t.Fatalf("expected ErrLegacyIdentity, got %v", err)
	}

	if !strings.Contains(readConfigString(t, userPath), "identity:") {
		t.Fatalf("expected user file with identity to remain unmodified")
	}
}

func TestMigrateRejectsTeamProfiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
team_profiles:
  platform:
    registries: []
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, []string{"factory"})
	if !errors.Is(err, ErrLegacyIdentity) {
		t.Fatalf("expected ErrLegacyIdentity, got %v", err)
	}

	if !strings.Contains(readConfigString(t, repoPath), "team_profiles:") {
		t.Fatalf("expected repo file with team_profiles to remain unmodified")
	}
}

func TestMigrateIdempotentV3(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries: []
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v3
registries: []
`)
	originalUser := readConfigString(t, userPath)
	originalRepo := readConfigString(t, repoPath)

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected idempotent migration to succeed: %v", err)
	}
	if result.UserMigrated || result.RepoMigrated {
		t.Fatalf("expected no files to be modified for already-v3 configs: %+v", result)
	}
	if got := readConfigString(t, userPath); got != originalUser {
		t.Fatalf("expected v3 user config bytes to remain unchanged, got:\n%s", got)
	}
	if got := readConfigString(t, repoPath); got != originalRepo {
		t.Fatalf("expected v3 repo config bytes to remain unchanged, got:\n%s", got)
	}
}

func TestMigrateRejectsRepoBackupAndLoggingWithoutDeletingPolicy(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	originalUser := readConfigString(t, userPath)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
harnesses:
  - claude
backup:
  enabled: true
logging:
  level: debug
registries:
  - url: git@example.com:repo.git
    ref: main
`)
	originalRepo := readConfigString(t, repoPath)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if !errors.Is(err, ErrUnmigratablePolicy) {
		t.Fatalf("expected ErrUnmigratablePolicy, got %v", err)
	}
	if got := readConfigString(t, userPath); got != originalUser {
		t.Fatalf("rejected migration changed user bytes:\nbefore: %q\nafter:  %q", originalUser, got)
	}
	if got := readConfigString(t, repoPath); got != originalRepo {
		t.Fatalf("rejected migration changed repo bytes:\nbefore: %q\nafter:  %q", originalRepo, got)
	}
}

func TestMigratePreservesPolicyOnlyAtValidAuthority(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses: [factory]
allowed_registries:
  - ^example\\.com/
backup:
  enabled: false
logging:
  level: debug
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
allowed_registries:
  - ^repo\\.example/
registries: []
`)

	if _, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil); err != nil {
		t.Fatalf("MigrateConfigFiles: %v", err)
	}
	for path, fields := range map[string][]string{
		userPath: {"allowed_registries:", "backup:", "logging:", "level: debug"},
		repoPath: {"allowed_registries:", "^repo\\\\.example/"},
	} {
		data := readConfigString(t, path)
		for _, field := range fields {
			if !strings.Contains(data, field) {
				t.Fatalf("%s lost policy %q:\n%s", path, field, data)
			}
		}
	}

	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	originalLocal := "apiVersion: agent-marketplace/v2\nallowed_registries: [.*]\nskills:\n  include: [audit]\n"
	writeV2Config(t, localPath, originalLocal)
	beforeUser := readConfigString(t, userPath)
	beforeRepo := readConfigString(t, repoPath)
	if _, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath, Local: localPath}, nil); !errors.Is(err, ErrUnmigratablePolicy) {
		t.Fatalf("expected local policy rejection, got %v", err)
	}
	if readConfigString(t, userPath) != beforeUser || readConfigString(t, repoPath) != beforeRepo || readConfigString(t, localPath) != originalLocal {
		t.Fatal("local policy rejection changed config bytes")
	}
}

func TestMigrateRejectsEveryAuthorityChangingV2PolicyBeforeWrites(t *testing.T) {
	tests := []struct {
		name    string
		layer   string
		content string
	}{
		{
			name:    "user materialization belongs to project authority",
			layer:   "user",
			content: "# user policy\napiVersion: agent-marketplace/v2\nharnesses: [factory]\nmaterialization:\n  mode: file\n",
		},
		{
			name:    "local top-level tags have no local v3 representation",
			layer:   "local",
			content: "# local tags\napiVersion: agent-marketplace/v2\ntags: [security]\n",
		},
		{
			name:    "local required registry would gain organization authority",
			layer:   "local",
			content: "# local required\napiVersion: agent-marketplace/v2\nregistries:\n  - url: ./registry\n    required: true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			paths := MigratePaths{
				User:  filepath.Join(root, "home", ".agents", "config.yaml"),
				Repo:  filepath.Join(root, "repo", ".agents", "config.yaml"),
				Local: filepath.Join(root, "repo", ".agents", "config.local.yaml"),
			}
			if tt.layer == "user" {
				writeV2Config(t, paths.User, tt.content)
			} else {
				writeV2Config(t, paths.User, "# stable user\napiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
				writeV2Config(t, paths.Local, tt.content)
			}
			before := map[string]string{}
			for _, path := range []string{paths.User, paths.Repo, paths.Local} {
				if data, err := os.ReadFile(path); err == nil {
					before[path] = string(data)
				}
			}

			_, err := MigrateConfigFiles(paths, nil)
			if !errors.Is(err, ErrUnmigratablePolicy) {
				t.Fatalf("expected ErrUnmigratablePolicy, got %v", err)
			}
			if !strings.Contains(err.Error(), "move") {
				t.Fatalf("policy refusal lacks remediation: %v", err)
			}
			for path, original := range before {
				if got := readConfigString(t, path); got != original {
					t.Fatalf("policy refusal changed %q:\nbefore: %q\nafter:  %q", path, original, got)
				}
			}
		})
	}
}

func TestMigrateLocalConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, localPath, `apiVersion: agent-marketplace/v2
harnesses:
  - claude
skills:
  include:
    - local-skill
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath, Local: localPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}

	localData := readConfigString(t, localPath)
	if !strings.Contains(localData, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected local config to be v3, got:\n%s", localData)
	}
	if strings.Contains(localData, "harnesses:") {
		t.Fatalf("expected local harnesses to be removed, got:\n%s", localData)
	}

	if _, errs := loadV3LocalConfig([]byte(localData)); len(errs) > 0 {
		t.Fatalf("expected migrated local config to be valid: %v", errors.Join(errs...))
	}
}

func TestMigratePreservesCommentsSelectorsAndRemovesForbiddenTargets(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeV2Config(t, repoPath, `# repo contract comment
apiVersion: agent-marketplace/v2
harnesses: [cursor]
registries:
  - url: ./registry
    skills:
      include: [audit]
      exclude: [legacy]
`)
	writeV2Config(t, localPath, `# local contract comment
apiVersion: agent-marketplace/v2
harnesses: [gemini]
skills:
  include: [personal]
  exclude: [blocked]
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath, Local: localPath}, nil)
	if err != nil {
		t.Fatalf("MigrateConfigFiles: %v", err)
	}
	userData := readConfigString(t, userPath)
	if !strings.Contains(userData, "- cursor") || strings.Contains(userData, "devin") {
		t.Fatalf("user targets were not moved exactly from repo:\n%s", userData)
	}
	for path, wants := range map[string][]string{
		repoPath:  {"# repo contract comment", "include: [audit]", "exclude: [legacy]"},
		localPath: {"# local contract comment", "include: [personal]", "exclude: [blocked]"},
	} {
		data := readConfigString(t, path)
		if strings.Contains(data, "harnesses:") {
			t.Fatalf("%s retained forbidden targets:\n%s", path, data)
		}
		for _, want := range wants {
			if !strings.Contains(data, want) {
				t.Fatalf("%s lost %q:\n%s", path, want, data)
			}
		}
	}
}

func TestMigrateRejectsUnknownTargetWithoutMutation(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	original := "# unchanged\napiVersion: agent-marketplace/v2\nharnesses: [devni]\nregistries: []\n"
	writeV2Config(t, repoPath, original)

	if _, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil); err == nil || !strings.Contains(err.Error(), "devni") {
		t.Fatalf("migration error = %v, want unknown target", err)
	}
	if got := readConfigString(t, repoPath); got != original {
		t.Fatalf("invalid migration rewrote repo config:\nbefore: %q\nafter:  %q", original, got)
	}
	if _, err := os.Stat(userPath); !os.IsNotExist(err) {
		t.Fatalf("invalid migration created user config: %v", err)
	}
}

func TestMigrateConvertsLegacyCollections(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
collections:
  - name: core-security
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}

	repoData := readConfigString(t, repoPath)
	if !strings.Contains(repoData, "include:") || !strings.Contains(repoData, "core-security") {
		t.Fatalf("expected collections to be converted to map form, got:\n%s", repoData)
	}
}

func TestMigrateIdempotentForEmptyCollections(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
collections: []
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}
	if !result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("expected user and repo to migrate on first run: %+v", result)
	}

	repoCfg, errs := loadV3Config([]byte(readConfigString(t, repoPath)), repoLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected valid migrated repo config: %v", errors.Join(errs...))
	}
	if len(repoCfg.Collections.Include) != 0 || len(repoCfg.Collections.Exclude) != 0 {
		t.Fatalf("expected empty collections selector, got %+v", repoCfg.Collections)
	}

	result, err = MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected second migration to succeed: %v", err)
	}
	if result.RepoMigrated {
		t.Fatalf("expected second migration to be idempotent: %+v", result)
	}
}

func TestMigrateRejectsUnsupportedApiVersion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v1
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err == nil {
		t.Fatal("expected migration to fail for unsupported apiVersion")
	}
	if !strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("expected unsupported apiVersion error, got: %v", err)
	}

	// No file should have been modified.
	if strings.Contains(readConfigString(t, repoPath), "agent-marketplace/v3") {
		t.Fatalf("expected repo file to remain unmodified")
	}
}

func TestMigrateNormalizesExplicitHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
registries: []
`)

	if _, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, []string{"factory", " factory ", "factory"}); err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}

	userCfg, errs := loadV3Config([]byte(readConfigString(t, userPath)), userLayer, true)
	if len(errs) > 0 {
		t.Fatalf("expected valid user config: %v", errors.Join(errs...))
	}
	if !equalStrings(userCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected deduplicated and trimmed harnesses [factory], got %v", userCfg.Harnesses)
	}
}

func TestMigrateTransactionalFailureLeavesFilesIntact(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:repo.git
    ref: main
`)
	writeV2Config(t, localPath, `apiVersion: agent-marketplace/v2
identity:
  provider: ldap
`)

	originalUser := readConfigString(t, userPath)
	originalRepo := readConfigString(t, repoPath)
	originalLocal := readConfigString(t, localPath)

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath, Local: localPath}, nil)
	if !errors.Is(err, ErrLegacyIdentity) {
		t.Fatalf("expected ErrLegacyIdentity, got %v", err)
	}

	if readConfigString(t, userPath) != originalUser {
		t.Fatalf("expected user file to remain unchanged")
	}
	if readConfigString(t, repoPath) != originalRepo {
		t.Fatalf("expected repo file to remain unchanged")
	}
	if readConfigString(t, localPath) != originalLocal {
		t.Fatalf("expected local file to remain unchanged")
	}
}

func TestMigrateSkipsDuplicatePaths(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()

	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:repo.git
    ref: main
`)
	writeV2Config(t, localPath, `apiVersion: agent-marketplace/v2
harnesses:
  - claude
skills:
  include:
    - local-skill
`)

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: userPath, Local: localPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}
	if !result.UserMigrated {
		t.Fatalf("expected user to be migrated")
	}
	if result.RepoMigrated {
		t.Fatalf("expected repo to be skipped because it shares the user path")
	}
	if !result.LocalMigrated {
		t.Fatalf("expected local to be migrated")
	}

	userData := readConfigString(t, userPath)
	if !strings.Contains(userData, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected user config to be v3, got:\n%s", userData)
	}
	if !strings.Contains(userData, "harnesses:") {
		t.Fatalf("expected user harnesses to be preserved, got:\n%s", userData)
	}

	localData := readConfigString(t, localPath)
	if strings.Contains(localData, "harnesses:") {
		t.Fatalf("expected local harnesses to be removed, got:\n%s", localData)
	}
	if !strings.Contains(localData, "apiVersion: agent-marketplace/v3") {
		t.Fatalf("expected local config to be v3, got:\n%s", localData)
	}
}

func TestMigrateRejectsMultiDocumentBeforeCreatingUser(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n---\napiVersion: agent-marketplace/v2\nharnesses: [cursor]\n")
	writeV2Config(t, repoPath, string(original))

	if _, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, []string{"factory"}); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected multi-document rejection, got %v", err)
	}
	if got, err := os.ReadFile(repoPath); err != nil || string(got) != string(original) {
		t.Fatalf("multi-document input changed: got %q, err %v", got, err)
	}
	if _, err := os.Stat(userPath); !os.IsNotExist(err) {
		t.Fatalf("rejected migration created user config: %v", err)
	}
}

func TestMigrateRejectsMultiDocumentInEveryLayerByteIdentically(t *testing.T) {
	multiDocument := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n---\napiVersion: agent-marketplace/v2\nharnesses: [cursor]\n"
	for _, layer := range []string{"user", "repo", "local"} {
		t.Run(layer, func(t *testing.T) {
			root := t.TempDir()
			paths := MigratePaths{
				User:  filepath.Join(root, "home", ".agents", "config.yaml"),
				Repo:  filepath.Join(root, "repo", ".agents", "config.yaml"),
				Local: filepath.Join(root, "repo", ".agents", "config.local.yaml"),
			}
			path := map[string]string{"user": paths.User, "repo": paths.Repo, "local": paths.Local}[layer]
			writeV2Config(t, path, multiDocument)
			if layer == "local" {
				writeV2Config(t, paths.User, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
			}

			_, err := MigrateConfigFiles(paths, []string{"factory"})
			if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
				t.Fatalf("expected multi-document rejection, got %v", err)
			}
			if got := readConfigString(t, path); got != multiDocument {
				t.Fatalf("rejected %s config changed:\nbefore: %q\nafter:  %q", layer, multiDocument, got)
			}
		})
	}
}

func TestMigrateRejectsExplicitTrailingEmptyYAMLDocumentByteIdentically(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n---\n"
	writeV2Config(t, path, original)
	_, err := MigrateConfigFiles(MigratePaths{User: path}, []string{"factory"})
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected explicit empty document rejection, got %v", err)
	}
	if got := readConfigString(t, path); got != original {
		t.Fatalf("explicit empty document rejection changed bytes:\nbefore: %q\nafter:  %q", original, got)
	}
}

func TestMigrateAcceptsUnversionedLocalOnly(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeV2Config(t, userPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	originalLocal := []byte("# local-only schema\nskills:\n  include: [audit]\n")
	writeV2Config(t, localPath, string(originalLocal))

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Local: localPath}, nil)
	if err != nil {
		t.Fatalf("MigrateConfigFiles: %v", err)
	}
	if !result.UserMigrated || result.LocalMigrated {
		t.Fatalf("migration result = %+v", result)
	}
	if got, err := os.ReadFile(localPath); err != nil || string(got) != string(originalLocal) {
		t.Fatalf("unversioned local changed: got %q, err %v", got, err)
	}
}

func TestMigrateRejectsUnversionedUserAndRepoByteIdentically(t *testing.T) {
	for _, layer := range []string{"user", "repo"} {
		t.Run(layer, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".agents", "config.yaml")
			original := []byte("# v1\nregistries: []\n")
			writeV2Config(t, path, string(original))
			paths := MigratePaths{}
			if layer == "user" {
				paths.User = path
			} else {
				paths.Repo = path
			}
			if _, err := MigrateConfigFiles(paths, []string{"factory"}); err == nil || !strings.Contains(err.Error(), "v1") {
				t.Fatalf("expected v1 rejection, got %v", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
				t.Fatalf("rejected %s changed: got %q, err %v", layer, got, err)
			}
		})
	}
}

func TestMigrationPlanPreservesOrganizationFallbackWithoutUserOverride(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeV2Config(t, repoPath, "# repo\napiVersion: agent-marketplace/v2\nregistries: []\n")

	plan, err := PlanConfigMigration(MigratePaths{User: userPath, Repo: repoPath}, nil, []string{"factory"})
	if err != nil {
		t.Fatalf("PlanConfigMigration: %v", err)
	}
	if plan.TargetSource != MigrationTargetOrganization || !equalStrings(plan.TargetHarnesses, []string{"factory"}) {
		t.Fatalf("target plan = %s %v", plan.TargetSource, plan.TargetHarnesses)
	}
	if plan.CreatesUser {
		t.Fatal("organization fallback planned a user override")
	}
	if len(plan.Sources) != 2 || plan.Sources[0].Layer != "user" || plan.Sources[0].Exists || plan.Sources[0].Digest != "" || plan.Sources[1].Layer != "repo" || plan.Sources[1].Digest == "" {
		t.Fatalf("source digest preconditions = %+v", plan.Sources)
	}
	for _, file := range plan.Files {
		if file.Layer == "user" {
			t.Fatalf("organization fallback planned user write: %+v", file)
		}
	}
	result, err := ExecuteMigrationPlan(plan)
	if err != nil {
		t.Fatalf("ExecuteMigrationPlan: %v", err)
	}
	if result.UserMigrated || !result.RepoMigrated {
		t.Fatalf("migration result = %+v", result)
	}
	if _, err := os.Stat(userPath); !os.IsNotExist(err) {
		t.Fatalf("organization fallback created user config: %v", err)
	}
}

func TestMigrateThroughSymlinkPreservesLinkAndUpdatesTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "managed", "config.yaml")
	linkPath := filepath.Join(dir, "config.yaml")
	writeV2Config(t, targetPath, "# target\napiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateConfigFiles(MigratePaths{User: linkPath}, nil)
	if err != nil {
		t.Fatalf("MigrateConfigFiles: %v", err)
	}
	if !result.UserMigrated {
		t.Fatalf("migration result = %+v", result)
	}
	canonicalTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || filepath.Clean(result.Files[0].ResolvedPath) != filepath.Clean(canonicalTarget) {
		t.Fatalf("migration did not report canonical symlink target: %+v", result.Files)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("migration replaced symlink: info=%v err=%v", info, err)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "apiVersion: agent-marketplace/v3") || !strings.Contains(string(data), "# target") {
		t.Fatalf("symlink target was not migrated safely:\n%s", data)
	}
}

func TestExecuteMigrationPlanRejectsConcurrentEditBeforeWrite(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	originalUser := "# user\napiVersion: agent-marketplace/v2\nharnesses: [factory]\n"
	originalRepo := "# repo\napiVersion: agent-marketplace/v2\nregistries: []\n"
	writeV2Config(t, userPath, originalUser)
	writeV2Config(t, repoPath, originalRepo)
	plan, err := PlanConfigMigration(MigratePaths{User: userPath, Repo: repoPath}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	externalRepo := "# external edit\napiVersion: agent-marketplace/v2\nregistries: []\n"
	previousBarrier := migrationPublishBarrier
	migrationPublishBarrier = func(index int, _ *migrationFile) {
		if index == 0 {
			mustWriteFile(t, repoPath, []byte(externalRepo))
		}
	}
	t.Cleanup(func() { migrationPublishBarrier = previousBarrier })

	result, err := ExecuteMigrationPlan(plan)
	if !errors.Is(err, ErrMigrationSourceChanged) {
		t.Fatalf("expected source-change error, got result=%+v err=%v", result, err)
	}
	var commitErr *MigrationCommitError
	if !errors.As(err, &commitErr) || commitErr.Layer != "repo" || commitErr.Path != repoPath {
		t.Fatalf("source-change identity was not reported exactly: %v", err)
	}
	for _, digest := range []string{migrationDigest([]byte(originalRepo), true), migrationDigest([]byte(externalRepo), true)} {
		if !strings.Contains(err.Error(), digest) {
			t.Fatalf("source-change error omitted digest %s: %v", digest, err)
		}
	}
	if len(result.Files) != 2 || result.Files[0].Status != MigrationFilePending || result.Files[1].Status != MigrationFileDiverged {
		t.Fatalf("source conflict file outcomes = %+v", result.Files)
	}
	if got := readConfigString(t, userPath); got != originalUser {
		t.Fatalf("concurrent edit failure changed user config: %q", got)
	}
	if got := readConfigString(t, repoPath); got != externalRepo {
		t.Fatalf("concurrent edit was overwritten: %q", got)
	}
	if result.UserMigrated || result.RepoMigrated || result.Partial {
		t.Fatalf("source conflict reported a migration write: %+v", result)
	}
}

func TestAcquireMigrationLocksUsesDeterministicCanonicalOrder(t *testing.T) {
	root := t.TempDir()
	files := map[string]*migrationFile{
		"user":  {resolvedPath: filepath.Join(root, "z", "config.yaml")},
		"repo":  {resolvedPath: filepath.Join(root, "a", "config.yaml")},
		"local": {resolvedPath: filepath.Join(root, "m", "config.yaml")},
	}
	locks, err := acquireMigrationLocks(files)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseMigrationLocks(locks)
	want := []string{
		filepath.Join(root, "a", "config.yaml.agent-marketplace.lock"),
		filepath.Join(root, "m", "config.yaml.agent-marketplace.lock"),
		filepath.Join(root, "z", "config.yaml.agent-marketplace.lock"),
	}
	if len(locks) != len(want) {
		t.Fatalf("locks = %+v", locks)
	}
	for i := range want {
		if locks[i].path != want[i] {
			t.Fatalf("lock %d = %q, want %q", i, locks[i].path, want[i])
		}
	}
}

func TestExecuteMigrationPlanPreservesConcurrentEditAfterFirstPublishAndReportsExactState(t *testing.T) {
	paths, originals := migrationThreeFileFixture(t)
	plan, err := PlanConfigMigration(paths, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	externalRepo := "# concurrent repo edit\napiVersion: agent-marketplace/v2\nregistries: []\n"
	previousBarrier := migrationPublishBarrier
	migrationPublishBarrier = func(index int, _ *migrationFile) {
		if index == 1 {
			mustWriteFile(t, paths.Repo, []byte(externalRepo))
		}
	}
	t.Cleanup(func() { migrationPublishBarrier = previousBarrier })

	result, err := ExecuteMigrationPlan(plan)
	var commitErr *MigrationCommitError
	if !errors.As(err, &commitErr) || !errors.Is(err, ErrMigrationSourceChanged) {
		t.Fatalf("expected structured source-change error, got result=%+v err=%v", result, err)
	}
	if commitErr.Layer != "repo" || commitErr.Path != paths.Repo {
		t.Fatalf("source-change identity = %s %q, want repo %q", commitErr.Layer, commitErr.Path, paths.Repo)
	}
	if result.Partial || !result.RolledBack || result.UserMigrated || result.RepoMigrated || result.LocalMigrated {
		t.Fatalf("concurrent-edit rollback result = %+v", result)
	}
	wantStatuses := []MigrationFileStatus{MigrationFileRolledBack, MigrationFileDiverged, MigrationFilePending}
	if len(result.Files) != len(wantStatuses) {
		t.Fatalf("file outcomes = %+v", result.Files)
	}
	for i, want := range wantStatuses {
		if result.Files[i].Status != want {
			t.Fatalf("file outcome %d = %+v, want status %s", i, result.Files[i], want)
		}
	}
	if len(commitErr.Files) != len(result.Files) {
		t.Fatalf("commit error omitted exact file state: error=%+v result=%+v", commitErr.Files, result.Files)
	}
	for i := range result.Files {
		if commitErr.Files[i] != result.Files[i] {
			t.Fatalf("commit error file %d = %+v, result = %+v", i, commitErr.Files[i], result.Files[i])
		}
	}
	if got := readConfigString(t, paths.User); got != originals[paths.User] {
		t.Fatalf("published user config was not rolled back: %q", got)
	}
	if got := readConfigString(t, paths.Repo); got != externalRepo {
		t.Fatalf("concurrent repo edit was overwritten: %q", got)
	}
	if got := readConfigString(t, paths.Local); got != originals[paths.Local] {
		t.Fatalf("untouched local config changed: %q", got)
	}
	assertNoMigrationTemps(t, paths)
}

func TestMigrationRollbackOnSecondAndThirdPublishFailure(t *testing.T) {
	for _, failAt := range []int{2, 3} {
		t.Run(fmt.Sprintf("rename-%d", failAt), func(t *testing.T) {
			paths, originals := migrationThreeFileFixture(t)
			plan, err := PlanConfigMigration(paths, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			previousRename := migrationRename
			calls := 0
			migrationRename = func(oldPath, newPath string) error {
				calls++
				if calls == failAt {
					return errors.New("injected publish failure")
				}
				return os.Rename(oldPath, newPath)
			}
			t.Cleanup(func() { migrationRename = previousRename })

			result, err := ExecuteMigrationPlan(plan)
			var commitErr *MigrationCommitError
			if !errors.As(err, &commitErr) || commitErr.Phase != "publish" {
				t.Fatalf("expected structured publish error, got result=%+v err=%v", result, err)
			}
			if !result.RolledBack || result.Partial || result.UserMigrated || result.RepoMigrated || result.LocalMigrated {
				t.Fatalf("rollback result = %+v", result)
			}
			wantStatuses := map[int][]MigrationFileStatus{
				2: {MigrationFileRolledBack, MigrationFileUnchanged, MigrationFilePending},
				3: {MigrationFileRolledBack, MigrationFileRolledBack, MigrationFileUnchanged},
			}[failAt]
			if len(result.Files) != len(wantStatuses) {
				t.Fatalf("file outcomes = %+v", result.Files)
			}
			for i, want := range wantStatuses {
				if result.Files[i].Status != want {
					t.Fatalf("file outcome %d = %+v, want status %s", i, result.Files[i], want)
				}
			}
			if len(commitErr.Files) != len(result.Files) {
				t.Fatalf("commit error omitted exact rollback state: %+v", commitErr)
			}
			assertMigrationFixtureBytes(t, paths, originals)
			assertNoMigrationTemps(t, paths)
		})
	}
}

func TestMigrationReportsStructuredPartialWhenRollbackFails(t *testing.T) {
	paths, _ := migrationThreeFileFixture(t)
	plan, err := PlanConfigMigration(paths, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	previousRename := migrationRename
	calls := 0
	migrationRename = func(oldPath, newPath string) error {
		calls++
		if calls == 2 || calls == 3 {
			return errors.New("injected publish or rollback failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { migrationRename = previousRename })

	result, err := ExecuteMigrationPlan(plan)
	var commitErr *MigrationCommitError
	if !errors.As(err, &commitErr) || !commitErr.Partial || len(commitErr.RollbackErrors) == 0 {
		t.Fatalf("expected precise partial commit error, got result=%+v err=%v", result, err)
	}
	if !result.Partial || result.RolledBack || !result.UserMigrated || result.RepoMigrated || result.LocalMigrated {
		t.Fatalf("partial result = %+v", result)
	}
	wantStatuses := []MigrationFileStatus{MigrationFileRollbackFailed, MigrationFileUnchanged, MigrationFilePending}
	if len(result.Files) != len(wantStatuses) {
		t.Fatalf("partial file outcomes = %+v", result.Files)
	}
	for i, want := range wantStatuses {
		if result.Files[i].Status != want {
			t.Fatalf("partial file outcome %d = %+v, want status %s", i, result.Files[i], want)
		}
	}
	if len(commitErr.Files) != len(result.Files) {
		t.Fatalf("partial error omitted exact file outcomes: %+v", commitErr)
	}
	for _, want := range []string{"user=rollback-failed", "repo=unchanged", "local=pending", "rollback failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("partial error omitted %q: %v", want, err)
		}
	}
	assertNoMigrationTemps(t, paths)
}

func migrationThreeFileFixture(t *testing.T) (MigratePaths, map[string]string) {
	t.Helper()
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	paths := MigratePaths{
		User:  filepath.Join(homeDir, ".agents", "config.yaml"),
		Repo:  filepath.Join(repoRoot, ".agents", "config.yaml"),
		Local: filepath.Join(repoRoot, ".agents", "config.local.yaml"),
	}
	originals := map[string]string{
		paths.User:  "# user\napiVersion: agent-marketplace/v2\nharnesses: [factory]\n",
		paths.Repo:  "# repo\napiVersion: agent-marketplace/v2\nharnesses: [cursor]\nregistries: []\n",
		paths.Local: "# local\napiVersion: agent-marketplace/v2\nharnesses: [gemini]\nskills:\n  include: [audit]\n",
	}
	for path, content := range originals {
		writeV2Config(t, path, content)
	}
	return paths, originals
}

func assertMigrationFixtureBytes(t *testing.T, paths MigratePaths, originals map[string]string) {
	t.Helper()
	for _, path := range []string{paths.User, paths.Repo, paths.Local} {
		if got := readConfigString(t, path); got != originals[path] {
			t.Fatalf("migration did not restore %q:\nbefore: %q\nafter:  %q", path, originals[path], got)
		}
	}
}

func assertNoMigrationTemps(t *testing.T, paths MigratePaths) {
	t.Helper()
	for _, path := range []string{paths.User, paths.Repo, paths.Local} {
		for _, pattern := range []string{".*.migration-new-*", ".*.migration-old-*"} {
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), pattern))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("migration temp files remain: %v", matches)
			}
		}
	}
}

func TestMigrateAtomicWriteLeavesFilesIntactOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory permission test is Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("root can write to read-only directories")
	}

	homeDir := t.TempDir()
	repoRoot := t.TempDir()

	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")

	writeV2Config(t, userPath, `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`)
	writeV2Config(t, repoPath, `apiVersion: agent-marketplace/v2
harnesses:
  - claude
backup:
  enabled: true
registries:
  - url: git@example.com:repo.git
    ref: main
`)

	originalUser := readConfigString(t, userPath)
	originalRepo := readConfigString(t, repoPath)

	// Make the repo config directory read-only so the temp file cannot be
	// created. The user file must not be modified when the repo write fails.
	if err := os.Chmod(filepath.Dir(repoPath), 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() {
		_ = os.Chmod(filepath.Dir(repoPath), 0o755)
	}()

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err == nil {
		t.Fatalf("expected migration to fail for read-only repo directory")
	}

	if readConfigString(t, userPath) != originalUser {
		t.Fatalf("expected user file to remain unchanged when repo write fails")
	}
	if readConfigString(t, repoPath) != originalRepo {
		t.Fatalf("expected repo file to remain unchanged when repo write fails")
	}
}
