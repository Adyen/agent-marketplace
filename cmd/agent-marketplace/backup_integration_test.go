package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestBackupCreatedOnInstall(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "backup-test-skill", "A skill for backup testing", []string{"test"})
	commitAndPushAll(t, working, "Add backup test skill")

	// User config with registry.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// Install with --scope user so the skill is machine-scoped.
	output, err := executeCommand("install", "--scope", "user", "backup-test-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	// First install doesn't create a backup (nothing to back up before materialization).
	// Run a second sync to capture the current state.
	backupsDir := filepath.Join(homeDir, ".agent-marketplace", "backups")
	if err := os.Remove(filepath.Join(backupsDir, ".last_backup")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove .last_backup: %v", err)
	}
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// Verify backup archive was created.
	archives, err := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if err != nil || len(archives) == 0 {
		t.Fatalf("expected backup archive in %s/factory/, got %v (err=%v)", backupsDir, archives, err)
	}
	t.Logf("Backup archive: %s", archives[0])
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "roundtrip-skill", "Skill for round-trip testing", []string{"test"})
	commitAndPushAll(t, working, "Add roundtrip skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// First install — sets up the initial state.
	output, err := executeCommand("install", "--scope", "user", "roundtrip-skill")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}

	// Read the original skill content from the machine-scope dir.
	factoryMachineDir := filepath.Join(homeDir, ".factory")
	skillLink := filepath.Join(factoryMachineDir, "skills", "roundtrip-skill")
	skillMD := filepath.Join(skillLink, "SKILL.md")
	originalContent, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read original skill: %v", err)
	}

	// Sync to capture the current state in a backup.
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// Corrupt: remove the symlink and replace with a regular dir containing bad content.
	if err := os.Remove(skillLink); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove skill link: %v", err)
	}
	if err := os.MkdirAll(skillLink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMD, []byte("# Corrupted\n"), 0o644); err != nil {
		t.Fatalf("corrupt skill: %v", err)
	}

	// Restore from backup.
	restoreOutput, err := executeCommand("backup", "restore", "--target", "factory")
	if err != nil {
		t.Fatalf("backup restore failed: %v\n%s", err, restoreOutput)
	}
	t.Logf("Restore output: %s", restoreOutput)

	// Verify the skill content was restored.
	if info, err := os.Lstat(skillLink); err != nil {
		t.Fatalf("lstat skill link after restore: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink after restore, got mode %v", info.Mode())
	}
	restoredContent, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read restored skill: %v", err)
	}
	if string(restoredContent) != string(originalContent) {
		t.Fatalf("restored content mismatch:\noriginal: %q\nrestored: %q", string(originalContent), string(restoredContent))
	}
}

func TestBackupListCommand(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "list-test-skill", "Skill for list testing", []string{"test"})
	commitAndPushAll(t, working, "Add list test skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// Install with --scope user to set up state, then sync to create backup.
	output, err := executeCommand("install", "--scope", "user", "list-test-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// List backups.
	listOutput, err := executeCommand("backup", "list")
	if err != nil {
		t.Fatalf("backup list failed: %v\n%s", err, listOutput)
	}
	t.Logf("List output: %s", listOutput)

	if !strings.Contains(listOutput, "factory") {
		t.Fatalf("expected 'factory' in backup list output, got: %s", listOutput)
	}
}

func TestSyncNoBackupFlag(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "no-backup-skill", "Skill for --no-backup testing", []string{"test"})
	commitAndPushAll(t, working, "Add no-backup skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      include:
        - no-backup-skill
`)
	withWorkingDir(t, repoRoot)

	factoryMachineDir := filepath.Join(homeDir, ".factory")
	if err := os.MkdirAll(filepath.Join(factoryMachineDir, "skills", "existing-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryMachineDir, "skills", "existing-skill", "SKILL.md"), []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("install", "--scope", "user", "no-backup-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	backupsDir := filepath.Join(homeDir, ".agent-marketplace", "backups")
	if err := backup.Create(map[hcc.Target]backup.ManagedPaths{
		hcc.TargetFactory: {Root: factoryMachineDir, RelPaths: []string{"skills"}},
	}); err != nil {
		t.Fatal(err)
	}
	initialArchives, err := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(initialArchives) == 0 {
		t.Fatal("expected initial backup create to produce an archive")
	}

	syncOutput, err := executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync --no-backup failed: %v\n%s", err, syncOutput)
	}

	archives, err := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != len(initialArchives) {
		t.Fatalf("expected --no-backup to leave archive count unchanged: before=%d after=%d", len(initialArchives), len(archives))
	}
}

func TestBackupOnlyIncludesManagedPaths(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "managed-skill", "Skill for managed-paths testing", []string{"test"})
	commitAndPushAll(t, working, "Add managed skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// Create a user-owned file in the machine-scope factory dir BEFORE install.
	factoryMachineDir := filepath.Join(homeDir, ".factory")
	if err := os.MkdirAll(factoryMachineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryMachineDir, "personal.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install with --scope user for machine-scope materialization.
	output, err := executeCommand("install", "--scope", "user", "managed-skill")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}

	// Sync so the backup captures the current state.
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// Corrupt the entire factory dir (remove symlinks first so we don't corrupt cache).
	// Remove the symlink and replace with empty dir.
	skillLink := filepath.Join(factoryMachineDir, "skills", "managed-skill")
	if err := os.Remove(skillLink); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove skill link: %v", err)
	}
	if err := os.RemoveAll(factoryMachineDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(factoryMachineDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Restore from backup.
	restoreOutput, err := executeCommand("backup", "restore", "--target", "factory")
	if err != nil {
		t.Fatalf("backup restore failed: %v\n%s", err, restoreOutput)
	}

	// Managed skill should be restored.
	if _, err := os.Stat(filepath.Join(factoryMachineDir, "skills", "managed-skill", "SKILL.md")); err != nil {
		t.Fatal("managed skill should have been restored")
	}

	// User-owned file should NOT be restored (wasn't backed up).
	if _, err := os.Stat(filepath.Join(factoryMachineDir, "personal.txt")); !os.IsNotExist(err) {
		t.Fatal("personal.txt should NOT have been restored (not a managed path)")
	}
}

func TestBackupDisabledViaConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "disabled-skill", "Skill for disabled backup testing", []string{"test"})
	commitAndPushAll(t, working, "Add disabled skill")

	// User config with backup disabled.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
backup:
  enabled: false
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "disabled-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	backupsDir := filepath.Join(homeDir, ".agent-marketplace", "backups")
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// Verify NO backup was created.
	archives, _ := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if len(archives) > 0 {
		t.Fatalf("expected no backups when disabled, got %d", len(archives))
	}
}

func TestBackupRestoreNonexistentTarget(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	output, err := executeCommand("backup", "restore", "--target", "factory")
	if err == nil {
		t.Fatalf("expected error restoring nonexistent backup, got: %s", output)
	}
	if !strings.Contains(output, "no backups found") {
		t.Fatalf("expected 'no backups found' in error, got: %s", output)
	}
}

func TestBackupRestoreRequiresTargetFlag(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	output, err := executeCommand("backup", "restore")
	if err == nil {
		t.Fatalf("expected error when --target is missing, got: %s", output)
	}
	if !strings.Contains(output, "required flag") && !strings.Contains(output, "target") {
		t.Fatalf("expected missing target flag error, got: %s", output)
	}
}

func TestBackupPreservesSymlinks(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "symlink-skill", "Skill for symlink testing", []string{"test"})
	commitAndPushAll(t, working, "Add symlink skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "symlink-skill")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}

	// Sync so the backup captures the current state.
	syncOutput, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, syncOutput)
	}

	// The skill dir in ~/.factory/skills/symlink-skill should be a symlink.
	factoryMachineDir := filepath.Join(homeDir, ".factory")
	skillLink := filepath.Join(factoryMachineDir, "skills", "symlink-skill")
	linkTarget, err := os.Readlink(skillLink)
	if err != nil {
		t.Fatalf("skill dir should be a symlink: %v", err)
	}
	t.Logf("Skill symlink target: %s", linkTarget)

	// Read original content through the symlink.
	skillMD := filepath.Join(skillLink, "SKILL.md")
	originalContent, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read original skill: %v", err)
	}

	// Corrupt by removing the symlink and replacing with a file.
	if err := os.Remove(skillLink); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove skill link: %v", err)
	}
	if err := os.MkdirAll(skillLink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMD, []byte("# Corrupted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore.
	restoreOutput, err := executeCommand("backup", "restore", "--target", "factory")
	if err != nil {
		t.Fatalf("backup restore failed: %v\n%s", err, restoreOutput)
	}

	// Verify the skill is a symlink again.
	restoredTarget, err := os.Readlink(skillLink)
	if err != nil {
		t.Fatalf("restored skill dir should be a symlink: %v", err)
	}
	// Resolve symlink targets: if relative, resolve from the symlink's directory.
	resolveSymlinkTarget := func(baseDir, target string) (string, error) {
		if filepath.IsAbs(target) {
			return filepath.EvalSymlinks(target)
		}
		return filepath.EvalSymlinks(filepath.Join(baseDir, target))
	}
	baseDir := filepath.Dir(skillLink)
	resolvedOriginal, err := resolveSymlinkTarget(baseDir, linkTarget)
	if err != nil {
		t.Fatalf("eval original symlink: %v", err)
	}
	resolvedRestored, err := resolveSymlinkTarget(baseDir, restoredTarget)
	if err != nil {
		t.Fatalf("eval restored symlink: %v", err)
	}
	if resolvedRestored != resolvedOriginal {
		t.Fatalf("symlink target changed: was %q, now %q", resolvedOriginal, resolvedRestored)
	}

	// Verify content is restored.
	restoredContent, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read restored skill: %v", err)
	}
	if string(restoredContent) != string(originalContent) {
		t.Fatalf("restored content mismatch:\noriginal: %q\nrestored: %q", string(originalContent), string(restoredContent))
	}
}

// Ensure the backup package's types are used to avoid "unused import" errors
// in case the compiler is strict about this.
var _ = hcc.TargetFactory
var _ = backup.ManagedPaths{}

func TestBackupCreateOnDemand(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "on-demand-skill", "Skill for on-demand backup testing", []string{"test"})
	commitAndPushAll(t, working, "Add on-demand skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	// Install a skill so there's something to back up.
	output, err := executeCommand("install", "--scope", "user", "on-demand-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	backupsDir := filepath.Join(homeDir, ".agent-marketplace", "backups")

	// No backup should exist yet (install doesn't create one).
	archives, _ := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if len(archives) > 0 {
		t.Fatalf("expected no backups before 'backup create', got %d", len(archives))
	}

	// Run on-demand backup create.
	createOutput, err := executeCommand("backup", "create")
	if err != nil {
		t.Fatalf("backup create failed: %v\n%s", err, createOutput)
	}
	if !strings.Contains(createOutput, "Backup created for:") {
		t.Fatalf("expected success message in output, got: %s", createOutput)
	}

	// Verify backup archive was created.
	archives, err = filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if err != nil || len(archives) == 0 {
		t.Fatalf("expected backup archive after 'backup create', got %v (err=%v)", archives, err)
	}
}

func TestBackupCreateWhenDisabled(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "skills.git")
	writeSkill(t, working, "disabled-create-skill", "Skill for disabled create testing", []string{"test"})
	commitAndPushAll(t, working, "Add disabled-create skill")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
backup:
  enabled: false
registries:
  - url: `+origin+`
    ref: main
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--scope", "user", "disabled-create-skill")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	createOutput, err := executeCommand("backup", "create")
	if err != nil {
		t.Fatalf("backup create should not error when disabled: %v\n%s", err, createOutput)
	}
	if !strings.Contains(createOutput, "disabled") {
		t.Fatalf("expected 'disabled' message, got: %s", createOutput)
	}

	backupsDir := filepath.Join(homeDir, ".agent-marketplace", "backups")
	archives, _ := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if len(archives) > 0 {
		t.Fatalf("expected no backups when disabled, got %d", len(archives))
	}
}

func TestBackupCreateNoManagedPaths(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// No config at all — resolve will have nothing to back up.
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	createOutput, err := executeCommand("backup", "create")
	if err != nil {
		t.Fatalf("backup create should not error with no managed paths: %v\n%s", err, createOutput)
	}
	if !strings.Contains(createOutput, "No managed paths") {
		t.Fatalf("expected 'No managed paths' message, got: %s", createOutput)
	}
}
