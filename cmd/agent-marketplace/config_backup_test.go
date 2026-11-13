package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

type configBackupFixture struct {
	home, working, configPath, lockPath, skillPath string
}

func newConfigBackupFixture(t *testing.T, extra string) configBackupFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "absent-org.yaml"))
	origin, working := createBareRepoWithWorkingClone(t, "config-backup-skills.git")
	writeSkill(t, working, "config-backup-skill", "Original backup skill", []string{"test"})
	commitAndPushAll(t, working, "Add config backup skill")
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, configPath, fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
%s
sources:
  skills:
    git: file://%s
    ref: main
`, extra, origin))
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  skills:\n    git: file://%s\n    ref: main\n", origin))
	withWorkingDir(t, repo)
	mustConfigBackupCommand(t, "install", "--scope", "user", "config-backup-skill")
	clearConfigArchives(t, home)
	return configBackupFixture{
		home: home, working: working, configPath: configPath,
		lockPath:  filepath.Join(home, ".agents", "lock.yaml"),
		skillPath: filepath.Join(home, ".factory", "skills", "config-backup-skill", "SKILL.md"),
	}
}

func updateConfigBackupFixtureRef(t *testing.T, f configBackupFixture) {
	t.Helper()
	runGit(t, f.working, "checkout", "-b", "updated")
	writeSkill(t, f.working, "config-backup-skill", "Updated backup skill", []string{"test"})
	commitAndPushAllToBranch(t, f.working, "Update config backup skill", "updated")
	configBytes, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := bytes.Replace(configBytes, []byte("ref: main"), []byte("ref: updated"), 1)
	if bytes.Equal(updated, configBytes) {
		t.Fatal("user config has no ref: main entry to update")
	}
	if err := os.WriteFile(f.configPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
}

func configArchivePaths(t *testing.T, home string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "agents-config", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func clearConfigArchives(t *testing.T, home string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(home, ".agent-marketplace", "backups", "agents-config")); err != nil {
		t.Fatal(err)
	}
}

func oneConfigArchive(t *testing.T, home string) string {
	t.Helper()
	paths := configArchivePaths(t, home)
	if len(paths) != 1 {
		t.Fatalf("configuration archives = %v, want one", paths)
	}
	return paths[0]
}

func readConfigArchive(t *testing.T, archive string) map[string][]byte {
	t.Helper()
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			t.Error(err)
		}
	}()
	tr, files := tar.NewReader(gz), make(map[string][]byte)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatalf("archive entry %q has type %d, want regular", header.Name, header.Typeflag)
		}
		files[header.Name], err = io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func mustConfigBackupCommand(t *testing.T, args ...string) string {
	t.Helper()
	output, err := executeCommand(args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, output)
	}
	return output
}

// spec: backup-recovery/missing-user-configuration-files-are-left-out
// spec: backup-recovery/configuration-archive-keeps-the-lock-from-before-the-sync
func TestSyncCapturesMissingFilesAndCommandStartLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newConfigBackupFixture(t, "")
	configBytes, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.lockPath); err != nil {
		t.Fatal(err)
	}
	mustConfigBackupCommand(t, "sync")
	contents := readConfigArchive(t, oneConfigArchive(t, f.home))
	if len(contents) != 1 || !bytes.Equal(contents["config.yaml"], configBytes) {
		t.Fatalf("missing-lock archive has entries %v, want config.yaml only", contents)
	}

	clearConfigArchives(t, f.home)
	beforeLock, err := os.ReadFile(f.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	updateConfigBackupFixtureRef(t, f)
	mustConfigBackupCommand(t, "sync")
	afterLock, err := os.ReadFile(f.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(afterLock, beforeLock) {
		t.Fatal("sync did not rewrite lock.yaml")
	}
	contents = readConfigArchive(t, oneConfigArchive(t, f.home))
	if !bytes.Equal(contents["lock.yaml"], beforeLock) {
		t.Fatalf("archived lock = %q, want command-start bytes %q", contents["lock.yaml"], beforeLock)
	}
	if got, err := os.ReadFile(f.skillPath); err != nil || !bytes.Contains(got, []byte("Updated backup skill")) {
		t.Fatalf("sync did not materialize changed skill: %q (err=%v)", got, err)
	}
}

// spec: backup-recovery/no-configuration-archive-without-user-configuration-files
func TestSyncWithoutUserConfigFilesStillBacksUpHarnesses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin, working := createBareRepoWithWorkingClone(t, "organization-config-backup-skills.git")
	writeSkill(t, working, "organization-backup-skill", "Organization backup skill", []string{"test"})
	commitAndPushAll(t, working, "Add organization backup skill")
	orgConfig := filepath.Join(t.TempDir(), "organization.yaml")
	writeFile(t, orgConfig, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  skills:\n    git: file://%s\n    ref: main\nresources:\n  skills: [skill/organization-backup-skill]\n", origin))
	config.SetTestOrganizationConfigPath(t, orgConfig)
	requireOrganizationLock(t, orgConfig)
	withWorkingDir(t, t.TempDir())
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	mustConfigBackupCommand(t, "sync")
	if archives := configArchivePaths(t, home); len(archives) != 1 {
		t.Fatalf("initial sync configuration archives = %v, want one while user config exists", archives)
	}
	clearConfigArchives(t, home)
	if err := os.RemoveAll(filepath.Join(home, ".agents")); err != nil {
		t.Fatal(err)
	}
	mustConfigBackupCommand(t, "sync")
	harnessArchives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "factory", "*.tar.gz"))
	if err != nil || len(harnessArchives) == 0 {
		t.Fatalf("sync did not create a harness backup: %v (err=%v)", harnessArchives, err)
	}
	if archives := configArchivePaths(t, home); len(archives) != 0 {
		t.Fatalf("configuration archives = %v, want none", archives)
	}
}

// spec: backup-recovery/restore-refuses-the-configuration-archive-target
func TestBackupRestoreRefusesConfigurationArchiveWithoutChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir, backupsDir := filepath.Join(home, ".agents"), filepath.Join(home, ".agent-marketplace", "backups")
	archiveDir := filepath.Join(backupsDir, "agents-config")
	writeFile(t, filepath.Join(userDir, "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	writeFile(t, filepath.Join(archiveDir, "2026-11-13-010203.tar.gz"), "archive bytes")
	beforeUser, beforeBackups := snapshotBackupTree(t, userDir), snapshotBackupTree(t, backupsDir)
	output, err := executeCommand("backup", "restore", "--target", "agents-config")
	message := output
	if err != nil {
		message = err.Error() + "\n" + message
	}
	if err == nil || !strings.Contains(message, "tar -xzf") || !strings.Contains(message, "are not restored by backup restore") {
		t.Fatalf("restore did not refuse manual config extraction: %v\n%s", err, output)
	}
	if after := snapshotBackupTree(t, userDir); !reflect.DeepEqual(beforeUser, after) {
		t.Fatalf("restore changed ~/.agents: before=%v after=%v", beforeUser, after)
	}
	if after := snapshotBackupTree(t, backupsDir); !reflect.DeepEqual(beforeBackups, after) {
		t.Fatalf("restore changed backups: before=%v after=%v", beforeBackups, after)
	}
}

// spec: backup-recovery/read-only-modes-write-no-configuration-archive
func TestReadOnlyModesDoNotWriteConfigurationArchives(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newConfigBackupFixture(t, "")
	mustConfigBackupCommand(t, "sync")
	before := configArchivePaths(t, f.home)
	if len(before) == 0 {
		t.Fatal("initial sync did not create a configuration archive")
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}, {"lock", "--check"}} {
		mustConfigBackupCommand(t, args...)
		if after := configArchivePaths(t, f.home); !slices.Equal(before, after) {
			t.Fatalf("%v changed configuration archives: before=%v after=%v", args, before, after)
		}
	}
}

// spec: backup-recovery/configuration-archive-failure-leaves-harness-files-unchanged
func TestConfigurationArchiveFailureLeavesHarnessFilesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newConfigBackupFixture(t, "")
	before, err := os.ReadFile(f.skillPath)
	if err != nil {
		t.Fatal(err)
	}
	updateConfigBackupFixtureRef(t, f)
	backupsDir := filepath.Join(f.home, ".agent-marketplace", "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupsDir, "agents-config"), []byte("block archive directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("sync")
	if err == nil || !strings.Contains(err.Error()+"\n"+output, "backup agents-config") {
		t.Fatalf("sync did not report the config backup failure: %v\n%s", err, output)
	}
	after, err := os.ReadFile(f.skillPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("harness skill changed after failed backup: before=%q after=%q err=%v", before, after, err)
	}
}

func snapshotBackupTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel], err = os.ReadFile(path)
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return result
}
