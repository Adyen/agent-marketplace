package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// makeArchive creates a zero-byte stub archive at the given path.
func makeArchive(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndRestore(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a fake machine-scope harness directory with managed content.
	srcDir := filepath.Join(tmpDir, "factory")
	skillDir := filepath.Join(srcDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Test Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a symlink under skills/ to test symlink preservation.
	linkPath := filepath.Join(skillDir, "link-to-skill.md")
	if err := os.Symlink("SKILL.md", linkPath); err != nil {
		t.Fatal(err)
	}
	// Create a root-level mcp.json config file.
	if err := os.WriteFile(filepath.Join(srcDir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a user-owned file at root that should NOT be backed up.
	if err := os.WriteFile(filepath.Join(srcDir, "user-notes.txt"), []byte("personal"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override home dir so backups go into tmpDir.
	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	t.Setenv("HOME", tmpDir)

	// Create backup of only managed paths (skills dir + mcp.json).
	managed := map[hcc.Target]ManagedPaths{
		hcc.TargetFactory: {Root: srcDir, RelPaths: []string{"skills", "commands", "droids", "hooks", ".factory-plugin", "mcp.json", "settings.json"}},
	}
	if err := Create(managed); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify archive exists.
	archives, err := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if err != nil || len(archives) == 0 {
		t.Fatalf("expected archive in %s/factory/, got %v (err=%v)", backupsDir, archives, err)
	}

	// Restore to a new directory.
	restoreDir := filepath.Join(tmpDir, "restored")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	timestamp, err := Restore(hcc.TargetFactory, restoreDir)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if timestamp == "" {
		t.Fatal("expected non-empty timestamp")
	}

	// Verify restored skill content.
	restoredSkill := filepath.Join(restoreDir, "skills", "test-skill", "SKILL.md")
	data, err := os.ReadFile(restoredSkill)
	if err != nil {
		t.Fatalf("read restored skill: %v", err)
	}
	if string(data) != "# Test Skill\n" {
		t.Fatalf("expected '# Test Skill\\n', got %q", string(data))
	}

	// Verify symlink was restored.
	restoredLink := filepath.Join(restoreDir, "skills", "test-skill", "link-to-skill.md")
	linkDst, err := os.Readlink(restoredLink)
	if err != nil {
		t.Fatalf("readlink restored symlink: %v", err)
	}
	if !strings.HasSuffix(linkDst, "SKILL.md") {
		t.Fatalf("expected symlink to end with SKILL.md, got %q", linkDst)
	}

	// Verify root config file was restored.
	restoredMCP := filepath.Join(restoreDir, "mcp.json")
	mcpData, err := os.ReadFile(restoredMCP)
	if err != nil {
		t.Fatalf("read restored mcp.json: %v", err)
	}
	if string(mcpData) != `{"mcpServers":{}}` {
		t.Fatalf("expected mcp.json content, got %q", string(mcpData))
	}

	// Verify user-owned file was NOT backed up.
	if _, err := os.Stat(filepath.Join(restoreDir, "user-notes.txt")); !os.IsNotExist(err) {
		t.Fatal("user-notes.txt should not have been backed up")
	}
}

func TestRotate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	targetDir := filepath.Join(backupsDir, "factory")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 7 archives across 7 distinct days (1 per day).
	for i := 0; i < 7; i++ {
		ts := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02-150405")
		makeArchive(t, filepath.Join(targetDir, ts+".tar.gz"))
	}

	// Rotate: keep 5 days, 3 per day.
	if err := Rotate(Config{Enabled: true, MaxDays: 5, MaxPerDay: 3}); err != nil {
		t.Fatal(err)
	}

	archives, _ := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
	if len(archives) != 5 {
		t.Fatalf("expected 5 archives after day-cap rotation, got %d: %v", len(archives), archives)
	}
	// Oldest 2 days should be gone; newest 5 days remain.
	sortBackupArchives(archives)
	if !strings.HasPrefix(filepath.Base(archives[0]), "2026-01-03") {
		t.Fatalf("expected oldest remaining archive to be 2026-01-03, got %s", filepath.Base(archives[0]))
	}
}

func TestRotatePerDayCap(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	targetDir := filepath.Join(backupsDir, "factory")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 5 archives all on the same day (different times).
	day := "2026-01-15"
	for i := 0; i < 5; i++ {
		ts := fmt.Sprintf("%s-%02d0000", day, i)
		makeArchive(t, filepath.Join(targetDir, ts+".tar.gz"))
	}

	// Rotate: keep 10 days, 3 per day → oldest 2 of today removed.
	if err := Rotate(Config{Enabled: true, MaxDays: 10, MaxPerDay: 3}); err != nil {
		t.Fatal(err)
	}

	archives, _ := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
	if len(archives) != 3 {
		t.Fatalf("expected 3 archives after per-day-cap rotation, got %d: %v", len(archives), archives)
	}
	sortBackupArchives(archives)
	// Newest 3 (hours 02, 03, 04) should survive.
	if !strings.HasPrefix(filepath.Base(archives[0]), "2026-01-15-020000") {
		t.Fatalf("expected oldest surviving archive to be 2026-01-15-020000, got %s", filepath.Base(archives[0]))
	}
}

func TestRotateTwoDimensions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	targetDir := filepath.Join(backupsDir, "factory")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 3 days × 4 archives each = 12 total.
	for day := 1; day <= 3; day++ {
		for hour := 0; hour < 4; hour++ {
			ts := fmt.Sprintf("2026-01-%02d-%02d0000", day, hour)
			makeArchive(t, filepath.Join(targetDir, ts+".tar.gz"))
		}
	}

	// Rotate: keep 2 days, 2 per day → expect 4 archives total.
	if err := Rotate(Config{Enabled: true, MaxDays: 2, MaxPerDay: 2}); err != nil {
		t.Fatal(err)
	}

	archives, _ := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
	if len(archives) != 4 {
		t.Fatalf("expected 4 archives (2 days × 2 per day), got %d: %v", len(archives), archives)
	}
	sortBackupArchives(archives)
	// All remaining archives should be from the newest 2 days (Jan 02 and Jan 03).
	for _, a := range archives {
		base := filepath.Base(a)
		if !strings.HasPrefix(base, "2026-01-02") && !strings.HasPrefix(base, "2026-01-03") {
			t.Fatalf("unexpected archive from old day: %s", base)
		}
		// Within each day only hours 02 and 03 (newest two) survive.
		hour := base[len("2026-01-0X-") : len("2026-01-0X-")+2]
		if hour != "02" && hour != "03" {
			t.Fatalf("expected only hours 02/03 to survive per-day cap, got %s", base)
		}
	}
}

func TestArchiveDayExtraction(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"2026-01-15-120000", "2026-01-15"},
		{"2026-01-15-120000-1", "2026-01-15"},
		{"2026-12-31-235959", "2026-12-31"},
		{"short", "short"},
	}
	for _, tc := range cases {
		if got := archiveDay(tc.name); got != tc.want {
			t.Errorf("archiveDay(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCreateArchiveRefusesExistingArchive(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "mcp.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(tmpDir, "backup.tar.gz")
	if err := os.WriteFile(archivePath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := createArchive(srcDir, []string{"mcp.json"}, archivePath); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected os.ErrExist, got %v", err)
	}
}

func TestListBackups(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	factoryDir := filepath.Join(backupsDir, "factory")
	claudeDir := filepath.Join(backupsDir, "claude")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create archives for two targets.
	for _, dir := range []string{factoryDir, claudeDir} {
		for i := 0; i < 3; i++ {
			ts := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02-150405")
			if err := os.WriteFile(filepath.Join(dir, ts+".tar.gz"), []byte("dummy"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	result, err := ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(result))
	}
	if len(result[hcc.TargetFactory]) != 3 {
		t.Fatalf("expected 3 factory backups, got %d", len(result[hcc.TargetFactory]))
	}
	if len(result[hcc.TargetClaude]) != 3 {
		t.Fatalf("expected 3 claude backups, got %d", len(result[hcc.TargetClaude]))
	}
}

func TestRestoreNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := Restore(hcc.TargetFactory, tmpDir)
	if err == nil {
		t.Fatal("expected error when no backups exist")
	}
}

func TestCreateSkipsNonexistentPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// None of these paths exist → should be silently skipped.
	managed := map[hcc.Target]ManagedPaths{
		hcc.TargetFactory: {Root: filepath.Join(tmpDir, "nonexistent"), RelPaths: []string{"skills", "mcp.json"}},
	}
	if err := Create(managed); err != nil {
		t.Fatalf("Create should not error on nonexistent paths: %v", err)
	}

	// No archive should be created since no paths exist.
	backupsDir := filepath.Join(tmpDir, ".agent-marketplace", "backups")
	archives, _ := filepath.Glob(filepath.Join(backupsDir, "factory", "*.tar.gz"))
	if len(archives) != 0 {
		t.Fatalf("expected no archives when all paths are nonexistent, got %d", len(archives))
	}
}

func TestCreateOnlyManagedPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	srcDir := filepath.Join(tmpDir, "factory")
	// Create managed content.
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "my-skill", "SKILL.md"), []byte("# Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create unmanaged content at root.
	if err := os.WriteFile(filepath.Join(srcDir, "personal.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "personal-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	managed := map[hcc.Target]ManagedPaths{
		hcc.TargetFactory: {Root: srcDir, RelPaths: []string{"skills"}},
	}
	if err := Create(managed); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Restore and verify only skills/ was backed up.
	restoreDir := filepath.Join(tmpDir, "restored")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetFactory, restoreDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(restoreDir, "skills", "my-skill", "SKILL.md")); err != nil {
		t.Fatal("skills/ should have been restored")
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "personal.txt")); !os.IsNotExist(err) {
		t.Fatal("personal.txt should NOT have been backed up")
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "personal-dir")); !os.IsNotExist(err) {
		t.Fatal("personal-dir should NOT have been backed up")
	}
}

func TestArchiveTargetRejectsTraversal(t *testing.T) {
	dstDir := t.TempDir()
	for _, name := range []string{"", "../escape", "skills/../../escape", "/absolute"} {
		if _, err := archiveTarget(dstDir, name); err == nil {
			t.Fatalf("unsafe archive entry %q accepted", name)
		}
	}
	if _, err := archiveTarget(dstDir, filepath.Join("skills", "ok")); err != nil {
		t.Fatalf("expected safe archive entry: %v", err)
	}
}

func TestValidateSymlinkTarget(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "relative traversal", target: "../escape", wantErr: true},
		{name: "absolute target", target: filepath.Join(string(filepath.Separator), "tmp", "escape"), wantErr: true},
		{name: "absolute cache target", target: filepath.Join(homeDir, ".cache", "agent-marketplace", "compiled", "factory", "skill")},
		{name: "safe relative target", target: filepath.Join("skills", "ok")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSymlinkTarget(tc.target)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestRestoreRejectsSymlinkAncestor(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	srcDir := filepath.Join(tmpDir, "factory")
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "test-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "test-skill", "SKILL.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	managed := map[hcc.Target]ManagedPaths{
		hcc.TargetFactory: {Root: srcDir, RelPaths: []string{"skills"}},
	}
	if err := Create(managed); err != nil {
		t.Fatal(err)
	}

	restoreDir := filepath.Join(tmpDir, "restore")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(restoreDir, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(hcc.TargetFactory, restoreDir); err == nil {
		t.Fatal("expected restore through symlink ancestor to fail")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "test-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("restore wrote through symlink ancestor")
	}
}

func TestRestoreRejectsSymlinkFileTarget(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	srcDir := filepath.Join(tmpDir, "factory")
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "test-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "test-skill", "SKILL.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Create(map[hcc.Target]ManagedPaths{
		hcc.TargetFactory: {Root: srcDir, RelPaths: []string{"skills"}},
	}); err != nil {
		t.Fatal(err)
	}

	restoreDir := filepath.Join(tmpDir, "restore")
	restoreSkillDir := filepath.Join(restoreDir, "skills", "test-skill")
	if err := os.MkdirAll(restoreSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(tmpDir, "outside.md")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(restoreSkillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(hcc.TargetFactory, restoreDir); err == nil {
		t.Fatal("expected restore over symlink file target to fail")
	}
	data, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside\n" {
		t.Fatal("restore wrote through symlink file target")
	}
}
