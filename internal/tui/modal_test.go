package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	mptestutil "github.com/Adyen/agent-marketplace/internal/testutil/marketplace"
)

// helperFlatCursorOnSkill returns a model with cursor on a known skill row.
func helperFlatCursorOnSkill(t *testing.T, r row) Model {
	t.Helper()
	m := NewModel("/repo", "/home")
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no skill flatItem found")
	return m
}

func TestV4ToggleExcludesCompetingSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	trusted := t.TempDir()
	rival := t.TempDir()
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  trusted: {directory: " + trusted + "}\n  rival: {directory: " + rival + "}\nresources:\n  trusted: {include: [skill/review]}\n  rival: {include: [skill/review]}\n")
	if err := os.WriteFile(userPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	rows := []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "review", Kind: ir.KindSkill}, Source: config.UserPersonal}, RegistryURL: rival}}
	act := toggleAction{scope: scopeGlobal, enable: true, kind: ir.KindSkill, skillName: "review", registryURL: trusted}
	cmd := excludeCompetingRegistriesCmd(repo, rows, act)
	if cmd == nil {
		t.Fatal("v4 competing source did not produce a toggle command")
	}
	written, ok := cmd().(configWrittenMsg)
	if !ok || written.err != nil {
		t.Fatalf("v4 competing exclusion = %#v", written)
	}
	v4, err := config.LoadV4ConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if group := v4.Groups["rival"]; len(group.Include) != 0 || len(group.Exclude) != 1 || group.Exclude[0] != "skill/review" {
		t.Fatalf("competing v4 source was not excluded: %#v", group)
	}
	if group := v4.Groups["trusted"]; len(group.Include) != 1 || group.Include[0].Locator != "skill/review" {
		t.Fatalf("trusted v4 source was modified: %#v", group)
	}
}

func TestV4MarketplaceToggleCopiesAndRemovesExactPinAtomically(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	url := "https://market.example"
	data := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {hosted: {marketplace: " + url + "}}\nresources: {hosted: {include: [tools/skill/review]}}\n")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	archive := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	lockData, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser, Marketplaces: []lockfile.MarketplaceLock{{
		URL: url, InstanceID: "mp", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
		Resources: []lockfile.MarketplaceResourceLock{{RegistryName: "tools", RegistryID: "src", Kind: "skill", Name: "review", Revision: string(digest), ContentDigest: digest, ArchiveDigest: archive, Acquisition: lockfile.MarketplaceAcquisitionMirror}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadConfigFromPath(configPath)
	if err != nil || len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 1 {
		t.Fatalf("load v4 Marketplace selection: %#v: %v", loaded, err)
	}
	entry := loaded.Marketplaces[0]
	entry.Source = config.UserPersonal
	r := row{
		Artifact:      resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, IsMarketplace: true, Source: config.UserPersonal},
		EnabledGlobal: true, RegistryURL: url,
		MarketplaceSelections: map[config.Source]marketplaceSelection{config.UserPersonal: {Entry: entry, Pin: entry.Resources[0]}},
	}
	localPath := config.LocalConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	localAct := scopedToggleAction(r, scopeLocal)
	if !localAct.enable {
		t.Fatal("v4 Marketplace pin should be available in local scope")
	}
	localMutatorCalled := false
	localMsg := runDependencyScopedToggle(context.Background(), repo, localAct, func(context.Context, string, func(pipeline.ConfigPathMapper) error) error {
		localMutatorCalled = true
		return errors.New("legacy local mutation was used")
	})()
	localWritten, ok := localMsg.(configWrittenMsg)
	if !ok || localWritten.err != nil || localMutatorCalled {
		t.Fatalf("v4 cross-scope Marketplace toggle = %#v, legacy mutator called=%t", localMsg, localMutatorCalled)
	}
	localV4, err := config.LoadV4ConfigFromPath(localPath)
	if err != nil || len(localV4.Sources) != 1 {
		t.Fatalf("local Marketplace source missing: %#v: %v", localV4, err)
	}
	var localSourceName string
	for name := range localV4.Sources {
		localSourceName = name
		if group := localV4.Groups[name]; len(group.Include) != 1 || group.Include[0].Locator != "tools/skill/review" {
			t.Fatalf("local Marketplace pin not selected: %#v", group)
		}
	}
	localLockBytes, err := os.ReadFile(filepath.Join(repo, ".agents", "lock.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	localLock, err := lockfile.Decode(localLockBytes)
	if err != nil || len(localLock.Marketplaces) != 1 || len(localLock.Marketplaces[0].Resources) != 1 {
		t.Fatalf("exact Marketplace pin not copied to local lock: %#v: %v", localLock, err)
	}
	originalLocal, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	aliasedLocal := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {%s: {marketplace: %s}}\nresources:\n  %s:\n    include:\n      - tools/skill/review:\n          as: team-review\n", localSourceName, url, localSourceName)
	if err := os.WriteFile(localPath, []byte(aliasedLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	localOff := localAct
	localOff.enable = false
	staleLocalMsg := runDependencyScopedToggle(context.Background(), repo, localOff, func(context.Context, string, func(pipeline.ConfigPathMapper) error) error {
		localMutatorCalled = true
		return errors.New("legacy local mutation was used")
	})()
	staleLocal, ok := staleLocalMsg.(configWrittenMsg)
	if !ok || !errors.Is(staleLocal.err, dependencytxn.ErrGenerationConflict) || localMutatorCalled {
		t.Fatalf("stale cross-scope toggle did not fail closed: %#v", staleLocalMsg)
	}
	if after, err := os.ReadFile(localPath); err != nil || string(after) != aliasedLocal {
		t.Fatalf("stale cross-scope toggle changed local config: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(repo, ".agents", "lock.local.yaml")); err != nil || string(after) != string(localLockBytes) {
		t.Fatalf("stale cross-scope toggle changed local lock: %v", err)
	}
	if err := os.WriteFile(localPath, originalLocal, 0o644); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(configPath); err != nil || string(after) != string(data) {
		t.Fatalf("cross-scope toggle changed user config: %v", err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockData) {
		t.Fatalf("cross-scope toggle changed user lock: %v", err)
	}
	act := scopedToggleAction(r, scopeGlobal)
	if act.enable {
		t.Fatal("selected v4 Marketplace request should be disabled")
	}
	mutatorCalled := false
	mutate := func(context.Context, string, func(pipeline.ConfigPathMapper) error) error {
		mutatorCalled = true
		return errors.New("legacy config mutation was used")
	}
	stale := act
	stale.marketplaceSelection.Pin.Digest = "sha256:" + strings.Repeat("c", 64)
	staleMsg := runDependencyScopedToggle(context.Background(), repo, stale, mutate)()
	staleWritten, ok := staleMsg.(configWrittenMsg)
	if !ok || !errors.Is(staleWritten.err, dependencytxn.ErrGenerationConflict) || mutatorCalled {
		t.Fatalf("stale v4 Marketplace toggle did not fail closed: %#v, legacy mutator called=%t", staleMsg, mutatorCalled)
	}
	if after, err := os.ReadFile(configPath); err != nil || string(after) != string(data) {
		t.Fatalf("stale toggle changed v4 config: %v", err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockData) {
		t.Fatalf("stale toggle changed v4 lock: %v", err)
	}
	msg := runDependencyScopedToggle(context.Background(), repo, act, mutate)()
	written, ok := msg.(configWrittenMsg)
	if !ok || written.err != nil || mutatorCalled {
		t.Fatalf("v4 Marketplace toggle = %#v, legacy mutator called=%t", msg, mutatorCalled)
	}
	v4, err := config.LoadV4ConfigFromPath(configPath)
	if err != nil || len(v4.Groups["hosted"].Include) != 0 {
		t.Fatalf("v4 request remains after toggle: %#v: %v", v4, err)
	}
	updated, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Decode(updated)
	if err != nil || len(locked.Marketplaces) != 1 || len(locked.Marketplaces[0].Resources) != 0 {
		t.Fatalf("v4 Marketplace lock not pruned with config: %#v: %v", locked, err)
	}
}

func TestV4MarketplaceToggleLegacyPinEvidence(t *testing.T) {
	for _, test := range []struct {
		name     string
		verified bool
		slug     string
		revision string
	}{{name: "missing", slug: "tools"}, {name: "verified", verified: true, slug: "tools"}, {name: "verified-without-slug", verified: true}, {name: "unrepresentable-revision", verified: true, slug: "tools", revision: "v2"}} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			repo := t.TempDir()
			url := "https://market.example"
			digest := "sha256:" + strings.Repeat("a", 64)
			var archiveDigest string
			if test.verified {
				proof := mptestutil.SeedVerifiedMarketplaceSkill(t, filepath.Join(cache.CacheDir(), "marketplace"), url, "mp", "src", "review")
				digest, archiveDigest = proof.ContentDigest, proof.ArchiveDigest
			}
			userPath := filepath.Join(home, ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			revision := digest
			if test.revision != "" {
				revision = test.revision
			}
			pin := config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: test.slug, Kind: "skill", Name: "review", Revision: revision, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror, Direct: true}
			if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(userPath, url, "mp", "hosted", []config.MarketplaceResourcePin{pin}); err != nil {
				t.Fatal(err)
			}
			beforeUser, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatal(err)
			}
			localPath := config.LocalConfigPath(repo)
			if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
				t.Fatal(err)
			}
			localData := []byte("apiVersion: agent-marketplace/v4\n")
			if err := os.WriteFile(localPath, localData, 0o644); err != nil {
				t.Fatal(err)
			}
			entry := config.MarketplaceEntry{URL: url, InstanceID: "mp", Alias: "hosted", Source: config.UserPersonal}
			r := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, IsMarketplace: true, Source: config.UserPersonal}, EnabledGlobal: true,
				MarketplaceSelections: map[config.Source]marketplaceSelection{config.UserPersonal: {Entry: entry, Pin: pin}}}
			act := scopedToggleAction(r, scopeLocal)
			mutatorCalled := false
			msg := runDependencyScopedToggle(context.Background(), repo, act, func(context.Context, string, func(pipeline.ConfigPathMapper) error) error {
				mutatorCalled = true
				return errors.New("legacy local mutation was used")
			})()
			if mutatorCalled {
				t.Fatalf("v4 Marketplace copy used the legacy mutator: %#v", msg)
			}
			lockPath := filepath.Join(repo, ".agents", "lock.local.yaml")
			if test.verified && test.revision == "" {
				written, ok := msg.(configWrittenMsg)
				if !ok || written.err != nil {
					t.Fatalf("verified legacy pin was not copied: %#v", msg)
				}
				v4, err := config.LoadV4ConfigFromPath(localPath)
				if err != nil || len(v4.Sources) != 1 {
					t.Fatalf("v4 target source missing: %#v: %v", v4, err)
				}
				wantRegistry := test.slug
				if wantRegistry == "" {
					wantRegistry = "src"
				}
				for name := range v4.Sources {
					if group := v4.Groups[name]; len(group.Include) != 1 || group.Include[0].Locator != wantRegistry+"/skill/review" {
						t.Fatalf("v4 target pin not selected: %#v", group)
					}
				}
				lockBytes, err := os.ReadFile(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				locked, err := lockfile.Decode(lockBytes)
				if err != nil || len(locked.Marketplaces) != 1 || len(locked.Marketplaces[0].Resources) != 1 || string(locked.Marketplaces[0].Resources[0].ArchiveDigest) != archiveDigest {
					t.Fatalf("verified legacy evidence not published: %#v: %v", locked, err)
				}
			} else {
				fallback, ok := msg.(marketplaceToggleFallbackMsg)
				if !ok {
					t.Fatalf("legacy pin did not request a consented preview: %#v", msg)
				}
				if test.revision != "" {
					if !errors.Is(fallback.reason, pipeline.ErrV4MarketplaceRevisionUnrepresentable) {
						t.Fatalf("unrepresentable legacy revision was not identified: %v", fallback.reason)
					}
				} else if !errors.Is(fallback.reason, dependencytxn.ErrGenerationConflict) || !errors.Is(fallback.reason, pipeline.ErrV4MarketplaceEvidenceUnavailable) {
					t.Fatalf("missing legacy evidence was not identified: %v", fallback.reason)
				}
				if after, err := os.ReadFile(localPath); err != nil || string(after) != string(localData) {
					t.Fatalf("v4 target changed during refused copy: %v", err)
				}
				if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
					t.Fatalf("v4 target lock created without proof: %v", err)
				}
			}
			if after, err := os.ReadFile(userPath); err != nil || string(after) != string(beforeUser) {
				t.Fatalf("legacy source changed during copy: %v", err)
			}
		})
	}
}

func TestSpaceOnSkillRowOpensToggleModal(t *testing.T) {
	r := mkRow("code-review", withProject, withRegistry("t.git", "t"))
	m := helperFlatCursorOnSkill(t, r)

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)

	if m.pane != paneToggleModal {
		t.Fatalf("expected pane=paneToggleModal, got %v", m.pane)
	}
	if m.modalSkill.Artifact.Name != "code-review" {
		t.Fatalf("expected modalSkill=code-review, got %q", m.modalSkill.Artifact.Name)
	}
}

func TestSpaceOnMarketplaceCollectionLeafShowsToastNotModal(t *testing.T) {
	r := mkRow("collection-leaf", withProject)
	r.Artifact.IsMarketplace = true
	r.MarketplaceCollectionOnly = true
	m := helperFlatCursorOnSkill(t, r)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)

	if m.pane == paneToggleModal {
		t.Fatal("collection-selected Marketplace leaf must bypass modal")
	}
	if cmd == nil {
		t.Fatal("expected a toast cmd for collection-selected Marketplace leaf")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "collection-selected Marketplace items must be toggled through their collection" {
		t.Fatalf("unexpected toast text: %q", toast.text)
	}
}

func TestSpaceOnRequiredSkillShowsToastNotModal(t *testing.T) {
	r := mkRow("req-skill", withRequired, withProject)
	m := helperFlatCursorOnSkill(t, r)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	if m.pane == paneToggleModal {
		t.Fatalf("required skill must bypass modal, got pane=paneToggleModal")
	}
	// Cmd should emit a toast message.
	if cmd == nil {
		t.Fatalf("expected a toast cmd for required skill")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "required items cannot be toggled" {
		t.Fatalf("unexpected toast text: %q", toast.text)
	}
}

func TestRenderToggleModalShowsPerScopeState(t *testing.T) {
	r := mkRow("audit", withGlobal, withProject, withExcluded, withRegistry("s.git", "s"))
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = r

	out := renderToggleModal(m)
	for _, want := range []string{
		"audit",
		"Global",
		"Project",
		"Local",
		"Local exclude",
		"[g]",
		"[p]",
		"[l]",
		"[e]",
		"[esc]",
	} {
		if !contains(out, want) {
			t.Fatalf("modal missing %q in output:\n%s", want, out)
		}
	}
}

func TestModalGkeyTogglesGlobalByCurrentState(t *testing.T) {
	// Not enabled globally: g should schedule an Add.
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withProject)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'g'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action, got pane=%v", m.pane)
	}
}

func TestModalGlobalRepoRelativeSelectionFailsBeforeWrite(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	selected := mkRow("audit", withAvailable, withRegistry("./local-registry", "local"))
	selected.Artifact.Source = config.UserPersonal
	competitor := mkRow("audit", withAvailable, withRegistry("https://example.com/remote.git", "remote"))
	competitor.Artifact.Source = config.RepoCommitted
	m := NewModel(repoRoot, homeDir)
	m.pane = paneToggleModal
	m.modalSkill = selected
	m.rows = []row{selected, competitor}
	m.errs = []error{fmt.Errorf(`skill "audit" is provided by multiple registries`)}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected global toggle command")
	}
	msg, ok := cmd().(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	var globalRelativeErr *config.GlobalRelativeRegistryError
	if !errors.As(msg.err, &globalRelativeErr) {
		t.Fatalf("expected GlobalRelativeRegistryError, got %v", msg.err)
	}
	globalConfig := filepath.Join(homeDir, ".agents", "config.yaml")
	if _, err := os.Stat(globalConfig); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("global config was created before collision policy failed: %v", err)
	}
	if m.pendingExcludeAction != nil {
		t.Fatal("collision follow-up remained pending after preflight failure")
	}
}

func TestModalLocalCollisionDoesNotScheduleUnsupportedFollowUp(t *testing.T) {
	for _, key := range []rune{'l', 'e'} {
		t.Run(string(key), func(t *testing.T) {
			selected := mkRow("audit", withAvailable, withRegistry("https://example.com/remote.git", "remote"))
			competitor := mkRow("audit", withAvailable, withRegistry("./local-registry", "local"))
			m := NewModel(t.TempDir(), t.TempDir())
			m.pane = paneToggleModal
			m.modalSkill = selected
			m.rows = []row{selected, competitor}
			m.errs = []error{fmt.Errorf(`skill "audit" is provided by multiple registries`)}

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
			m = updated.(Model)
			if cmd == nil {
				t.Fatal("expected local toggle command")
			}
			if m.pendingExcludeAction != nil {
				t.Fatal("unsupported local collision follow-up was scheduled")
			}
		})
	}
}

func TestModalMarketplaceEnableDoesNotScheduleRegistryCollisionExcludes(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")
	m.modalSkill.Artifact.Kind = ir.KindSkill
	m.modalSkill.Artifact.IsMarketplace = true
	m.errs = []error{fmt.Errorf(`skill "audit" is provided by multiple registries: marketplace, registry`)}

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mi.(Model)

	if cmd == nil {
		t.Fatal("expected Marketplace toggle command")
	}
	if m.pendingExcludeAction != nil {
		t.Fatalf("Marketplace toggle scheduled registry collision excludes: %#v", m.pendingExcludeAction)
	}
}

func TestModalPkeyTogglesProject(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'p'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestModalLkeyTogglesLocal(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'l'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestModalEkeyTogglesLocalExclude(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'e'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestRenderToggleModalDisablesLocalExcludeForMachineScopeSkill(t *testing.T) {
	r := mkRow("audit", withGlobal)
	r.Artifact.Source = config.UserPersonal

	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = r

	out := renderToggleModal(m)
	if !contains(out, "toggle skill exclude (use [g] to toggle at user level)") {
		t.Fatalf("expected disabled local exclude hint in modal output, got:\n%s", out)
	}
}

func TestModalEkeyShowsToastForMarketplaceItem(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withProject)
	m.modalSkill.Artifact.IsMarketplace = true

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = mi.(Model)

	if m.pane != paneToggleModal {
		t.Fatal("Marketplace modal should stay open when local exclude is blocked")
	}
	if cmd == nil {
		t.Fatal("expected toast cmd for Marketplace local exclude")
	}
	toast, ok := cmd().(toastMsg)
	if !ok || toast.text != "Marketplace items must be toggled by installation scope" {
		t.Fatalf("unexpected Marketplace exclude result: %#v", toast)
	}
}

func TestModalEkeyShowsToastForMachineScopeSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withGlobal)
	m.modalSkill.Artifact.Source = config.UserPersonal

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected toast cmd for blocked local exclude")
	}
	if m.pane != paneToggleModal {
		t.Fatalf("modal should stay open when local exclude is blocked")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "Cannot exclude org/user skill at local level — use [g] instead" {
		t.Fatalf("unexpected toast text: %q", toast.text)
	}
	if toast.duration != 2*time.Second {
		t.Fatalf("unexpected toast duration: %v", toast.duration)
	}
}

func TestModalEscCancelsWithoutWrite(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("esc should close modal")
	}
	if cmd != nil {
		t.Fatalf("esc must not emit a write cmd")
	}
}

func TestScopedToggleAddsWhenDisabledRemovesWhenEnabled(t *testing.T) {
	// Global disabled → action = enable
	r1 := mkRow("x", withProject)
	act := scopedToggleAction(r1, scopeGlobal)
	if !act.enable {
		t.Fatalf("scopeGlobal on project-only skill should enable")
	}
	if act.disablesResource() {
		t.Fatal("enabling a global selection should not disable the resource")
	}

	r2 := mkRow("x", withGlobal, withProject)
	act = scopedToggleAction(r2, scopeGlobal)
	if act.enable {
		t.Fatalf("scopeGlobal on globally-enabled skill should disable")
	}
	if !act.disablesResource() {
		t.Fatal("removing a global selection should disable the resource")
	}

	// Exclude toggle: if not excluded → add; if excluded → remove.
	r3 := mkRow("x", withProject)
	act = scopedToggleAction(r3, scopeLocalExclude)
	if !act.enable {
		t.Fatalf("scopeLocalExclude on non-excluded skill should add exclude")
	}
	if !act.disablesResource() {
		t.Fatal("adding a local exclusion should disable the resource")
	}

	r4 := mkRow("x", withProject, withExcluded)
	act = scopedToggleAction(r4, scopeLocalExclude)
	if act.enable {
		t.Fatalf("scopeLocalExclude on excluded skill should remove exclude")
	}
	if act.disablesResource() {
		t.Fatal("removing a local exclusion should not disable the resource")
	}
}

func TestScopedToggleActionPopulatesRegistryMetadata(t *testing.T) {
	r := mkRow("audit", withAvailable,
		withRegistry("git@example.com:sec.git", "sec"),
		withRegistryMeta("main", "skills"),
	)

	act := scopedToggleAction(r, scopeProject)
	if act.registryURL != "git@example.com:sec.git" {
		t.Fatalf("expected registryURL=git@example.com:sec.git, got %q", act.registryURL)
	}
	if act.registryRef != "main" {
		t.Fatalf("expected registryRef=main, got %q", act.registryRef)
	}
	if act.registryPath != "skills" {
		t.Fatalf("expected registryPath=skills, got %q", act.registryPath)
	}
}

func TestRunScopedToggleDisablesProjectMarketplacePinWithoutRegistryMutation(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := config.ProjectConfigPath(repoRoot)
	digest := "sha256:" + strings.Repeat("a", 64)
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v1", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror, InstalledName: "team-review", Direct: true,
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Source: config.RepoCommitted}
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(configPath, entry.URL, entry.InstanceID, entry.Alias, []config.MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	r := row{
		Artifact:       resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "team-review"}, IsMarketplace: true},
		EnabledProject: true,
		RegistryURL:    entry.URL,
		MarketplaceSelections: map[config.Source]marketplaceSelection{
			config.RepoCommitted: {Entry: entry, Pin: pin},
		},
	}

	msg := runScopedToggle(repoRoot, scopedToggleAction(r, scopeProject))()

	if written, ok := msg.(configWrittenMsg); !ok || written.err != nil {
		t.Fatalf("Marketplace toggle = %#v", msg)
	}
	loaded, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 0 {
		t.Fatalf("Marketplace resources after disable = %#v", loaded.Marketplaces)
	}
	if len(loaded.Registries) != 0 {
		t.Fatalf("registries mutated by Marketplace disable: %#v", loaded.Registries)
	}
}

func TestRunScopedToggleCopiesExactMarketplacePinToLocalScope(t *testing.T) {
	repoRoot := t.TempDir()
	digest := "sha256:" + strings.Repeat("b", 64)
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v2", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror, InstalledName: "team-review", Direct: true,
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: "v1"}}),
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Source: config.RepoCommitted}
	r := row{
		Artifact:       resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "team-review"}, IsMarketplace: true, Source: config.RepoCommitted},
		EnabledProject: true,
		MarketplaceSelections: map[config.Source]marketplaceSelection{
			config.RepoCommitted: {Entry: entry, Pin: pin},
		},
	}

	msg := runScopedToggle(repoRoot, scopedToggleAction(r, scopeLocal))()

	if written, ok := msg.(configWrittenMsg); !ok || written.err != nil {
		t.Fatalf("Marketplace toggle = %#v", msg)
	}
	loaded, err := config.LoadLocalConfigFromPath(config.LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 1 {
		t.Fatalf("local Marketplace config = %#v", loaded)
	}
	got := loaded.Marketplaces[0].Resources[0]
	if got.RegistryID != pin.RegistryID || got.Name != pin.Name || got.InstalledName != pin.InstalledName || got.Revision != pin.Revision || got.Digest != pin.Digest || !got.Direct {
		t.Fatalf("copied Marketplace pin = %#v, want %#v", got, pin)
	}
	if len(got.SelectedBy.List()) != 0 {
		t.Fatalf("copied direct pin retained foreign collection selectors: %#v", got.SelectedBy.List())
	}
	if len(loaded.Registries) != 0 {
		t.Fatalf("registries mutated by Marketplace enable: %#v", loaded.Registries)
	}
}

func TestRunScopedToggleRejectsMarketplaceAliasConflictWithoutMutation(t *testing.T) {
	repoRoot := t.TempDir()
	localPath := config.LocalConfigPath(repoRoot)
	digest := "sha256:" + strings.Repeat("f", 64)
	existingPin := config.MarketplaceResourcePin{
		RegistryID: "existing-source", Kind: "skill", Name: "existing", Revision: "v1", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
	}
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(localPath, "https://other-market.example", "other-id", "public", []config.MarketplaceResourcePin{existingPin}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: "source-id", Kind: "skill", Name: "review", Revision: "v1", Digest: digest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Alias: "public", Source: config.RepoCommitted}
	r := row{
		Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, IsMarketplace: true, Source: config.RepoCommitted},
		MarketplaceSelections: map[config.Source]marketplaceSelection{
			config.RepoCommitted: {Entry: entry, Pin: pin},
		},
	}

	msg := runScopedToggle(repoRoot, scopedToggleAction(r, scopeLocal))()

	written, ok := msg.(configWrittenMsg)
	if !ok || written.err == nil {
		t.Fatalf("Marketplace alias conflict toggle = %#v", msg)
	}
	after, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("alias conflict mutated config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunScopedToggleProjectScopeAutoEnsuresRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Project config has NO registry matching the global-only URL.
	projectConfig := `apiVersion: agent-marketplace/v2
registries:
  - url: ./local
    path: skills
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Toggle a skill from a global-only registry at project scope.
	act := toggleAction{
		skillName:    "audit",
		scope:        scopeProject,
		enable:       true,
		registryURL:  "git@example.com:global-skills.git",
		registryRef:  "main",
		registryPath: "skills",
	}

	cmd := runScopedToggle(repoRoot, act)
	msg := cmd()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err != nil {
		t.Fatalf("project-scope toggle should succeed with auto-ensure, got error: %v", cw.err)
	}
	if !cw.enabled {
		t.Fatalf("expected enabled=true")
	}

	// Verify the project config now has the global registry entry.
	cfg, err := config.LoadConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	found := false
	for _, reg := range cfg.Registries {
		if reg.URL == "git@example.com:global-skills.git" {
			found = true
			if reg.Ref != "main" {
				t.Fatalf("expected ref=main, got %q", reg.Ref)
			}
			if reg.Path != "skills" {
				t.Fatalf("expected path=skills, got %q", reg.Path)
			}
			// The skill should be in the registry's skills list.
			hasAudit := false
			for _, s := range reg.Skills.Include {
				if s == "audit" {
					hasAudit = true
				}
			}
			if !hasAudit {
				t.Fatalf("expected audit in registry skills list, got %v", reg.Skills)
			}
		}
	}
	if !found {
		t.Fatalf("expected global-skills.git registry to be added to project config")
	}
}

// TestRunScopedToggleLocalScopeWritesRegistryScoped verifies that enabling a
// skill at local scope writes a registry-scoped include into config.local.yaml
// (creating the registry entry), NOT a top-level skills.include. Previously the
// local branch wrote to the top-level selector, which was inconsistent with the
// project/global scopes.
func TestRunScopedToggleLocalScopeWritesRegistryScoped(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	act := toggleAction{
		skillName:    "audit",
		scope:        scopeLocal,
		enable:       true,
		registryURL:  "git@example.com:global-skills.git",
		registryRef:  "main",
		registryPath: "skills",
	}

	cmd := runScopedToggle(repoRoot, act)
	msg := cmd()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err != nil {
		t.Fatalf("local-scope toggle should succeed, got error: %v", cw.err)
	}

	localCfg, err := config.LoadLocalConfigFromPath(filepath.Join(agentsDir, "config.local.yaml"))
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	// Local config must exist and must NOT use the top-level selector.
	if localCfg == nil || len(localCfg.Skills.Include) != 0 {
		t.Fatalf("expected local config written with no top-level skills.include, got %#v", localCfg)
	}
	// Must be registry-scoped under the matching entry.
	found := false
	for _, reg := range localCfg.Registries {
		if reg.URL == "git@example.com:global-skills.git" {
			found = true
			// The ensured local shadow entry must preserve the exact-entry
			// identity fields (ref + path), not just the URL-scoped include.
			if reg.Ref != "main" {
				t.Fatalf("expected ref=main on local registry entry, got %q", reg.Ref)
			}
			if reg.Path != "skills" {
				t.Fatalf("expected path=skills on local registry entry, got %q", reg.Path)
			}
			if !containsString(reg.Skills.Include, "audit") {
				t.Fatalf("expected audit in registry skills.include, got %v", reg.Skills.Include)
			}
		}
	}
	if !found {
		t.Fatalf("expected global-skills.git registry entry in local config, got %#v", localCfg.Registries)
	}
}

// TestRunScopedToggleLocalExcludeIsRegistryScoped reproduces the bug where
// toggling the local exclude ([e]) on a skill that was locally enabled wrote a
// top-level skills.exclude AND left the registry-scoped include in place,
// producing a contradictory include+exclude pair. The exclude must now be
// registry-scoped and must remove the include.
func TestRunScopedToggleLocalExcludeIsRegistryScoped(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Local config already has a registry-scoped include (from a prior [l] enable).
	localPath := filepath.Join(agentsDir, "config.local.yaml")
	if err := os.WriteFile(localPath, []byte(`registries:
  - url: ./plugins/terminals
    skills:
      include:
        - adding-terminal-analytics-events
`), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	act := toggleAction{
		skillName:   "adding-terminal-analytics-events",
		kind:        ir.KindSkill,
		scope:       scopeLocalExclude,
		enable:      true,
		registryURL: "./plugins/terminals",
	}
	msg := runScopedToggle(repoRoot, act)()
	if cw, ok := msg.(configWrittenMsg); !ok || cw.err != nil {
		t.Fatalf("expected successful configWrittenMsg, got %#v", msg)
	}

	localCfg, err := config.LoadLocalConfigFromPath(localPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	// No top-level exclude.
	if len(localCfg.Skills.Exclude) != 0 {
		t.Fatalf("expected no top-level skills.exclude, got %v", localCfg.Skills.Exclude)
	}
	if len(localCfg.Registries) != 1 {
		t.Fatalf("expected one registry entry, got %#v", localCfg.Registries)
	}
	entry := localCfg.Registries[0]
	// Registry-scoped exclude present; the contradictory include removed.
	if !containsString(entry.Skills.Exclude, "adding-terminal-analytics-events") {
		t.Fatalf("expected registry-scoped exclude, got %#v", entry.Skills)
	}
	if containsString(entry.Skills.Include, "adding-terminal-analytics-events") {
		t.Fatalf("expected the contradictory include to be removed, got %#v", entry.Skills.Include)
	}
}

func TestHasCollisionErrorDetectsCollision(t *testing.T) {
	errs := []error{
		fmt.Errorf(`skill "smith" is provided by multiple registries: global.git, project.git`),
	}
	if !hasCollisionError(errs, ir.KindSkill, "smith") {
		t.Fatalf("expected collision to be detected for smith")
	}
	if hasCollisionError(errs, ir.KindSkill, "other") {
		t.Fatalf("expected no collision for other")
	}
	if hasCollisionError(nil, ir.KindSkill, "smith") {
		t.Fatalf("expected no collision with nil errs")
	}
	if hasCollisionError([]error{fmt.Errorf("some other error")}, ir.KindSkill, "smith") {
		t.Fatalf("expected no collision for unrelated error")
	}
	// Different kind with same name should not match.
	if hasCollisionError(errs, ir.KindCommand, "smith") {
		t.Fatalf("expected no collision for command smith when error is about skill")
	}
}

func TestCompetingRegistryURLsFindsOtherSources(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@a.com:global.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@b.com:project.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "other", Kind: ir.KindSkill}}, RegistryURL: "git@a.com:global.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindCommand}}, RegistryURL: "git@c.com:cmd.git"},
	}

	competing := competingRegistryURLs(rows, ir.KindSkill, "smith", "git@a.com:global.git")
	if len(competing) != 1 || competing[0] != "git@b.com:project.git" {
		t.Fatalf("expected competing=[git@b.com:project.git], got %v", competing)
	}

	// No competing sources.
	competing = competingRegistryURLs(rows, ir.KindSkill, "other", "git@a.com:global.git")
	if len(competing) != 0 {
		t.Fatalf("expected no competing URLs, got %v", competing)
	}

	// Same name but different kind should not be competing.
	competing = competingRegistryURLs(rows, ir.KindCommand, "smith", "git@c.com:cmd.git")
	if len(competing) != 0 {
		t.Fatalf("expected no competing URLs across different kinds, got %v", competing)
	}
}

func TestRenderToggleModalShowsCollisionWarning(t *testing.T) {
	r := mkRow("smith", withAvailable, withRegistry("global.git", "global"))
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = r
	m.errs = []error{
		fmt.Errorf(`skill "smith" is provided by multiple registries: global.git, project.git`),
	}

	out := renderToggleModal(m)
	if !contains(out, "Name collision") {
		t.Fatalf("expected collision warning in modal output, got:\n%s", out)
	}
}

func TestRenderToggleModalNoCollisionWarningWithoutError(t *testing.T) {
	r := mkRow("smith", withAvailable, withRegistry("global.git", "global"))
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = r
	// No errors — no collision warning.

	out := renderToggleModal(m)
	if contains(out, "Name collision") {
		t.Fatalf("did not expect collision warning without error, got:\n%s", out)
	}
}

func TestCompactToggleModalDoesNotAdvertiseMachineExclude(t *testing.T) {
	r := mkRow("required", withGlobal)
	r.Artifact.Source = config.Organization
	m := NewModel("/repo", "/home")
	m.width = 50
	m.height = 12
	m.pane = paneToggleModal
	m.modalSkill = r

	out := renderToggleModal(m)
	if !contains(out, "[e] unavailable") || contains(out, "[e] exclude") {
		t.Fatalf("machine-scoped compact actions are misleading:\n%s", out)
	}
}

// TestRunScopedToggleGlobalRejectsRepoRelativeRegistry verifies that enabling a
// skill globally from a repo-relative local registry is refused, and that no
// global config file is written.
func TestRunScopedToggleGlobalRejectsRepoRelativeRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	act := toggleAction{
		skillName:   "audit",
		scope:       scopeGlobal,
		enable:      true,
		registryURL: "./plugins/gitlab-mr",
	}

	msg := runScopedToggle(repoRoot, act)()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err == nil {
		t.Fatalf("expected error for global toggle of repo-relative registry")
	}
	var gre *config.GlobalRelativeRegistryError
	if !errors.As(cw.err, &gre) {
		t.Fatalf("expected GlobalRelativeRegistryError, got %T: %v", cw.err, cw.err)
	}
	if gre.URL != act.registryURL {
		t.Fatalf("expected error URL %q, got %q", act.registryURL, gre.URL)
	}

	// The global config must not have been created.
	globalConfig := filepath.Join(homeDir, ".agents", "config.yaml")
	if _, err := os.Stat(globalConfig); !os.IsNotExist(err) {
		t.Fatalf("expected no global config written, got %v", err)
	}
}

// TestRunScopedToggleProjectAllowsRepoRelativeRegistry verifies that enabling a
// skill at project scope from a repo-relative registry still works; only the
// global layer refuses the write.
func TestRunScopedToggleProjectAllowsRepoRelativeRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a local registry under the repo.
	regDir := filepath.Join(repoRoot, "plugins", "gitlab-mr")
	if err := os.MkdirAll(filepath.Join(regDir, "skills", "audit"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "skills", "audit", "SKILL.md"), []byte("---\nname: audit\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	act := toggleAction{
		skillName:   "audit",
		scope:       scopeProject,
		enable:      true,
		registryURL: "./plugins/gitlab-mr",
	}

	msg := runScopedToggle(repoRoot, act)()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err != nil {
		t.Fatalf("project-scope toggle of repo-relative registry should succeed: %v", cw.err)
	}
}
