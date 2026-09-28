package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

func TestReadSnapshotStagesDeliversConfigInventoryThenAssessed(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)

	msgs := collectSnapshotStages(readSnapshotStages(context.Background(), repoRoot, homeDir, catalogLoadCached, 7))
	if len(msgs) != 4 {
		t.Fatalf("staged snapshot produced %d messages, want provisional+config+inventory+final: %#v", len(msgs), msgs)
	}
	for _, msg := range msgs {
		if msg.generation != 7 {
			t.Fatalf("stage %d carried generation %d, want 7", msg.stage, msg.generation)
		}
	}
	provisional, configStage, inventory, final := msgs[0], msgs[1], msgs[2], msgs[3]
	if provisional.stage != snapshotStageConfig || provisional.provisionalCfg == nil || provisional.mergedCfg != nil {
		t.Fatalf("first stage = %+v, want provisional configured selections", provisional)
	}
	for _, r := range provisional.provisionalRows {
		if !r.PendingVerification {
			t.Fatalf("provisional stage published non-pending row %#v", r.Artifact)
		}
	}
	if configStage.stage != snapshotStageConfig || configStage.mergedCfg == nil {
		t.Fatalf("second stage = %+v, want validated config", configStage)
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

func TestUntouchedCursorStaysOnTopWhenInstalledRowsArrive(t *testing.T) {
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
	var items []mpapi.ResourceListItem
	for i := range 30 {
		items = append(items, mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: fmt.Sprintf("remote-%02d", i), Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable})
	}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: items, Total: len(items), CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d before installed rows, want 0", m.cursor)
	}

	var installed []row
	for i := range 20 {
		installed = append(installed, mkRow(fmt.Sprintf("installed-%02d", i), withGlobal))
	}
	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, rows: installed, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)
	if m.cursor != 0 || m.listOffset != 0 {
		t.Fatalf("installed rows moved the untouched cursor: cursor=%d offset=%d item=%q", m.cursor, m.listOffset, flatItemKey(m.flatItems[m.cursor]))
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, rows: installed, driftAssessment: driftReady})
	m = updated.(Model)
	if m.cursor != 0 || m.listOffset != 0 {
		t.Fatalf("final stage moved the untouched cursor: cursor=%d offset=%d", m.cursor, m.listOffset)
	}
}

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
	if footer := renderFooter(m); !strings.Contains(footer, "verifying resources...") || !strings.Contains(footer, m.spinner.View()) {
		t.Fatalf("inventory stage lost startup progress: %q", footer)
	}
	mi, _ := m.applyRowSpaceEligibility(m.rows[0])
	m = mi.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("inventory row was not toggle eligible; pane=%v", m.pane)
	}
}

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
	if footer := renderFooter(m); strings.Contains(footer, "verifying resources...") {
		t.Fatalf("completed assessment still shows startup progress: %q", footer)
	}
}

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

func TestProvisionalConfigStagePublishesPendingRows(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:    "https://market.example",
		Source: config.Organization,
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "tools", Kind: "skill", Name: "remote",
			Revision: "sha256:" + strings.Repeat("a", 64), Direct: true,
		}},
	}}}

	updated, cmd := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: buildProvisionalRows(provisional, nil, nil), stages: stages})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("provisional stage did not schedule the next stage")
	}
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "remote" {
		t.Fatalf("provisional stage published rows %#v, want the configured remote selection", m.rows)
	}
	pending := m.rows[0]
	if !pending.PendingVerification {
		t.Fatal("provisional row is not marked verification-pending")
	}
	if pending.Glyph() == "[x]" || pending.StatusText() == "enabled" {
		t.Fatalf("provisional row fabricates installed state: glyph=%q status=%q", pending.Glyph(), pending.StatusText())
	}
	if badge := originBadge(pending); badge != "" {
		t.Fatalf("provisional row fabricates an enabled badge %q", badge)
	}

	mi, _ := m.applyRowSpaceEligibility(pending)
	m = mi.(Model)
	if m.pane == paneToggleModal {
		t.Fatal("verification-pending row opened the toggle modal before verification")
	}

	verified := row{Artifact: resolver.ResolvedArtifact{
		Entry:         catalog.Entry{Kind: ir.KindSkill, Name: "remote"},
		IsMarketplace: true, MarketplaceInstanceID: "mp", MarketplaceRegistryID: "tools",
		MarketplaceResourceName: "remote",
	}, EnabledGlobal: true}
	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: provisional, rows: []row{verified}, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)
	if len(m.rows) != 1 || m.rows[0].PendingVerification {
		t.Fatalf("verified inventory did not replace the provisional row: %#v", m.rows)
	}
	mi, _ = m.applyRowSpaceEligibility(m.rows[0])
	m = mi.(Model)
	if m.pane != paneToggleModal {
		t.Fatal("verified row lost toggle eligibility")
	}
}

func TestStaleProvisionalStageCannotPublishRows(t *testing.T) {
	m := newTestModel()
	stale := m.requests.active(requestSnapshot)
	m.beginRequest(requestSnapshot)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:       "https://market.example",
		Resources: []config.MarketplaceResourcePin{{RegistryID: "tools", Kind: "skill", Name: "remote"}},
	}}}

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: stale, provisionalCfg: provisional, provisionalRows: buildProvisionalRows(provisional, nil, nil)})
	m = updated.(Model)
	for _, r := range m.rows {
		if r.PendingVerification {
			t.Fatalf("stale provisional stage published row %#v", r)
		}
	}
}

func TestProvisionalConfiguredRowsRenderBeforeBlockedAcquisition(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	unblock := blockedOrganizationMarketplace(t)

	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	m.width, m.height = 80, 24
	generation := m.beginRequest(requestSnapshot)
	if footer := renderFooter(m); !strings.Contains(footer, "loading resources...") || strings.Contains(footer, "verifying resources...") {
		t.Fatalf("startup before configured rows has incorrect status: %q", footer)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stages := readSnapshotStages(ctx, repoRoot, homeDir, catalogLoadCached, generation)
	drainStagesAfterRelease(t, unblock, cancel, stages)

	var first snapshotLoadedMsg
	select {
	case first = <-stages:
	case <-time.After(30 * time.Second):
		t.Fatal("no provisional config stage published while organization acquisition was blocked")
	}
	if first.stage != snapshotStageConfig || first.provisionalCfg == nil {
		t.Fatalf("first stage = %+v, want provisional configured selections", first)
	}
	if len(first.provisionalCfg.Marketplaces) != 1 || len(first.provisionalCfg.Marketplaces[0].Resources) != 1 {
		t.Fatalf("provisional config lost the configured pin: %#v", first.provisionalCfg.Marketplaces)
	}
	if first.mergedCfg != nil {
		t.Fatal("provisional stage claimed a verified merged config before acquisition")
	}

	updated, _ := m.Update(first)
	m = updated.(Model)
	pendingRow := -1
	for i, r := range m.rows {
		if !r.PendingVerification {
			t.Fatalf("pre-verification row %#v is not marked pending", r.Artifact)
		}
		if r.Artifact.Name == "remote" && r.Artifact.IsMarketplace {
			pendingRow = i
		}
	}
	if pendingRow < 0 {
		t.Fatalf("configured Marketplace selection did not render pending verification while blocked: %#v", m.rows)
	}
	select {
	case extra, ok := <-stages:
		t.Fatalf("resolve advanced past acquisition while blocked: %+v closed=%t", extra, !ok)
	default:
	}
	view := m.View()
	if !strings.Contains(view, "remote") {
		t.Fatalf("rendered view lacks the pending configured row:\n%s", view)
	}
	if !strings.Contains(view, "verifying resources...") || strings.Contains(view, "loading resources...") {
		t.Fatalf("rendered view lacks verification progress while acquisition is blocked:\n%s", view)
	}
	if strings.Contains(view, "[x] remote") || strings.Contains(view, "[x]  skill remote") {
		t.Fatalf("pending row claims installed state:\n%s", view)
	}
}

func writeProvisionalSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+name+" workflow\n---\n# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write skill %s: %v", name, err)
	}
}

func blockedOrganizationMarketplace(t *testing.T) (unblock func()) {
	t.Helper()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock = func() { releaseOnce.Do(func() { close(release) }) }
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			select {
			case <-release:
			case <-r.Context().Done():
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.Close)

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + fixture.URL + "}\nresources:\n  mp: {include: [tools/skill/remote]}\n"
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatalf("write organization config: %v", err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) {
		return config.LoadOrganizationConfig()
	}))
	return unblock
}

func drainStagesAfterRelease(t *testing.T, unblock func(), cancel context.CancelFunc, stages <-chan snapshotLoadedMsg) {
	t.Helper()
	t.Cleanup(func() {
		unblock()
		cancel()
		done := make(chan struct{})
		go func() {
			for range stages {
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("stage producer did not finish after release and cancellation")
		}
	})
}

func provisionalLocalAllTrueFixture(t *testing.T) (repoRoot, homeDir, regA, regB string) {
	t.Helper()
	repoRoot = t.TempDir()
	homeDir = t.TempDir()
	t.Setenv("HOME", homeDir)

	regA = t.TempDir()
	writeProvisionalSkill(t, regA, "alpha")
	writeProvisionalSkill(t, regA, "beta")
	regB = t.TempDir()
	for _, name := range []string{"gamma", "delta", "sigma"} {
		writeProvisionalSkill(t, regB, name)
	}
	if err := os.MkdirAll(filepath.Join(regB, "commands"), 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regB, "commands", "deploy.md"), []byte("---\nname: deploy\ndescription: Deploy workflow\n---\n# Deploy\n"), 0o644); err != nil {
		t.Fatalf("write command: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(homeDir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	globalConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  rega: {directory: " + regA + "}\nresources:\n  rega: {all: true}\n"
	if err := os.WriteFile(filepath.Join(homeDir, ".agents", "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	repoConfig := "apiVersion: agent-marketplace/v4\nsources:\n  regb: {directory: " + regB + "}\nresources:\n  regb: {all: true, exclude: [skill/delta]}\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"), []byte("skills:\n  exclude:\n    - gamma\n"), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	}))
	return repoRoot, homeDir, regA, regB
}

func newProvisionalStageModel() Model {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	m.width, m.height = 120, 40
	return m
}

func pendingRowNames(rows []row) map[string]ir.ResourceKind {
	got := make(map[string]ir.ResourceKind, len(rows))
	for _, r := range rows {
		got[r.Artifact.Name] = r.Artifact.Kind
	}
	return got
}

func TestProvisionalLocalAllTrueRowsRenderBeforeBlockedAcquisition(t *testing.T) {
	repoRoot, homeDir, _, _ := provisionalLocalAllTrueFixture(t)
	unblock := blockedOrganizationMarketplace(t)

	m := newProvisionalStageModel()
	generation := m.beginRequest(requestSnapshot)

	ctx, cancel := context.WithCancel(context.Background())
	stages := readSnapshotStages(ctx, repoRoot, homeDir, catalogLoadCached, generation)
	drainStagesAfterRelease(t, unblock, cancel, stages)

	var first snapshotLoadedMsg
	select {
	case first = <-stages:
	case <-time.After(30 * time.Second):
		t.Fatal("no provisional config stage published while organization acquisition was blocked")
	}
	if first.stage != snapshotStageConfig || first.provisionalCfg == nil {
		t.Fatalf("first stage = %+v, want provisional configured selections", first)
	}
	if first.mergedCfg != nil {
		t.Fatal("provisional stage claimed a verified merged config before acquisition")
	}

	updated, _ := m.Update(first)
	m = updated.(Model)
	want := map[string]ir.ResourceKind{
		"alpha":  ir.KindSkill,
		"beta":   ir.KindSkill,
		"sigma":  ir.KindSkill,
		"deploy": ir.KindCommand,
		"remote": ir.KindSkill,
	}
	for _, r := range m.rows {
		if !r.PendingVerification {
			t.Fatalf("pre-verification row %#v is not marked pending", r.Artifact)
		}
	}
	got := pendingRowNames(m.rows)
	for name, kind := range want {
		if got[name] != kind {
			t.Fatalf("configured %s %q missing from provisional rows while acquisition blocked: %#v", kind, name, m.rows)
		}
	}
	for _, excluded := range []string{"gamma", "delta"} {
		if _, present := got[excluded]; present {
			t.Fatalf("excluded resource %q rendered a provisional row", excluded)
		}
	}
	select {
	case extra, ok := <-stages:
		t.Fatalf("resolve advanced past acquisition while blocked: %+v closed=%t", extra, !ok)
	default:
	}
}

func TestProvisionalLocalAllTrueRowsRenderOnNormalConfigPath(t *testing.T) {
	repoRoot, homeDir, _, _ := provisionalLocalAllTrueFixture(t)
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil }))

	m := newProvisionalStageModel()
	generation := m.beginRequest(requestSnapshot)

	msgs := collectSnapshotStages(readSnapshotStages(context.Background(), repoRoot, homeDir, catalogLoadCached, generation))
	if len(msgs) < 2 {
		t.Fatalf("normal config path produced %d stages, want a provisional stage before the merged config", len(msgs))
	}
	provisional := msgs[0]
	if provisional.stage != snapshotStageConfig || provisional.provisionalCfg == nil {
		t.Fatalf("first stage = %+v, want provisional configured selections", provisional)
	}

	updated, _ := m.Update(provisional)
	m = updated.(Model)
	want := map[string]ir.ResourceKind{
		"alpha":  ir.KindSkill,
		"beta":   ir.KindSkill,
		"sigma":  ir.KindSkill,
		"deploy": ir.KindCommand,
	}
	for _, r := range m.rows {
		if !r.PendingVerification {
			t.Fatalf("pre-verification row %#v is not marked pending", r.Artifact)
		}
	}
	got := pendingRowNames(m.rows)
	for name, kind := range want {
		if got[name] != kind {
			t.Fatalf("configured %s %q missing from provisional rows on the normal config path: %#v", kind, name, m.rows)
		}
	}
	for _, excluded := range []string{"gamma", "delta"} {
		if _, present := got[excluded]; present {
			t.Fatalf("excluded resource %q rendered a provisional row", excluded)
		}
	}
	if provisional.mergedCfg != nil {
		t.Fatal("provisional stage claimed a verified merged config")
	}

	var mergedStage snapshotLoadedMsg
	for _, msg := range msgs[1:] {
		if msg.stage == snapshotStageConfig && msg.mergedCfg != nil {
			mergedStage = msg
			break
		}
	}
	if mergedStage.mergedCfg == nil {
		t.Fatal("no merged config stage followed the provisional stage")
	}
	updated, _ = m.Update(mergedStage)
	m = updated.(Model)
	got = pendingRowNames(m.rows)
	for name := range want {
		if _, present := got[name]; !present {
			t.Fatalf("merged config stage downgraded provisional row %q: %#v", name, m.rows)
		}
	}
}

func TestExcludedOnlyProvisionalStagePublishesNoRows(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	reg := t.TempDir()
	writeProvisionalSkill(t, reg, "solo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	repoConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  reg: {directory: " + reg + "}\nresources:\n  reg: {include: [skill/solo]}\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"), []byte("skills:\n  exclude:\n    - solo\n"), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil }))
	t.Cleanup(swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil }))
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	}))

	m := newProvisionalStageModel()
	generation := m.beginRequest(requestSnapshot)

	msgs := collectSnapshotStages(readSnapshotStages(context.Background(), repoRoot, homeDir, catalogLoadCached, generation))
	if len(msgs) == 0 || msgs[0].provisionalCfg == nil {
		t.Fatalf("no provisional config stage produced: %#v", msgs)
	}
	provisional := msgs[0]
	if provisional.provisionalRows != nil {
		t.Fatalf("excluded-only provisional stage emitted rows %#v, want nil", provisional.provisionalRows)
	}

	updated, _ := m.Update(provisional)
	m = updated.(Model)
	for _, r := range m.rows {
		if r.PendingVerification {
			t.Fatalf("excluded selection %q resurrected as a verification-pending row", r.Artifact.Name)
		}
	}
}

func TestProvisionalRegistryScopedLocalExcludesApply(t *testing.T) {
	repoRoot, homeDir, regA, regB := provisionalLocalAllTrueFixture(t)
	regC := t.TempDir()
	for _, reg := range []string{regA, regB, regC} {
		writeProvisionalSkill(t, reg, "shared")
	}
	repoConfig := "apiVersion: agent-marketplace/v4\nsources:\n  regb: {directory: " + regB + "}\n  regc: {directory: " + regC + "}\nresources:\n  regb: {all: true}\n  regc: {all: true}\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	localConfig := "registries:\n  - url: " + regA + "\n    skills: {exclude: [shared]}\n  - url: " + regB + "\n    skills: {exclude: [shared]}\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "config.local.yaml"), []byte(localConfig), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	unblock := blockedOrganizationMarketplace(t)

	m := newProvisionalStageModel()
	generation := m.beginRequest(requestSnapshot)

	ctx, cancel := context.WithCancel(context.Background())
	stages := readSnapshotStages(ctx, repoRoot, homeDir, catalogLoadCached, generation)
	drainStagesAfterRelease(t, unblock, cancel, stages)

	var first snapshotLoadedMsg
	select {
	case first = <-stages:
	case <-time.After(30 * time.Second):
		t.Fatal("no provisional config stage published while organization acquisition was blocked")
	}
	if first.stage != snapshotStageConfig || first.provisionalCfg == nil {
		t.Fatalf("first stage = %+v, want provisional configured selections", first)
	}

	updated, _ := m.Update(first)
	m = updated.(Model)
	type scopedName struct {
		name string
		url  string
	}
	got := make(map[scopedName]bool)
	for _, r := range m.rows {
		if !r.PendingVerification {
			t.Fatalf("pre-verification row %#v is not marked pending", r.Artifact)
		}
		got[scopedName{r.Artifact.Name, r.RegistryURL}] = true
	}
	for _, want := range []scopedName{
		{"shared", regA},
		{"alpha", regA},
		{"beta", regA},
		{"sigma", regB},
		{"deploy", regB},
		{"shared", regC},
	} {
		if !got[want] {
			t.Fatalf("pending row %q from %q missing: %#v", want.name, want.url, m.rows)
		}
	}
	if got[scopedName{"shared", regB}] {
		t.Fatalf("registry-scoped local exclude did not hide %q from %q: %#v", "shared", regB, m.rows)
	}
}

func TestAuthoritativeFailureClearsProvisionalRows(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "tools", Kind: "skill", Name: "remote",
			Revision: "sha256:" + strings.Repeat("a", 64), Direct: true,
		}},
	}}}

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: buildProvisionalRows(provisional, nil, nil), stages: stages})
	m = updated.(Model)
	if len(m.rows) != 1 || !m.rows[0].PendingVerification {
		t.Fatalf("provisional stage did not publish pending rows: %#v", m.rows)
	}

	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, errs: []error{errors.New("organization Marketplace acquisition failed")}})
	m = updated.(Model)
	for _, r := range m.rows {
		if r.PendingVerification {
			t.Fatalf("authoritative failure left verification-pending row %#v", r)
		}
	}
	if len(m.errs) == 0 {
		t.Fatal("authoritative failure dropped the recoverable diagnostics")
	}
}

func TestPendingDetailFollowsVerifiedRow(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	revision := "sha256:" + strings.Repeat("a", 64)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "tools", Kind: "skill", Name: "remote",
			Revision: revision, Direct: true,
		}},
	}}}

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: buildProvisionalRows(provisional, nil, nil), stages: stages})
	m = updated.(Model)
	if len(m.rows) != 1 || !m.rows[0].PendingVerification {
		t.Fatalf("provisional stage did not publish pending rows: %#v", m.rows)
	}
	m.pane = paneDetail
	m.detailKey = rowKey(m.rows[0])

	verified := mkRow("remote", withKind(ir.KindSkill), withGlobal)
	verified.RegistryURL = "https://market.example"
	verified.Artifact.RegistryPath = "tools"
	verified.Artifact.Ref = revision
	verified.Artifact.IsMarketplace = true
	verified.Artifact.MarketplaceInstanceID = "mp"
	verified.Artifact.MarketplaceRegistryID = "tools"
	verified.Artifact.MarketplaceResourceName = "remote"
	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: provisional, rows: []row{verified}, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)
	if m.detailKey == "" {
		t.Fatal("verified inventory cleared the open detail for the pending selection")
	}
	if m.detailKey != rowKey(verified) {
		t.Fatalf("detail key %q did not follow the verified row %q", m.detailKey, rowKey(verified))
	}
}

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

func TestSyncKeyBlockedBeforeVerifiedInventory(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, mergedCfg: &config.Config{}, stages: stages})
	m = updated.(Model)

	updated, cmd := m.Update(keyPress('s'))
	m = updated.(Model)
	if m.syncing || cmd != nil {
		t.Fatalf("sync key started a mutation before verified inventory: syncing=%t cmd=%v", m.syncing, cmd)
	}
	updated, cmd = m.Update(keyPress('u'))
	m = updated.(Model)
	if m.syncing || cmd != nil {
		t.Fatalf("update key started a mutation before verified inventory: syncing=%t cmd=%v", m.syncing, cmd)
	}

	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)
	updated, cmd = m.Update(keyPress('s'))
	m = updated.(Model)
	if !m.syncing || cmd == nil {
		t.Fatalf("verified inventory lost sync eligibility: syncing=%t cmd=%v", m.syncing, cmd)
	}
}

func TestDiscoveryRowCannotInstallBeforeVerifiedInventory(t *testing.T) {
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

	target := -1
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact && it.Row.MarketplaceDiscovery != nil {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("cached discovery row missing before verification: %#v", m.flatItems)
	}
	m.cursor = target

	updated, _ = m.Update(keyPress(' '))
	m = updated.(Model)
	if m.pane == paneToggleModal {
		t.Fatal("Space opened the install scope modal on an unverified discovery row")
	}
}
