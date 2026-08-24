package config

import (
	"errors"
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

func TestMigrateV2UserAndRepo(t *testing.T) {
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
	if result.UserMigrated {
		t.Fatalf("expected valid user config to be unchanged: %+v", result)
	}
	if result.RepoMigrated {
		t.Fatalf("expected valid repository config to be unchanged: %+v", result)
	}

	userData := readConfigString(t, userPath)
	if !strings.Contains(userData, "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected user config to remain v2, got:\n%s", userData)
	}
	if !strings.Contains(userData, "harnesses:") {
		t.Fatalf("expected user harnesses to be preserved, got:\n%s", userData)
	}

	repoData := readConfigString(t, repoPath)
	if !strings.Contains(repoData, "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected repo config to remain v2, got:\n%s", repoData)
	}
	if !strings.Contains(repoData, "harnesses:") || !strings.Contains(repoData, "- factory") {
		t.Fatalf("expected repo harnesses to be preserved, got:\n%s", repoData)
	}
}

func TestMigratePreservesUserAndRepoHarnesses(t *testing.T) {
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
	if !equalStrings(userCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected user harnesses preserved as [factory], got %v", userCfg.Harnesses)
	}

	repoCfg, errs := loadV3Config([]byte(readConfigString(t, repoPath)), repoLayer, false)
	if len(errs) > 0 {
		t.Fatalf("expected valid migrated repo config: %v", errors.Join(errs...))
	}
	if !equalStrings(repoCfg.Harnesses, []string{"claude"}) {
		t.Fatalf("expected repo harnesses preserved as [claude], got %v", repoCfg.Harnesses)
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
	if result.UserMigrated || result.RepoMigrated {
		t.Fatalf("expected project-only config to remain unchanged: %+v", result)
	}
	if _, err := os.Stat(userPath); !os.IsNotExist(err) {
		t.Fatalf("expected project harnesses not to create a user config, stat error: %v", err)
	}
	if !strings.Contains(readConfigString(t, repoPath), "- windsurf") {
		t.Fatalf("expected project harnesses to be preserved")
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

func TestMigrateStripsRepoBackupAndLogging(t *testing.T) {
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

	_, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("expected migration to succeed: %v", err)
	}

	repoData := readConfigString(t, repoPath)
	if strings.Contains(repoData, "backup:") || strings.Contains(repoData, "logging:") {
		t.Fatalf("expected repo backup and logging to be removed, got:\n%s", repoData)
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
	if !strings.Contains(localData, "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected local config to be v2, got:\n%s", localData)
	}
	if strings.Contains(localData, "harnesses:") {
		t.Fatalf("expected local harnesses to be removed, got:\n%s", localData)
	}

	if _, errs := loadV3LocalConfig([]byte(localData)); len(errs) > 0 {
		t.Fatalf("expected migrated local config to be valid: %v", errors.Join(errs...))
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
	if result.UserMigrated || result.RepoMigrated {
		t.Fatalf("expected valid v2 configs to remain unchanged: %+v", result)
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
	if result.UserMigrated {
		t.Fatalf("expected user config to remain unchanged")
	}
	if result.RepoMigrated {
		t.Fatalf("expected repo to be skipped because it shares the user path")
	}
	if !result.LocalMigrated {
		t.Fatalf("expected local to be migrated")
	}

	userData := readConfigString(t, userPath)
	if !strings.Contains(userData, "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected user config to remain v2, got:\n%s", userData)
	}
	if !strings.Contains(userData, "harnesses:") {
		t.Fatalf("expected user harnesses to be preserved, got:\n%s", userData)
	}

	localData := readConfigString(t, localPath)
	if strings.Contains(localData, "harnesses:") {
		t.Fatalf("expected local harnesses to be removed, got:\n%s", localData)
	}
	if !strings.Contains(localData, "apiVersion: agent-marketplace/v2") {
		t.Fatalf("expected local config to be v2, got:\n%s", localData)
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
