package tui

import (
	"context"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	mptestutil "github.com/Adyen/agent-marketplace/internal/testutil/marketplace"
)

func TestSnapshotRepoV2ConfigDoesNotOpenMigrationModal(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	writeV2Config(t, filepath.Join(repoRoot, ".agents", "config.yaml"))

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return nil, nil
	})
	defer restoreLocal()

	m := NewModel(repoRoot, homeDir)
	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	snapshot.generation = m.requests.Active(requestSnapshot)
	mi, _ := m.Update(snapshot)
	m = mi.(Model)

	if m.pane == paneMigrationModal {
		t.Fatalf("unexpected migration modal for repo v2 config, got pane=%v", m.pane)
	}
	if m.migrationPrompt.HasConfigs() {
		t.Fatal("unexpected pending repo migration")
	}
}

func TestSnapshotV1ConfigDoesNotOpenMigrationModal(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	writeV1Config(t, filepath.Join(repoRoot, ".agents", "config.yaml"))

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return nil, nil
	})
	defer restoreLocal()

	m := NewModel(repoRoot, homeDir)
	mi, _ := m.Update(readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached))
	m = mi.(Model)

	if m.pane == paneMigrationModal {
		t.Fatalf("did not expect migration modal for v1 config, got pane=%v", m.pane)
	}
	if len(m.migrationPrompt.Configs) != 0 {
		t.Fatalf("expected no pending migration, got %d", len(m.migrationPrompt.Configs))
	}
}

func TestMigrationModalLeaveKeepsConfigUnchanged(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeV2Config(t, configPath)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	m := NewModel(repoRoot, t.TempDir())
	m.pane = paneMigrationModal
	m.migrationPrompt = pendingMigrationFor("", "", repoRoot)

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("leave should schedule toast expiry")
	}
	if m.pane != paneList {
		t.Fatalf("leave should close modal, got pane=%v", m.pane)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("leave changed config:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	mi, _ = m.Update(snapshotLoadedMsg{
		generation:       m.requests.Active(requestSnapshot),
		mergedCfg:        &config.Config{},
		pendingMigration: m.migrationPrompt,
	})
	m = mi.(Model)
	if m.pane == paneMigrationModal {
		t.Fatal("migration modal reopened after the user left it")
	}
}

// spec: session-interactions/accepting-the-offer-migrates-the-user-personal-root
// spec: tui-interactions/accepting-the-offer-migrates-only-the-user-personal-root
func TestMigrationModalRunMigratesConfig(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(globalAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir global agents: %v", err)
	}
	globalConfigPath := filepath.Join(globalAgentsDir, "config.yaml")
	if err := os.WriteFile(globalConfigPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeV2Config(t, configPath)
	writeV2Config(t, localPath)
	t.Chdir(repoRoot)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(repoRoot, homeDir)
	m.pane = paneMigrationModal
	m.migrationPrompt = pendingMigrationFor(repoRoot, repoRoot, homeDir)

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: string('m')})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("run should emit a migration command")
	}
	if m.pane != paneList {
		t.Fatalf("run should close modal before command completes, got pane=%v", m.pane)
	}

	msg := cmd()
	done, ok := msg.(migrationDoneMsg)
	if !ok {
		t.Fatalf("expected migrationDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("migration failed: %v", done.err)
	}
	if done.count != 1 {
		t.Fatalf("expected only user config to be migrated, got %d migrations", done.count)
	}

	repoCfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load migrated repo config: %v", err)
	}
	if repoCfg.APIVersion != config.APIVersionV2 {
		t.Fatalf("expected repo apiVersion %q, got %q", config.APIVersionV2, repoCfg.APIVersion)
	}
	if len(repoCfg.Harnesses) != 0 {
		t.Fatalf("expected repo config to have no harnesses, got %v", repoCfg.Harnesses)
	}

	for _, path := range []string{configPath, localPath} {
		if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
			t.Fatalf("migration changed %s: %q, %v", path, got, err)
		}
		if backups, err := filepath.Glob(path + ".pre-v4.*.bak"); err != nil || len(backups) != 0 {
			t.Fatalf("unexpected repo migration backups: %v, %v", backups, err)
		}
	}
	globalCfg, err := config.LoadConfigFromPath(globalConfigPath)
	if err != nil {
		t.Fatalf("load migrated global config: %v", err)
	}
	if globalCfg.APIVersion != config.APIVersionV4 {
		t.Fatalf("user config was not migrated: %q", globalCfg.APIVersion)
	}
	if !equalStrings(globalCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected global harnesses to be preserved, got %v", globalCfg.Harnesses)
	}
}

func TestMigrationModalRunMigratesMarketplaceConfig(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	// Isolate cache and transaction state from any developer environment.
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, ".local", "state"))

	// Verified mirror artifact in the default cache root for the pin below.
	seed := mptestutil.SeedVerifiedMarketplaceSkill(t, filepath.Join(cache.CacheDir(), "marketplace"),
		"https://market.example", "mp", "src", "review")
	contentDigest, archiveDigest := seed.ContentDigest, seed.ArchiveDigest

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "apiVersion: agent-marketplace/v2\nmarketplaces:\n" +
		"  - url: https://market.example\n    instance_id: mp\n    resources:\n" +
		"      - registry_id: src\n        kind: skill\n        name: review\n" +
		"        revision: " + contentDigest + "\n        digest: " + contentDigest + "\n        direct: true\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(repoRoot, homeDir)
	m.pane = paneMigrationModal
	m.migrationPrompt = pendingMigrationFor(repoRoot, repoRoot, homeDir)

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: string('m')})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("run should emit a migration command")
	}
	msg := cmd()
	done, ok := msg.(migrationDoneMsg)
	if !ok {
		t.Fatalf("expected migrationDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("marketplace migration failed: %v", done.err)
	}
	if done.count != 1 {
		t.Fatalf("expected 1 migration, got %d", done.count)
	}
	migrated, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("hydrate migrated pair: %v", err)
	}
	if len(migrated.Marketplaces) != 1 || len(migrated.Marketplaces[0].Resources) != 1 ||
		migrated.Marketplaces[0].Resources[0].LockArchiveDigest != archiveDigest {
		t.Fatalf("hydrated marketplaces = %#v", migrated.Marketplaces)
	}
}

// spec: session-interactions/confirmation-retains-all-user-choices
func TestRenderMigrationModalIncludesActions(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneMigrationModal
	m.migrationPrompt = migrationPrompt{Configs: []migrationConfig{{Label: "Project", Path: "/repo/.agents/config.yaml"}}}

	out := ansi.Strip(renderMigrationModal(m))
	for _, want := range []string{
		"Configuration migration required",
		"Project",
		"agent-marketplace migrate --scope user",
		"apiVersion: agent-marketplace/v4",
		"preserves source selections and layer policies",
		"enter/m migrate",
		"l/esc leave unchanged",
	} {
		if !contains(out, want) {
			t.Fatalf("migration modal missing %q in output:\n%s", want, out)
		}
	}
}

// spec: session-interactions/legacy-user-configuration-is-offered-for-migration
func TestPendingMigrationOffersOnlyTheUserPersonalRoot(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeV2Config(t, userPath)
	writeV2Config(t, repoPath)
	writeV2Config(t, localPath)

	prompt := pendingMigrationFor(repoRoot, repoRoot, homeDir)
	if len(prompt.Configs) != 1 {
		t.Fatalf("pending migration = %#v, want exactly the user-personal root", prompt.Configs)
	}
	if prompt.Configs[0].Scope != config.V4MigrationUser || prompt.Configs[0].Path != userPath {
		t.Fatalf("offered migration = %#v, want user-personal root %q", prompt.Configs[0], userPath)
	}
}

// spec: session-interactions/declining-the-offer-continues-through-the-legacy-reader
// spec: tui-interactions/declining-the-offer-continues-through-the-legacy-reader
func TestMigrationDeclineLeavesAllRootsUnchanged(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	writeV2Config(t, userPath)
	writeV2Config(t, repoPath)
	writeV2Config(t, localPath)

	before := map[string][]byte{}
	for _, path := range []string{userPath, repoPath, localPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		before[path] = data
	}

	m := NewModel(repoRoot, homeDir)
	m.migrationPrompt = pendingMigrationFor(repoRoot, repoRoot, homeDir)
	if len(m.migrationPrompt.Configs) != 1 || m.migrationPrompt.Configs[0].Scope != config.V4MigrationUser {
		t.Fatalf("pending migration = %#v, want only the user-personal root", m.migrationPrompt.Configs)
	}
	m.pane = paneMigrationModal

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')})
	m = mi.(Model)
	if m.pane == paneMigrationModal {
		t.Fatal("declining kept the migration modal open")
	}
	if done, ran := cmdRunsMigration(cmd); ran {
		t.Fatalf("declining dispatched a migration: %+v", done)
	}

	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("declining migration removed %s: %v", path, err)
		}
		if string(got) != string(want) {
			t.Fatalf("declining migration changed %s:\nbefore:\n%s\nafter:\n%s", path, want, got)
		}
	}

	// The client continues through the legacy reader: a later snapshot does
	// not reopen the offer or fail closed on the unchanged legacy roots.
	mi, _ = m.Update(snapshotLoadedMsg{
		generation:       m.requests.Active(requestSnapshot),
		mergedCfg:        &config.Config{},
		pendingMigration: m.migrationPrompt,
	})
	m = mi.(Model)
	if m.pane == paneMigrationModal {
		t.Fatal("migration modal reopened after the user declined")
	}
}

// cmdRunsMigration executes a command tree and reports any migration it ran.
func cmdRunsMigration(cmd tea.Cmd) (migrationDoneMsg, bool) {
	if cmd == nil {
		return migrationDoneMsg{}, false
	}
	msg := cmd()
	if done, ok := msg.(migrationDoneMsg); ok {
		return done, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, inner := range batch {
			if done, ok := cmdRunsMigration(inner); ok {
				return done, true
			}
		}
	}
	return migrationDoneMsg{}, false
}

func writeV1Config(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := []byte("registries:\n  - url: ./skills\n    skills:\n      - audit\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write v1 config: %v", err)
	}
}

func writeV2Config(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := []byte("apiVersion: agent-marketplace/v2\nregistries:\n  - url: ./skills\n    skills:\n      - audit\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write v2 config: %v", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
