package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// progressiveFixture builds a directory registry with two selected skills and
// swaps the layered config loaders to the sandbox. It mirrors the smallest
// actions_test fixture so the staged reader exercises real config load,
// catalog resolution, and dry-run assessment.
func progressiveFixture(t *testing.T) (repoRoot, homeDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	homeDir = t.TempDir()
	registryRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	for _, name := range []string{"audit", "review"} {
		skillDir := filepath.Join(registryRoot, "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir registry skill: %v", err)
		}
		content := "---\nname: " + name + "\ndescription: " + name + " workflow\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write skill: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	globalConfig := v2Header + `registries:
  - url: ` + registryRoot + `
    skills:
      - audit
      - review
`
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil }))
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	}))
	return repoRoot, homeDir
}

func collectSnapshotStages(stages <-chan snapshotLoadedMsg) []snapshotLoadedMsg {
	var msgs []snapshotLoadedMsg
	for msg := range stages {
		msgs = append(msgs, msg)
	}
	return msgs
}

// The progressive snapshot publishes a validated-config stage, then a verified
// inventory with drift still pending, and only then the assessed result.
func TestReadSnapshotStagesDeliversConfigInventoryThenAssessed(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)

	msgs := collectSnapshotStages(readSnapshotStages(context.Background(), repoRoot, homeDir, catalogLoadCached, 7))
	if len(msgs) != 3 {
		t.Fatalf("staged snapshot produced %d messages, want config+inventory+final: %#v", len(msgs), msgs)
	}
	for _, msg := range msgs {
		if msg.generation != 7 {
			t.Fatalf("stage %d carried generation %d, want 7", msg.stage, msg.generation)
		}
	}
	configStage, inventory, final := msgs[0], msgs[1], msgs[2]
	if configStage.stage != snapshotStageConfig || configStage.mergedCfg == nil {
		t.Fatalf("first stage = %+v, want validated config", configStage)
	}
	if len(configStage.rows) != 0 {
		t.Fatalf("config stage published %d rows before inventory", len(configStage.rows))
	}
	if inventory.stage != snapshotStageInventory {
		t.Fatalf("second stage = %d, want inventory", inventory.stage)
	}
	if len(inventory.rows) != 2 {
		t.Fatalf("inventory stage published %d rows, want 2", len(inventory.rows))
	}
	if inventory.driftAssessment != driftUnknown {
		t.Fatalf("inventory stage reported drift %d before assessment", inventory.driftAssessment)
	}
	if final.stage != snapshotStageFinal {
		t.Fatalf("last stage = %d, want final", final.stage)
	}
	if final.driftAssessment != driftReady || len(final.errs) != 0 {
		t.Fatalf("final stage drift=%d errors=%v, want assessed", final.driftAssessment, final.errs)
	}
	if len(final.rows) != 2 {
		t.Fatalf("final stage published %d rows, want 2", len(final.rows))
	}
}

// A failed config load still delivers a single fail-closed final stage.
func TestReadSnapshotStagesInvalidConfigStaysFailClosed(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	msgs := collectSnapshotStages(readSnapshotStages(context.Background(), repoRoot, homeDir, catalogLoadCached, 3))
	if len(msgs) != 1 {
		t.Fatalf("invalid config produced %d stages, want exactly one fail-closed final", len(msgs))
	}
	if msgs[0].stage != snapshotStageFinal || len(msgs[0].errs) == 0 {
		t.Fatalf("invalid config stage = %+v, want final with errors", msgs[0])
	}
}

func TestReadSnapshotStagesRejectsInvalidWorkspace(t *testing.T) {
	msgs := collectSnapshotStages(readSnapshotStagesForWorkspace(context.Background(), pipeline.Workspace{}, t.TempDir(), catalogLoadCached, 11))
	if len(msgs) != 1 || msgs[0].stage != snapshotStageFinal || msgs[0].generation != 11 || len(msgs[0].rows) != 0 || len(msgs[0].errs) != 1 || !errors.Is(msgs[0].errs[0], pipeline.ErrInvalidWorkspace) {
		t.Fatalf("invalid workspace stages = %+v, want one failed final", msgs)
	}
}

func TestReadSnapshotStagesUsesNestedLocalWorkspace(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	nested := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalConfigPath(nested), []byte("skills:\n  exclude: [audit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalConfigPath(repoRoot), []byte("skills:\n  exclude: [review]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatal(err)
	}
	originalLocal := loadLocalConfig
	t.Cleanup(func() { loadLocalConfig = originalLocal })
	loadLocalConfig = loadLocalConfigAt
	original := runMaterialize
	t.Cleanup(func() { runMaterialize = original })
	var materializeRoot string
	runMaterialize = func(_ context.Context, root string, _ []resolver.ResolvedArtifact, _ ...materialize.RunOption) (*materialize.Report, error) {
		materializeRoot = root
		return &materialize.Report{}, nil
	}
	msgs := collectSnapshotStages(readSnapshotStagesForWorkspace(context.Background(), ws, homeDir, catalogLoadCached, 13))
	if len(msgs) < 3 || msgs[len(msgs)-2].stage != snapshotStageInventory || msgs[len(msgs)-1].stage != snapshotStageFinal {
		t.Fatalf("nested workspace stages = %+v", msgs)
	}
	final := msgs[len(msgs)-1]
	if materializeRoot != nested || !slices.Equal(final.localExcludes, []string{"audit"}) || final.inventoryResult == nil || len(final.errs) != 0 {
		t.Fatalf("nested workspace root=%q excludes=%v errors=%v", materializeRoot, final.localExcludes, final.errs)
	}
}

// Once the validated-config stage lands, Marketplace discovery runs while the
// local catalog, detail capture, and dry run are still in flight.
func TestConfigStageStartsDiscoveryBeforeLocalVerification(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"}
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, cmd := m.Update(snapshotLoadedMsg{
		stage:      snapshotStageConfig,
		generation: generation,
		mergedCfg:  &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}},
		stages:     stages,
	})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("config stage did not schedule the next stage")
	}
	if m.mergedCfg == nil || len(m.mergedCfg.Marketplaces) != 1 {
		t.Fatalf("config stage did not publish merged config: %#v", m.mergedCfg)
	}
	if !m.discoveryLoading || m.requests.active(requestDiscovery) == 0 {
		t.Fatalf("config stage did not start Marketplace discovery: loading=%t generation=%d", m.discoveryLoading, m.requests.active(requestDiscovery))
	}

	// Cached results publish rows while the local snapshot is still loading.
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}})
	m = updated.(Model)
	if m.requests.active(requestSnapshot) != generation {
		t.Fatalf("snapshot generation advanced before assessment: %d", m.requests.active(requestSnapshot))
	}
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "remote" {
		t.Fatalf("cached Marketplace row not visible during local verification: %#v", m.rows)
	}
}

// The inventory stage makes verified rows navigable and config-only toggles
// eligible while drift stays explicitly pending.
func TestInventoryStageIsNavigableAndToggleReadyWithDriftPending(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	inventory := []row{mkRow("audit", withGlobal, withRegistry("sec.git", "security"))}

	updated, _ := m.Update(snapshotLoadedMsg{
		stage:           snapshotStageInventory,
		generation:      generation,
		mergedCfg:       &config.Config{},
		rows:            inventory,
		driftAssessment: driftUnknown,
		stages:          stages,
	})
	m = updated.(Model)
	if m.loading {
		t.Fatal("inventory stage left the model in the loading state")
	}
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "audit" {
		t.Fatalf("inventory rows = %#v", m.rows)
	}
	if m.driftAssessment != driftUnknown {
		t.Fatalf("inventory stage assessed drift early: %d", m.driftAssessment)
	}
	mi, _ := m.applyRowSpaceEligibility(m.rows[0])
	m = mi.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("inventory row was not toggle eligible; pane=%v", m.pane)
	}
}

// Drift assessment is keyed by kind/name against selected rows only. A
// same-name resource that is merely available from another catalog must not
// inherit the selected row's drift marker, while unavailable recovery rows
// keep the drift they already carry.
func TestApplyDriftItemsMarksOnlySelectedRows(t *testing.T) {
	key := skill.ResourceKey(ir.KindSkill, "shared")
	selected := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "selected-cat"}}
	available := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "other-cat"}, IsAvailable: true}
	recovery := row{
		Artifact:  resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "failed-cat"},
		DriftItem: &materialize.DriftItem{Name: key, Reason: materialize.DriftBroken},
	}
	report := materialize.DriftReport{Missing: []materialize.DriftItem{{Name: key, Reason: materialize.DriftMissing}}}

	out := applyDriftItems([]row{selected, available, recovery}, report)
	if out[0].DriftItem == nil || out[0].DriftItem.Reason != materialize.DriftMissing {
		t.Fatalf("selected row missing assessed drift: %#v", out[0].DriftItem)
	}
	if out[1].DriftItem != nil {
		t.Fatalf("available row adopted another catalog's drift: %#v", out[1].DriftItem)
	}
	if out[2].DriftItem == nil || out[2].DriftItem.Reason != materialize.DriftBroken {
		t.Fatalf("unavailable recovery row lost its drift: %#v", out[2].DriftItem)
	}
}

// The assessed stage replaces pending inventory rows and drift state.
func TestFinalStageReplacesPendingInventoryWithAssessment(t *testing.T) {
	m := newTestModel()
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)

	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit"), mkRow("review")}, driftAssessment: driftReady})
	m = updated.(Model)
	if m.driftAssessment != driftReady || len(m.rows) != 2 {
		t.Fatalf("final stage did not apply assessment: drift=%d rows=%#v", m.driftAssessment, m.rows)
	}
}

// A stage from a superseded generation cannot replace newer rows, merged
// config, or started discovery.
func TestSupersededStageDeliveryCannotOverwriteNewerState(t *testing.T) {
	m := newTestModel()
	stale := m.requests.active(requestSnapshot)
	current := m.beginRequest(requestSnapshot)
	kept := m.rows
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{
		stage:      snapshotStageConfig,
		generation: stale,
		mergedCfg:  &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "market"}}},
		stages:     stages,
	})
	m = updated.(Model)
	if m.mergedCfg != nil || m.requests.active(requestDiscovery) != 0 {
		t.Fatalf("stale config stage changed config or started discovery: cfg=%#v discovery=%d", m.mergedCfg, m.requests.active(requestDiscovery))
	}

	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: stale, mergedCfg: &config.Config{}, rows: []row{mkRow("stale")}, stages: stages})
	m = updated.(Model)
	if len(m.rows) != len(kept) || m.rows[0].Artifact.Name != "a" {
		t.Fatalf("stale inventory stage replaced rows: %#v", m.rows)
	}
	if m.requests.active(requestSnapshot) != current {
		t.Fatalf("stale stage consumed the active generation: %d", m.requests.active(requestSnapshot))
	}
}

// An inventory delivery must not finish a mutation's pending authoritative
// reload; only the assessed stage completes it.
func TestInventoryStageDoesNotCompletePendingToggleReload(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start mutation operation")
	}
	m.pendingToggleOperation = operation
	generation := m.requests.active(requestSnapshot)
	m.pendingToggleReload = generation
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, stages: stages})
	m = updated.(Model)
	if m.operations.active == nil || m.pendingToggleOperation != operation {
		t.Fatal("inventory stage released the pending mutation operation")
	}

	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, driftAssessment: driftReady})
	m = updated.(Model)
	if m.operations.active != nil || m.pendingToggleOperation != 0 {
		t.Fatalf("final stage did not finish the pending reload: active=%#v pending=%d", m.operations.active, m.pendingToggleOperation)
	}
}

// Discovery rows published during the config stage survive the assessed
// rebuild instead of being cleared by late authoritative rows.
func TestFinalStageMergesDiscoveryPublishedDuringConfigStage(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"}
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, stages: stages})
	m = updated.(Model)
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)

	enabled := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "audit"}}, EnabledGlobal: true}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, rows: []row{enabled}, driftAssessment: driftReady})
	m = updated.(Model)
	if len(m.rows) != 2 {
		t.Fatalf("final stage dropped discovery rows published during config stage: %#v", m.rows)
	}
	names := map[string]bool{}
	for _, r := range m.rows {
		names[r.Artifact.Name] = true
	}
	if !names["audit"] || !names["remote"] {
		t.Fatalf("final rows = %#v, want audit plus remote", m.rows)
	}
}
