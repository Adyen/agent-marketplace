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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/charmbracelet/x/ansi"
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

// spec: session-startup/startup-snapshot-delivers-config-inventory-then-assessed-stages
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

type snapshotReadCounters struct {
	global      atomic.Int64
	repo        atomic.Int64
	discoveries atomic.Int64
	dryRuns     atomic.Int64
}

type snapshotReadTotals struct {
	global, repo, discoveries, dryRuns int64
}

func (c *snapshotReadCounters) totals() snapshotReadTotals {
	return snapshotReadTotals{
		global:      c.global.Load(),
		repo:        c.repo.Load(),
		discoveries: c.discoveries.Load(),
		dryRuns:     c.dryRuns.Load(),
	}
}

func snapshotRowIdentities(rows []row) []string {
	identities := make([]string, 0, len(rows))
	for _, r := range rows {
		identities = append(identities, fmt.Sprintf("%s|%s|%s|%s", r.Artifact.Kind, r.Artifact.Name, r.Artifact.RegistryURL, r.RegistryURL))
	}
	slices.Sort(identities)
	return identities
}

func snapshotRowNames(rows []row) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Artifact.Name)
	}
	slices.Sort(names)
	return names
}

func snapshotDriftIdentities(report materialize.DriftReport) []string {
	var identities []string
	appendItems := func(category string, items []materialize.DriftItem) {
		for _, item := range items {
			identities = append(identities, fmt.Sprintf("%s|%s|%s", category, item.Name, item.Reason))
		}
	}
	appendItems("missing", report.Missing)
	appendItems("orphaned", report.Orphaned)
	appendItems("broken", report.Broken)
	appendItems("synced", report.Synced)
	slices.Sort(identities)
	return identities
}

// spec: session-startup/ordinary-reload-keeps-the-current-view-until-final-assessment
func TestOrdinaryReloadAvoidsProgressiveSnapshotWork(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	repoPath := config.ProjectConfigPath(repoRoot)
	globalBefore, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	repoBefore, err := os.ReadFile(repoPath)
	if err != nil {
		t.Fatal(err)
	}

	counts := &snapshotReadCounters{}
	originalGlobal, originalRepo := session.LoadGlobalConfig, session.LoadRepoConfig
	originalDiscover, originalMaterialize := session.DiscoverCatalog, session.RunMaterialize
	session.LoadGlobalConfig = func() (*config.Config, error) {
		counts.global.Add(1)
		return originalGlobal()
	}
	session.LoadRepoConfig = func() (*config.Config, error) {
		counts.repo.Add(1)
		return originalRepo()
	}
	session.DiscoverCatalog = func(dir, sourceHarness string) (catalog.LoadedCatalog, error) {
		counts.discoveries.Add(1)
		return originalDiscover(dir, sourceHarness)
	}
	session.RunMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		counts.dryRuns.Add(1)
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() {
		session.LoadGlobalConfig, session.LoadRepoConfig = originalGlobal, originalRepo
		session.DiscoverCatalog, session.RunMaterialize = originalDiscover, originalMaterialize
	})

	m := NewModel(repoRoot, homeDir)
	m.loading = false
	m.rows = []row{mkRow("previous", withGlobal, withRegistry("previous.git", "previous"))}
	m.authoritativeRows = slices.Clone(m.rows)
	m.recomputeView()
	generation := m.requests.Active(requestSnapshot)
	beforeView := ansi.Strip(m.viewContent())
	if !strings.Contains(beforeView, "previous") {
		t.Fatalf("ordinary reload fixture has no prior visible row:\n%s", beforeView)
	}
	cmd := loadSnapshot(context.Background(), m.workspace, homeDir, generation)
	ordinary, ok := cmd().(snapshotLoadedMsg)
	if !ok || ordinary.stage != snapshotStageFinal {
		t.Fatalf("ordinary reload result = %#v, want one final snapshot", ordinary)
	}
	if afterCommand := ansi.Strip(m.viewContent()); !strings.Contains(afterCommand, "previous") {
		t.Fatalf("ordinary reload changed the view before final acceptance:\n%s", afterCommand)
	}
	normalWork := counts.totals()
	if normalWork.discoveries != 1 {
		t.Fatalf("ordinary reload catalog discovery calls = %d, want only the authoritative discovery", normalWork.discoveries)
	}
	if normalWork.dryRuns != 1 {
		t.Fatalf("ordinary reload dry-run calls = %d, want 1", normalWork.dryRuns)
	}

	updated, _ := m.Update(ordinary)
	m = updated.(Model)
	if strings.Contains(ansi.Strip(m.viewContent()), "previous") {
		t.Fatal("accepted ordinary reload retained its previous rows")
	}

	progressive := collectSnapshotStages(readSnapshotStagesForWorkspace(context.Background(), m.workspace, homeDir, catalogLoadCached, generation+1))
	progressiveWork := counts.totals()
	if progressiveWork.discoveries-normalWork.discoveries < 1 || progressiveWork.global-normalWork.global < 1 || progressiveWork.repo-normalWork.repo < 1 {
		t.Fatalf("progressive-only callbacks did not run: ordinary=%+v progressive=%+v", normalWork, progressiveWork)
	}
	if progressiveWork.dryRuns-normalWork.dryRuns != 1 {
		t.Fatalf("progressive final dry-run delta = %d, want 1", progressiveWork.dryRuns-normalWork.dryRuns)
	}
	if len(progressive) != 4 || progressive[0].stage != snapshotStageConfig || progressive[0].provisionalCfg == nil || progressive[1].mergedCfg == nil || progressive[2].stage != snapshotStageInventory || progressive[3].stage != snapshotStageFinal {
		t.Fatalf("progressive startup stages = %+v, want provisional/config/inventory/final", progressive)
	}
	if names := snapshotRowNames(progressive[0].provisionalRows); !slices.Equal(names, []string{"audit", "review"}) {
		t.Fatalf("progressive pending names = %v, want audit and review", names)
	}
	for _, pending := range progressive[0].provisionalRows {
		if !pending.PendingVerification {
			t.Fatalf("progressive pending row is not marked pending: %#v", pending.Artifact)
		}
	}
	final := progressive[len(progressive)-1]
	if names := snapshotRowNames(final.rows); !slices.Equal(names, []string{"audit", "review"}) {
		t.Fatalf("progressive final names = %v, want audit and review", names)
	}
	if !slices.Equal(snapshotRowIdentities(ordinary.rows), snapshotRowIdentities(final.rows)) {
		t.Fatalf("ordinary and progressive final rows differ: ordinary=%v progressive=%v", snapshotRowIdentities(ordinary.rows), snapshotRowIdentities(final.rows))
	}
	if ordinary.driftAssessment != final.driftAssessment || !slices.Equal(snapshotDriftIdentities(ordinary.driftReport), snapshotDriftIdentities(final.driftReport)) {
		t.Fatalf("ordinary and progressive final drift differ: ordinary=%+v/%d progressive=%+v/%d", ordinary.driftReport, ordinary.driftAssessment, final.driftReport, final.driftAssessment)
	}
	if after, err := os.ReadFile(globalPath); err != nil || string(after) != string(globalBefore) {
		t.Fatalf("global config changed during snapshot reads: %v", err)
	}
	if after, err := os.ReadFile(repoPath); err != nil || string(after) != string(repoBefore) {
		t.Fatalf("project config changed during snapshot reads: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "audit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot read materialized a resource or failed to stat it: %v", err)
	}
}

// spec: session-startup/invalid-configuration-delivers-one-fail-closed-final-stage
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
	originalLocal := session.LoadLocalConfig
	t.Cleanup(func() { session.LoadLocalConfig = originalLocal })
	session.LoadLocalConfig = session.LoadLocalConfigAt
	original := session.RunMaterialize
	t.Cleanup(func() { session.RunMaterialize = original })
	var materializeRoot string
	session.RunMaterialize = func(_ context.Context, root string, _ []resolver.ResolvedArtifact, _ ...materialize.RunOption) (*materialize.Report, error) {
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

func TestReadSnapshotStagesProvisionalCatalogsUseWorkspaceLayerRoots(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	nested := filepath.Join(repoRoot, "nested")
	for _, fixture := range []struct{ root, directory, name string }{
		{repoRoot, "repo-registry", "repo-only"},
		{nested, "local-registry", "local-only"},
	} {
		dir := filepath.Join(fixture.root, fixture.directory, "skills", fixture.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+fixture.name+"\ndescription: workspace root fixture\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(swapLoadRepoConfig(func() (*config.Config, error) {
		return &config.Config{Registries: []config.RegistryEntry{{URL: "repo-registry", Default: true, V4Kind: config.V4SourceDirectory}}}, nil
	}))
	t.Cleanup(swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return &config.LocalConfig{Registries: []config.RegistryEntry{{URL: "local-registry", Default: true, V4Kind: config.V4SourceDirectory}}}, nil
	}))
	ws, err := pipeline.NewWorkspace(repoRoot, nested)
	if err != nil {
		t.Fatal(err)
	}
	msgs := collectSnapshotStages(readSnapshotStagesForWorkspace(context.Background(), ws, homeDir, catalogLoadCached, 17))
	var provisional []row
	for _, msg := range msgs {
		if msg.provisionalCfg != nil {
			provisional = msg.provisionalRows
		}
	}
	for _, stage := range []struct {
		name string
		rows []row
	}{{"provisional", provisional}, {"final", msgs[len(msgs)-1].rows}} {
		var names []string
		for _, row := range stage.rows {
			names = append(names, row.Artifact.Name)
		}
		if !slices.Contains(names, "repo-only") || !slices.Contains(names, "local-only") {
			t.Fatalf("%s inventory = %v, want both workspace layer sources", stage.name, names)
		}
	}
}

// Once the validated-config stage lands, Marketplace discovery runs while the
// local catalog, detail capture, and dry run are still in flight.
// spec: session-startup/config-stage-starts-cached-first-marketplace-discovery-before-local-verification-finishes
func TestConfigStageStartsDiscoveryBeforeLocalVerification(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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
	if !m.discoveryLoading || m.requests.Active(requestDiscovery) == 0 {
		t.Fatalf("config stage did not start Marketplace discovery: loading=%t generation=%d", m.discoveryLoading, m.requests.Active(requestDiscovery))
	}

	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.Active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}})
	m = updated.(Model)
	if m.requests.Active(requestSnapshot) != generation {
		t.Fatalf("snapshot generation advanced before assessment: %d", m.requests.Active(requestSnapshot))
	}
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "remote" {
		t.Fatalf("cached Marketplace row not visible during local verification: %#v", m.rows)
	}
}

// spec: session-startup/selection-follows-its-resource-when-rows-are-inserted
// spec: tui-startup/verified-rows-inserted-above-a-selection-keep-it-on-the-same-screen-line
func TestVerifiedRowsInsertedAboveSelectionKeepItOnTheSameScreenLine(t *testing.T) {
	m := newProvisionalStageModel()
	generation := m.requests.Active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	var provisional []row
	for i := range 30 {
		provisional = append(provisional, mkRow(fmt.Sprintf("g-%02d", i), withGlobal, func(r *row) { r.PendingVerification = true }))
	}
	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: &config.Config{}, provisionalRows: provisional, stages: stages})
	m = updated.(Model)
	if !m.restoreSelectionKey("row\x00" + rowKey(provisional[25])) {
		t.Fatal("fixture row g-25 is not selectable")
	}
	const line = 10
	m.listOffset = m.cursor - line
	selected := m.selectionKey()

	verified := make([]row, 0, 35)
	for i := range 5 {
		verified = append(verified, mkRow(fmt.Sprintf("a-%02d", i), withGlobal))
	}
	for i := range 30 {
		verified = append(verified, mkRow(fmt.Sprintf("g-%02d", i), withGlobal))
	}
	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{}, rows: verified, driftAssessment: driftUnknown, stages: stages})
	m = updated.(Model)
	if m.selectionKey() != selected || m.cursor-m.listOffset != line {
		t.Fatalf("selection %q moved to %q on screen line %d, want line %d", selected, m.selectionKey(), m.cursor-m.listOffset, line)
	}
}

// spec: session-startup/untouched-selection-remains-at-the-top-as-rows-arrive
// spec: tui-startup/an-untouched-cursor-stays-on-top-when-installed-rows-arrive
func TestUntouchedCursorStaysOnTopWhenInstalledRowsArrive(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.Active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
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

// spec: session-startup/verified-inventory-is-navigable-while-drift-is-pending
// spec: tui-startup/verified-inventory-is-navigable-and-toggle-ready-while-drift-is-pending
func TestInventoryStageIsNavigableAndToggleReadyWithDriftPending(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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

// spec: session-startup/drift-applies-only-to-rows-of-its-own-catalog-selection
func TestApplyDriftItemsMarksOnlySelectedRows(t *testing.T) {
	key := skill.ResourceKey(ir.KindSkill, "shared")
	selected := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "selected-cat"}}
	available := row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "other-cat"}, IsAvailable: true}
	recovery := row{
		Artifact:  resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "shared", Path: "skills/shared"}, CatalogKey: "failed-cat"},
		DriftItem: &materialize.DriftItem{Name: key, Reason: materialize.DriftBroken},
	}
	report := materialize.DriftReport{Missing: []materialize.DriftItem{{Name: key, Reason: materialize.DriftMissing}}}

	out := session.ApplyDriftItems([]row{selected, available, recovery}, report)
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

// spec: session-startup/final-stage-replaces-pending-inventory-with-the-assessment
func TestFinalStageReplacesPendingInventoryWithAssessment(t *testing.T) {
	m := newTestModel()
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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

// spec: session-startup/superseded-stage-delivery-cannot-overwrite-newer-state
func TestSupersededStageDeliveryCannotOverwriteNewerState(t *testing.T) {
	m := newTestModel()
	stale := m.requests.Active(requestSnapshot)
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
	if m.mergedCfg != nil || m.requests.Active(requestDiscovery) != 0 {
		t.Fatalf("stale config stage changed config or started discovery: cfg=%#v discovery=%d", m.mergedCfg, m.requests.Active(requestDiscovery))
	}

	updated, _ = m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: stale, mergedCfg: &config.Config{}, rows: []row{mkRow("stale")}, stages: stages})
	m = updated.(Model)
	if len(m.rows) != len(kept) || m.rows[0].Artifact.Name != "a" {
		t.Fatalf("stale inventory stage replaced rows: %#v", m.rows)
	}
	if m.requests.Active(requestSnapshot) != current {
		t.Fatalf("stale stage consumed the active generation: %d", m.requests.Active(requestSnapshot))
	}
}

// spec: session-startup/an-intermediate-stage-cannot-complete-a-pending-mutation-reload
func TestInventoryStageDoesNotCompletePendingToggleReload(t *testing.T) {
	m := newTestModel()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("failed to start mutation operation")
	}
	m.pendingToggleOperation = operation
	generation := m.requests.Active(requestSnapshot)
	m.pendingToggleReload = generation
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageInventory, generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, stages: stages})
	m = updated.(Model)
	if m.operations.Active == nil || m.pendingToggleOperation != operation {
		t.Fatal("inventory stage released the pending mutation operation")
	}

	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, mergedCfg: &config.Config{}, rows: []row{mkRow("audit")}, driftAssessment: driftReady})
	m = updated.(Model)
	if m.operations.Active != nil || m.pendingToggleOperation != 0 {
		t.Fatalf("final stage did not finish the pending reload: active=%#v pending=%d", m.operations.Active, m.pendingToggleOperation)
	}
}

// spec: session-startup/provisional-rows-show-configured-selection-state
// spec: tui-startup/provisional-config-stage-publishes-verification-pending-rows
func TestProvisionalConfigStagePublishesPendingRows(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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

	updated, cmd := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: session.BuildProvisionalRows(provisional, nil, nil), stages: stages})
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
	if pending.Glyph() != "[x]" || pending.StatusText() != "enabled in config; verification pending" {
		t.Fatalf("provisional row did not show configuration separately from verification: glyph=%q status=%q", pending.Glyph(), pending.StatusText())
	}
	if badge := originBadge(pending); badge != "G" {
		t.Fatalf("provisional row lost its configured scope badge %q", badge)
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

// spec: session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
// spec: tui-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
// spec: session-startup/provisional-rows-retain-single-view-position-and-guarded-actions
func TestProvisionalStageDoesNotCheckExcludedMarketplacePin(t *testing.T) {
	m := newProvisionalStageModel()
	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{{
			URL: "https://market.example", Source: config.Organization,
			Resources: []config.MarketplaceResourcePin{
				{RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "audit", Direct: true},
				{RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "review", Direct: true},
			},
		}},
		MarketplaceExclusions: []config.MarketplaceExclusion{{URL: "https://market.example", Locator: "tools/skill/review", Source: config.UserPersonal}},
	}
	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: m.requests.Active(requestSnapshot), provisionalCfg: cfg, provisionalRows: session.BuildProvisionalRows(cfg, nil, nil)})
	m = updated.(Model)
	if len(m.rows) != 2 || m.rows[0].Artifact.Name != "audit" || m.rows[0].Glyph() != "[x]" || originBadge(m.rows[0]) != "G" {
		t.Fatalf("early enabled state should check only the effective configured pin: %#v", m.rows)
	}
	excluded := m.rows[1]
	if excluded.Artifact.Name != "review" || !excluded.PendingVerification || excluded.Glyph() != "[ ]" || excluded.StatusText() != "excluded; verification pending" || originBadge(excluded) != "G" {
		t.Fatalf("excluded organization pin should hold its unchecked Global position while pending: glyph=%q status=%q badge=%q row=%#v", excluded.Glyph(), excluded.StatusText(), originBadge(excluded), excluded)
	}
	mi, _ := m.applyRowSpaceEligibility(excluded)
	if mi.(Model).pane == paneToggleModal {
		t.Fatal("verification-pending excluded row opened the toggle modal before verification")
	}
}

func statusLayout(rows []row) []string {
	var layout []string
	for _, item := range buildStatusTree(rows, nil) {
		if item.Kind == flatSection {
			layout = append(layout, item.SectionPath)
			continue
		}
		layout = append(layout, formatFlatItem(item, 400, false))
	}
	return layout
}

// spec: session-startup/provisional-layout-matches-verified-inventory
// spec: session-startup/provisional-rows-retain-single-view-position-and-guarded-actions
func TestProvisionalLayoutMatchesVerifiedInventory(t *testing.T) {
	audit := config.MarketplaceResourcePin{RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "audit", Direct: true}
	review := config.MarketplaceResourcePin{RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "review", Direct: true}
	market := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market-id", Source: config.Organization, Resources: []config.MarketplaceResourcePin{audit, review}}
	cfg := &config.Config{
		Marketplaces:          []config.MarketplaceEntry{market},
		MarketplaceExclusions: []config.MarketplaceExclusion{{URL: "https://market.example", Locator: "tools/skill/review", Source: config.UserPersonal}},
		Registries: []config.RegistryEntry{{
			URL: "./plugins/local", Source: config.RepoCommitted, CatalogKey: "local-key",
			Collections: config.KindSelector{Include: []string{"bundle"}},
		}},
	}
	catalogs := map[string]catalog.LoadedCatalog{"local-key": {Entries: []catalog.Entry{{Kind: ir.KindCollection, Name: "bundle"}}}}
	resolved := []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Kind: ir.KindSkill, Name: "audit"}, RegistryURL: market.URL, Source: config.Organization,
		CatalogKey: marketplaceCatalogKey(market, audit), IsMarketplace: true, MarketplaceURL: market.URL,
		MarketplaceInstanceID: market.InstanceID, MarketplaceRegistryID: "tools", MarketplaceResourceName: "audit",
	}}

	verified := statusLayout(buildRows(cfg, catalogs, resolved, nil, materialize.DriftReport{}))
	provisional := statusLayout(session.BuildProvisionalRows(cfg, nil, nil))
	if !slices.Equal(provisional, verified) {
		t.Fatalf("verified inventory reshuffles the provisional layout:\nprovisional: %q\nverified:    %q", provisional, verified)
	}
	for _, want := range []string{
		"  [x] G   skill market.example/audit",
		"  [ ]     skill market.example/review",
		"  [ ]     collection local/bundle",
	} {
		if !slices.Contains(verified, want) {
			t.Fatalf("verified layout lacks rendered row %q: %q", want, verified)
		}
	}
}

// spec: session-startup/organization-marketplace-collection-expands-from-the-verified-cached-manifest
// spec: session-startup/cached-collection-leaves-prefer-the-exact-then-newest-verified-revision
// spec: session-startup/tampered-cached-manifests-are-rejected
func TestProvisionalStageExpandsOrganizationCollectionFromVerifiedCache(t *testing.T) {
	cache := marketplace.NewCache(t.TempDir())
	const url = "https://market.example"
	leafRevision := "sha256:" + strings.Repeat("a", 64)
	payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: "bundle", SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "skill", Name: "audit", RegistryID: "tools", Revision: leafRevision},
			{Kind: "mcpserver", Name: "hub", RegistryID: "tools", Revision: leafRevision},
			{Kind: "skill", Name: "review", RegistryID: "tools", Revision: leafRevision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := tuiAssessedResourceRevision(t, store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "tools", Kind: "collection", Name: "bundle"}, ContentDigest: store.RevisionDigest(digest)})
	if _, err := cache.WriteResourceRevision(url, "tools", "collection", "bundle", digest, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(digest, payload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(digest); err != nil {
		t.Fatal(err)
	}

	placeholder := "sha256:" + strings.Repeat("0", 64)
	collection := config.MarketplaceResourcePin{RegistryID: "tools", RegistrySlug: "tools", Kind: "collection", Name: "bundle", Revision: placeholder, Digest: placeholder, Direct: true}
	exclusions := []config.MarketplaceExclusion{{URL: url, Locator: "tools/skill/review", Source: config.UserPersonal}}
	preAcquisition := &config.Config{MarketplaceExclusions: exclusions, Marketplaces: []config.MarketplaceEntry{{URL: url, InstanceID: "bootstrap", Source: config.Organization, Resources: []config.MarketplaceResourcePin{collection}}}}

	selectedBy := config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "tools", Name: "bundle", Revision: digest}})
	leaf := func(kind, name string) config.MarketplaceResourcePin {
		return config.MarketplaceResourcePin{RegistryID: "tools", RegistrySlug: "tools", Kind: kind, Name: name, Revision: leafRevision, Digest: leafRevision, SelectedBy: selectedBy}
	}
	acquired := config.MarketplaceEntry{URL: url, InstanceID: "mp", Source: config.Organization, Resources: []config.MarketplaceResourcePin{leaf("skill", "audit"), leaf("mcpserver", "hub"), leaf("skill", "review")}}
	verifiedCfg := &config.Config{MarketplaceExclusions: exclusions, Marketplaces: []config.MarketplaceEntry{acquired}}
	var resolved []resolver.ResolvedArtifact
	for _, pin := range acquired.Resources[:2] {
		kind, _ := mpapi.ToIR(pin.Kind)
		resolved = append(resolved, resolver.ResolvedArtifact{
			Entry: catalog.Entry{Kind: kind, Name: pin.Name}, RegistryURL: url, Source: config.Organization,
			CatalogKey: marketplaceCatalogKey(acquired, pin), IsMarketplace: true, MarketplaceURL: url,
			MarketplaceInstanceID: acquired.InstanceID, MarketplaceRegistryID: "tools", MarketplaceResourceName: pin.Name,
		})
	}

	rows := session.BuildProvisionalRows(session.WithCachedCollectionLeaves(preAcquisition, cache), nil, nil)
	verified := statusLayout(buildRows(verifiedCfg, nil, resolved, nil, materialize.DriftReport{}))
	if provisional := statusLayout(rows); !slices.Equal(provisional, verified) {
		t.Fatalf("cached collection members do not hold their verified positions:\nprovisional: %q\nverified:    %q", provisional, verified)
	}
	glyphs := map[string]string{}
	for _, r := range rows {
		glyphs[r.Artifact.Name] = r.Glyph()
	}
	if glyphs["audit"] != "[x]" || glyphs["hub"] != "[x]" || glyphs["review"] != "[ ]" {
		t.Fatalf("pending member glyphs = %v, want audit/hub checked and excluded review unchecked", glyphs)
	}
	if len(preAcquisition.Marketplaces[0].Resources) != 1 {
		t.Fatal("cache expansion mutated the validated configuration")
	}

	// The placeholder revision above resolved through the newest verified
	// manifest; an exact verified revision resolves to its own manifest.
	leaves, ok := cache.CachedCollectionLeaves(url, "tools", "bundle", digest)
	exactNames := map[string]bool{}
	for _, leaf := range leaves {
		exactNames[leaf.Name] = true
	}
	if !ok || len(leaves) != 3 || !exactNames["audit"] || !exactNames["hub"] || !exactNames["review"] {
		t.Fatalf("exact cached manifest leaves = %v ok=%t, want the 3 seeded members", leaves, ok)
	}

	// A newer verified manifest wins for uncached revisions, and a manifest
	// whose bytes no longer match its recorded digest is skipped.
	newerPayload, newerDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: "bundle", SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "skill", Name: "delta", RegistryID: "tools", Revision: leafRevision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	newerRevision := tuiAssessedResourceRevision(t, store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "tools", Kind: "collection", Name: "bundle"}, ContentDigest: store.RevisionDigest(newerDigest)})
	newerPath, err := cache.WriteResourceRevision(url, "tools", "collection", "bundle", newerDigest, newerRevision)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(newerPath, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(newerDigest, newerPayload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(newerDigest); err != nil {
		t.Fatal(err)
	}
	leaves, ok = cache.CachedCollectionLeaves(url, "tools", "bundle", placeholder)
	if !ok || len(leaves) != 1 || leaves[0].Name != "delta" {
		t.Fatalf("uncached revision leaves = %v ok=%t, want newest verified [delta]", leaves, ok)
	}
	raw, err := os.ReadFile(cache.CollectionManifestPath(newerDigest))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.CollectionManifestPath(newerDigest), []byte(strings.Replace(string(raw), "delta", "omega", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	leaves, ok = cache.CachedCollectionLeaves(url, "tools", "bundle", newerDigest)
	if !ok || len(leaves) != 3 {
		t.Fatalf("tampered newest manifest was not skipped: leaves=%v ok=%t", leaves, ok)
	}

	// A collection whose only manifest is tampered reports no leaves at all.
	soloPayload, soloDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: "solo", SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
		Members: []hccartifact.CollectionMemberEntry{
			{Kind: "skill", Name: "lone", RegistryID: "tools", Revision: leafRevision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	soloRevision := tuiAssessedResourceRevision(t, store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "tools", Kind: "collection", Name: "solo"}, ContentDigest: store.RevisionDigest(soloDigest)})
	if _, err := cache.WriteResourceRevision(url, "tools", "collection", "solo", soloDigest, soloRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteCollectionManifest(soloDigest, soloPayload); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkCollectionVerified(soloDigest); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(cache.CollectionManifestPath(soloDigest))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.CollectionManifestPath(soloDigest), []byte(strings.Replace(string(raw), "lone", "omega", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if leaves, ok := cache.CachedCollectionLeaves(url, "tools", "solo", soloDigest); ok {
		t.Fatalf("tampered manifest produced leaves %v", leaves)
	}
}

// spec: session-startup/optional-collection-member-yields-to-a-configured-selection-of-the-same-resource
func TestProvisionalCollectionMemberYieldsToConfiguredSelectionOfSameName(t *testing.T) {
	member := config.MarketplaceResourcePin{
		RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "audit", Revision: "sha256:" + strings.Repeat("a", 64),
		SelectedBy: config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "tools", Name: "bundle", Revision: "sha256:" + strings.Repeat("b", 64)}}),
	}
	cfg := &config.Config{
		Registries:   []config.RegistryEntry{{URL: "https://git.example/resources.git", Source: config.UserPersonal, CatalogKey: "resources", Skills: config.KindSelector{Include: []string{"audit"}}}},
		Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", Source: config.Organization, Resources: []config.MarketplaceResourcePin{member}}},
	}
	rows := session.BuildProvisionalRows(cfg, nil, nil)
	if len(rows) != 1 || rows[0].Artifact.IsMarketplace {
		t.Fatalf("collection member should yield to the configured selection that verified inventory keeps: %#v", rows)
	}
}

// spec: session-startup/registry-selections-carry-the-harness-override-badge
// spec: session-startup/provisional-rows-retain-single-view-position-and-guarded-actions
func TestProvisionalRowsCarryRegistryOverrideHarnessBadge(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: "https://git.example/skills.git", Source: config.RepoCommitted, CatalogKey: "skills", Harnesses: []string{"codex"},
		Skills: config.KindSelector{Include: []string{"audit"}},
	}}}
	rows := session.BuildProvisionalRows(cfg, nil, nil, map[config.Source][]string{config.RepoCommitted: {"claude"}})
	if len(rows) != 1 || harnessBadge(rows[0]) != "{Codex}" {
		t.Fatalf("pending row lacks the harness badge verified inventory renders: %#v", rows)
	}
	rendered := formatFlatItem(flatItem{Kind: flatArtifact, Row: rows[0], Depth: 1}, 400, false)
	if !strings.Contains(rendered, "skill skills/audit") || !strings.HasSuffix(rendered, " {Codex}") {
		t.Fatalf("pending row must render qualified label and harness badge, got %q", rendered)
	}
}

// spec: session-startup/a-stale-provisional-stage-cannot-publish-rows
func TestStaleProvisionalStageCannotPublishRows(t *testing.T) {
	m := newTestModel()
	stale := m.requests.Active(requestSnapshot)
	m.beginRequest(requestSnapshot)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:       "https://market.example",
		Resources: []config.MarketplaceResourcePin{{RegistryID: "tools", Kind: "skill", Name: "remote"}},
	}}}

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: stale, provisionalCfg: provisional, provisionalRows: session.BuildProvisionalRows(provisional, nil, nil)})
	m = updated.(Model)
	for _, r := range m.rows {
		if r.PendingVerification {
			t.Fatalf("stale provisional stage published row %#v", r)
		}
	}
}

// spec: session-startup/provisional-config-publishes-configured-pins-before-acquisition
// spec: session-startup/provisional-rows-render-while-transient-acquisition-is-blocked
func TestProvisionalConfiguredRowsRenderBeforeBlockedAcquisition(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)
	unblock, acquisitionHeld := blockedOrganizationMarketplace(t)

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
	m = applyBrowseSafeStageWhileBlocked(t, m, stages, acquisitionHeld)
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
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "remote") {
		t.Fatalf("rendered view lacks the pending configured row:\n%s", view)
	}
	if !strings.Contains(view, "verifying resources...") || strings.Contains(view, "loading resources...") {
		t.Fatalf("rendered view lacks verification progress while acquisition is blocked:\n%s", view)
	}
	if m.rows[pendingRow].Glyph() != "[x]" || originBadge(m.rows[pendingRow]) != "G" {
		t.Fatalf("configured remote selection should show an early enabled check and scope badge: glyph=%q badge=%q", m.rows[pendingRow].Glyph(), originBadge(m.rows[pendingRow]))
	}
	if status := m.rows[pendingRow].StatusText(); !strings.Contains(status, "verification pending") || !strings.Contains(status, "enabled in config") {
		t.Fatalf("early checked row does not distinguish configuration from installation: %q", status)
	}
	remoteLine := ""
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "remote") {
			remoteLine = l
		}
	}
	if !strings.Contains(remoteLine, "[x]") || !strings.Contains(remoteLine, " G ") {
		t.Fatalf("configured selection does not render an early checked badge and scope: %q", remoteLine)
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

// blockedOrganizationMarketplace configures an organization Marketplace with no
// deployed lock, so startup must acquire it transiently, and holds every
// /api/v1/ request until unblock is called. Only transient acquisition issues
// such requests in these fixtures (the browse-safe stage fetches the
// well-known document, and tests never run discovery commands), so
// acquisitionHeld is closed once acquisition has started and is parked.
func blockedOrganizationMarketplace(t *testing.T) (unblock func(), acquisitionHeld <-chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock = func() { releaseOnce.Do(func() { close(release) }) }
	held := make(chan struct{})
	var heldOnce sync.Once
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			heldOnce.Do(func() { close(held) })
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
	return unblock, held
}

// stagesBeforeBlockedAcquisition waits until transient acquisition is held by
// the fixture and returns every stage delivered before it, failing unless each
// one is a browse-safe config stage. Stages are sent synchronously on a
// buffered channel and acquisition starts only after the browse-safe callback
// returns, so once acquisitionHeld is closed every pre-acquisition stage is
// already buffered and the producer cannot send another until unblock. The
// result therefore does not depend on scheduling: an inventory, final, or
// authoritative (pinned) config stage, or a closed stream, always fails.
func stagesBeforeBlockedAcquisition(t *testing.T, stages <-chan snapshotLoadedMsg, acquisitionHeld <-chan struct{}) []snapshotLoadedMsg {
	t.Helper()
	var browseSafe []snapshotLoadedMsg
	accept := func(msg snapshotLoadedMsg, ok bool) {
		t.Helper()
		if !ok || !isBrowseSafeConfigStage(msg) {
			t.Fatalf("resolve advanced past acquisition while blocked: %+v closed=%t", msg, !ok)
		}
		browseSafe = append(browseSafe, msg)
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for held := false; !held; {
		select {
		case msg, ok := <-stages:
			accept(msg, ok)
		case <-acquisitionHeld:
			held = true
		case <-timer.C:
			t.Fatal("transient organization Marketplace acquisition never reached the blocked fixture")
		}
	}
	for {
		select {
		case msg, ok := <-stages:
			accept(msg, ok)
		default:
			return browseSafe
		}
	}
}

// isBrowseSafeConfigStage reports whether msg is the configuration published
// for browsing while acquisition is pending: a config stage with a merged
// configuration, no provisional selections or rows, and no Marketplace pins.
func isBrowseSafeConfigStage(msg snapshotLoadedMsg) bool {
	if msg.stage != snapshotStageConfig || msg.mergedCfg == nil || msg.provisionalCfg != nil || len(msg.provisionalRows) != 0 || len(msg.rows) != 0 {
		return false
	}
	for _, entry := range msg.mergedCfg.Marketplaces {
		if len(entry.Resources) != 0 {
			return false
		}
	}
	return true
}

// applyBrowseSafeStageWhileBlocked proves that resolution has published
// exactly one browse-safe config stage and nothing later while acquisition is
// held, and applies that stage as the running TUI would, so the assertions
// that follow observe the view shown while acquisition is still blocked.
func applyBrowseSafeStageWhileBlocked(t *testing.T, m Model, stages <-chan snapshotLoadedMsg, acquisitionHeld <-chan struct{}) Model {
	t.Helper()
	browseSafe := stagesBeforeBlockedAcquisition(t, stages, acquisitionHeld)
	if len(browseSafe) != 1 {
		t.Fatalf("resolution published %d browse-safe config stages before blocked acquisition, want 1: %+v", len(browseSafe), browseSafe)
	}
	updated, _ := m.Update(browseSafe[0])
	return updated.(Model)
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

// spec: session-startup/provisional-rows-render-while-transient-acquisition-is-blocked
func TestProvisionalLocalAllTrueRowsRenderBeforeBlockedAcquisition(t *testing.T) {
	repoRoot, homeDir, _, _ := provisionalLocalAllTrueFixture(t)
	unblock, acquisitionHeld := blockedOrganizationMarketplace(t)

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
	m = applyBrowseSafeStageWhileBlocked(t, m, stages, acquisitionHeld)
	want := map[string]ir.ResourceKind{
		"alpha":  ir.KindSkill,
		"beta":   ir.KindSkill,
		"sigma":  ir.KindSkill,
		"deploy": ir.KindCommand,
		"remote": ir.KindSkill,
	}
	badges := map[string]string{"alpha": "G", "beta": "G", "sigma": "P", "deploy": "P", "remote": "G"}
	for _, r := range m.rows {
		if !r.PendingVerification {
			t.Fatalf("pre-verification row %#v is not marked pending", r.Artifact)
		}
		if r.Glyph() != "[x]" || originBadge(r) != badges[r.Artifact.Name] {
			t.Fatalf("configured %q has glyph %q and scope %q, want checked %q", r.Artifact.Name, r.Glyph(), originBadge(r), badges[r.Artifact.Name])
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
}

// spec: session-startup/user-scope-local-all-true-selections-expand-into-pending-rows
// spec: tui-startup/local-all-true-selections-expand-into-pending-rows-on-the-normal-config-path
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

// spec: tui-startup/registry-scoped-local-excludes-apply-to-provisional-rows
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
	unblock, _ := blockedOrganizationMarketplace(t)

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

// spec: session-startup/authoritative-failure-clears-provisional-rows
func TestAuthoritativeFailureClearsProvisionalRows(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
	stages := make(chan snapshotLoadedMsg)
	close(stages)
	provisional := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "tools", Kind: "skill", Name: "remote",
			Revision: "sha256:" + strings.Repeat("a", 64), Direct: true,
		}},
	}}}

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: session.BuildProvisionalRows(provisional, nil, nil), stages: stages})
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

// spec: session-startup/an-open-detail-follows-its-verified-row
func TestPendingDetailFollowsVerifiedRow(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	generation := m.requests.Active(requestSnapshot)
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

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, provisionalCfg: provisional, provisionalRows: session.BuildProvisionalRows(provisional, nil, nil), stages: stages})
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

// spec: session-startup/final-stage-merges-discovery-published-during-the-config-stage
func TestFinalStageMergesDiscoveryPublishedDuringConfigStage(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"}
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, stages: stages})
	m = updated.(Model)
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.Active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
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

// spec: session-startup/mutation-actions-remain-gated-until-verified-inventory
// spec: tui-startup/mutations-stay-gated-until-verified-inventory
func TestSyncKeyBlockedBeforeVerifiedInventory(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
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

// spec: session-startup/mutation-actions-remain-gated-until-verified-inventory
// spec: tui-startup/mutations-stay-gated-until-verified-inventory
func TestDiscoveryRowCannotInstallBeforeVerifiedInventory(t *testing.T) {
	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	generation := m.requests.Active(requestSnapshot)
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "market", Alias: "public"}
	stages := make(chan snapshotLoadedMsg)
	close(stages)

	updated, _ := m.Update(snapshotLoadedMsg{stage: snapshotStageConfig, generation: generation, mergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, stages: stages})
	m = updated.(Model)
	item := mpapi.ResourceListItem{RegistryID: "registry-id", RegistrySlug: "tools", Kind: "skill", Name: "remote", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Availability: mpapi.AvailabilityAvailable}
	outcome := marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, CacheState: marketplace.DiscoveryCacheFresh, ObservedAt: time.Now()}
	updated, _ = m.Update(discoveryLoadedMsg{generation: m.requests.Active(requestDiscovery), sequence: 1, outcomes: []marketplace.DiscoveryOutcome{outcome}, done: true})
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
