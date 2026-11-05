package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

type MarketplaceLayerMutation struct {
	editor      *config.LayerEditor
	endpoint    config.MarketplaceEntry
	endpointS   bool
	pins        []config.MarketplaceResourcePin
	pinsS       bool
	removesPins bool
}

func (m *MarketplaceLayerMutation) EnsureEndpoint(entry config.MarketplaceEntry) error {
	if m == nil {
		return errors.New("marketplace layer mutation is nil")
	}
	if m.editor == nil {
		return errors.New("marketplace layer mutation has no open editor")
	}
	if _, err := m.editor.EnsureMarketplaceSource(entry.URL); err != nil {
		return err
	}
	m.endpoint = entry
	m.endpointS = true
	return nil
}

func (m *MarketplaceLayerMutation) IncludePins(entry config.MarketplaceEntry, pins []config.MarketplaceResourcePin) error {
	if m == nil {
		return errors.New("marketplace layer mutation is nil")
	}
	if len(pins) == 0 {
		return errors.New("marketplace install requires at least one pin")
	}
	if m.editor == nil {
		return errors.New("marketplace layer mutation has no open editor")
	}
	binding, err := m.editor.EnsureMarketplaceSource(entry.URL)
	if err != nil {
		return err
	}
	if err := m.editor.IncludeMarketplacePins(binding, pins); err != nil {
		return err
	}
	m.endpoint = entry
	m.endpointS = true
	m.pins = append(m.pins, pins...)
	m.pinsS = true
	return nil
}

func (m *MarketplaceLayerMutation) RemoveDirectPin(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) error {
	return m.removeMarketplacePin(entry, pin)
}

// RemoveCollection drops the collection's exact pin and records no
// collection/<name> exclusion, unlike Git-source collection removal
// (remove.go, SetLayerExclude). Marketplace sources support only explicit
// include requests at runtime (marketplaceEntryForLock refuses all, tags, and
// registries with v4.marketplace-runtime-unsupported), so no dynamic selection
// can re-select the removed collection.
func (m *MarketplaceLayerMutation) RemoveCollection(entry config.MarketplaceEntry, collection config.MarketplaceResourcePin) error {
	return m.removeMarketplacePin(entry, collection)
}

func (m *MarketplaceLayerMutation) removeMarketplacePin(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) error {
	if m == nil {
		return errors.New("marketplace layer mutation is nil")
	}
	if m.editor == nil {
		return errors.New("marketplace layer mutation has no open editor")
	}
	binding, found, err := m.editor.LookupMarketplaceSource(entry.URL)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("marketplace endpoint %q has no v4 source in this layer", entry.URL)
	}
	if err := m.editor.RemoveMarketplacePins(binding, []config.MarketplaceResourcePin{pin}); err != nil {
		return err
	}
	m.endpoint = entry
	m.endpointS = true
	m.removesPins = true
	return nil
}

func MutateMarketplaceLayer(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, mutate func(*MarketplaceLayerMutation) error, sync func() ([]error, error)) ([]error, error) {
	if mutate == nil {
		return nil, errors.New("marketplace layer mutation callback is required")
	}
	if err := RecoverDependencyTransactions(ctx); err != nil {
		return nil, err
	}
	if err := ValidateConsumerIntent(ctx, repoRoot, localConfigDir); err != nil {
		return nil, err
	}
	if _, err := config.LoadV4ConfigIfPresent(configPath, source); err != nil {
		return nil, err
	}
	return mutateV4MarketplaceLayer(ctx, repoRoot, localConfigDir, configPath, source, mutate, sync)
}

func mutateV4MarketplaceLayer(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, mutate func(*MarketplaceLayerMutation) error, sync func() ([]error, error)) (warnings []error, retErr error) {
	// Rollback validation must reuse this invocation's organization evidence,
	// never start fresh acquisition after the install has already failed.
	ctx = WithTransientOrganizationMarketplaceLockCache(ctx)
	staging, err := newConfigStagingManager()
	if err != nil {
		return nil, err
	}
	defer closeConfigStaging(staging, "marketplace layer mutation", &retErr)
	staged, err := staging.Path(configPath)
	if err != nil {
		return nil, err
	}
	target := config.LayerTarget{Source: source, StagedPath: staged, DeclaringPath: configPath}
	if err := seedMissingLayerRoot(target); err != nil {
		return nil, err
	}
	editor, err := config.OpenLayerEditor(target)
	if err != nil {
		return nil, err
	}
	m := &MarketplaceLayerMutation{editor: editor}
	if err := mutate(m); err != nil {
		return nil, err
	}
	candidate, err := editor.Render()
	if err != nil {
		return nil, err
	}
	if !candidate.Changed && !m.endpointS && !m.pinsS {
		if sync == nil {
			return nil, nil
		}
		return sync()
	}
	candidateV4 := editor.RenderedV4()
	if candidateV4 == nil {
		return nil, errors.New("marketplace v4 candidate is unavailable")
	}
	if err := os.WriteFile(staged, candidate.Bytes, candidate.Mode); err != nil {
		return nil, fmt.Errorf("write staged config %s: %w", staged, err)
	}
	edits, err := staging.Edits()
	if err != nil {
		return nil, err
	}
	if len(edits) != 1 {
		return nil, fmt.Errorf("staged config for %q is missing", configPath)
	}
	configEdit := edits[0]

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return nil, err
	}
	lockPath, err := marketplaceLayerLockPath(configPath)
	if err != nil {
		return nil, err
	}
	scope := lockfile.Scope(source.LockScope())
	lockBefore, lockGeneration, lockMode, lockDoc, err := readMarketplaceLayerLock(lockPath, scope)
	if err != nil {
		return nil, err
	}
	if lockDoc.Scope == "" {
		lockDoc.Scope = scope
	}
	configBefore, configGeneration, configExpectedMode, err := dependencytxn.ReadSnapshot(configEdit.Path)
	if err != nil {
		return nil, err
	}
	if configGeneration != configEdit.Expected {
		return nil, fmt.Errorf("%w: Marketplace config changed while staging %s", dependencytxn.ErrGenerationConflict, configEdit.Path)
	}

	mergeEntry := m.endpoint
	mergeEntry.Source = source
	exact, err := marketplacelock.MergePinsToV4(candidateV4, lockDoc, mergeEntry, m.pins, marketplace.DefaultCache())
	if err != nil {
		return nil, err
	}
	lockData, err := lockfile.Encode(exact)
	if err != nil {
		return nil, err
	}
	overlays := map[string][]byte{configEdit.Path: configEdit.Bytes, lockPath: lockData}
	if absolute, err := filepath.Abs(configPath); err == nil {
		overlays[filepath.Clean(absolute)] = configEdit.Bytes
	}
	ws, err := workspaceForRoots(repoRoot, localConfigDir)
	if err != nil {
		return nil, err
	}
	finalEC := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(), FileOverlays: overlays}, nil)
	if len(finalEC.Errors) > 0 {
		return nil, errors.Join(finalEC.Errors...)
	}
	if m.removesPins {
		if err := preflightHookRemoval(ctx, ws, edits, overlays); err != nil {
			return nil, err
		}
	}
	reads, err := marketplaceLayerReadAssertions(finalEC, configEdit, lockPath, lockData, lockGeneration)
	if err != nil {
		return nil, err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return nil, err
	}
	if err := dependencytxn.Recover(ctx, stateDir); err != nil {
		return nil, err
	}
	lockFileMode := lockMode
	if !lockGeneration.Exists {
		lockFileMode = 0o644
	}
	desired := []dependencytxn.DesiredFile{
		{Path: configEdit.Path, Expected: configEdit.Expected, ExpectedMode: configExpectedMode, Bytes: configEdit.Bytes, Mode: configEdit.Mode},
		{Path: lockPath, Expected: lockGeneration, ExpectedMode: lockMode, Bytes: lockData, Mode: lockFileMode},
	}
	if _, err := dependencytxn.Commit(ctx, stateDir, desired, dependencytxn.WithReadAssertions(reads)); err != nil {
		return nil, fmt.Errorf("commit Marketplace config and lock: %w", err)
	}
	if sync == nil {
		return nil, nil
	}
	defer func() {
		if retErr == nil {
			return
		}
		// A cancelled install still needs cleanup, but a competing writer must
		// not keep this deferred transaction waiting indefinitely.
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if rollbackErr := rollbackMarketplaceLayer(rollbackCtx, stateDir, ws, source, desired, configBefore, lockBefore, reads, retErr); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace config and lock: %w", rollbackErr))
		}
	}()
	warnings, err = sync()
	if err != nil {
		return nil, err
	}
	current, mode, err := dependencytxn.ReadGenerationAndMode(configEdit.Path)
	if err != nil {
		return nil, fmt.Errorf("read Marketplace config after sync: %w", err)
	}
	if current != (dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(configEdit.Bytes)}) || mode != configEdit.Mode {
		return nil, fmt.Errorf("%w: Marketplace config changed during install", dependencytxn.ErrGenerationConflict)
	}
	return warnings, nil
}

// Roll back the config and its Marketplace evidence together. Git reconciliation
// is durable independently of the install: retain its exact records, but never
// infer ownership of an arbitrary replacement lock from its cache availability.
func rollbackMarketplaceLayer(ctx context.Context, stateDir string, ws Workspace, source config.Source, published []dependencytxn.DesiredFile, configBefore, lockBefore []byte, reads []dependencytxn.ReadAssertion, cause error) error {
	configFile, lockFile := published[0], published[1]
	current, generation, mode, err := dependencytxn.ReadSnapshot(lockFile.Path)
	if err != nil {
		return fmt.Errorf("%w: inspect Marketplace lock during rollback: %w", dependencytxn.ErrGenerationConflict, err)
	}
	if !generation.Exists || mode != lockFile.Mode {
		return fmt.Errorf("%w: Marketplace lock disappeared or changed mode at %s", dependencytxn.ErrGenerationConflict, lockFile.Path)
	}
	configPublished := dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(configFile.Bytes)}
	// Keep every read/alias assertion; only our published destinations have
	// advanced. Check before validation as well as inside the rollback CAS.
	rollbackReads := append([]dependencytxn.ReadAssertion(nil), reads...)
	for i := range rollbackReads {
		switch rollbackReads[i].Path {
		case configFile.Path:
			rollbackReads[i].Expected = configPublished
		case lockFile.Path:
			rollbackReads[i].Expected = generation
		}
	}
	if err := dependencytxn.ValidateReadAssertions(rollbackReads); err != nil {
		return err
	}
	lockRestore := lockBefore
	if !bytes.Equal(current, lockFile.Bytes) {
		var committed *DependencyCommittedSyncError
		if !errors.As(cause, &committed) {
			return fmt.Errorf("%w: Marketplace lock changed outside dependency reconciliation at %s", dependencytxn.ErrGenerationConflict, lockFile.Path)
		}
		lockRestore, err = marketplaceRollbackReconciledLock(current, lockFile.Bytes, lockBefore)
		if err != nil {
			return err
		}
		// Validate the mixed candidate (original authored selection, retained
		// exact Git, original Marketplace evidence) before publishing any of it.
		ec := LoadEffective(ctx, ConfigLoadCached, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(), FileOverlays: map[string][]byte{configFile.Path: configBefore, lockFile.Path: lockRestore}}, nil)
		if len(ec.Errors) > 0 {
			return fmt.Errorf("%w: cannot validate Marketplace rollback candidate: %w", dependencytxn.ErrGenerationConflict, errors.Join(ec.Errors...))
		}
		var entries []config.RegistryEntry
		for _, layer := range ec.PresentLayers() {
			if layer.Source == source {
				entries = readOnlyRemoteEntries(layer)
			}
		}
		doc, err := lockfile.Decode(lockRestore)
		if err != nil {
			return err
		}
		if !readOnlyGitDocumentMatches(doc, entries) {
			return fmt.Errorf("%w: reconciled Git lock no longer matches Marketplace layer", dependencytxn.ErrGenerationConflict)
		}
	}
	restore := []dependencytxn.DesiredFile{
		{Path: configFile.Path, Expected: configPublished, ExpectedMode: configFile.Mode, Bytes: configBefore, Mode: configFile.ExpectedMode, Delete: !configFile.Expected.Exists},
		{Path: lockFile.Path, Expected: generation, ExpectedMode: mode, Bytes: lockRestore, Mode: lockFile.ExpectedMode, Delete: !lockFile.Expected.Exists && len(lockRestore) == 0},
	}
	if len(lockRestore) > 0 && !lockFile.Expected.Exists {
		restore[1].Mode = mode
	}
	_, err = dependencytxn.Commit(ctx, stateDir, restore, dependencytxn.WithReadAssertions(rollbackReads))
	return err
}

// A reconciler may add missing Git records, not change the Marketplace write
// set or retained exact records. Require the precise canonical bytes it writes;
// a comment, reformat, replacement pin, or changed retained record is foreign.
func marketplaceRollbackReconciledLock(current, published, before []byte) ([]byte, error) {
	conflict := func() ([]byte, error) {
		return nil, fmt.Errorf("%w: Marketplace lock changed after publication", dependencytxn.ErrGenerationConflict)
	}
	doc, err := lockfile.Decode(current)
	if err != nil {
		return conflict()
	}
	prepared, err := lockfile.Decode(published)
	if err != nil {
		return nil, err
	}
	canonical, err := lockfile.Encode(doc)
	if err != nil || !bytes.Equal(canonical, current) {
		return conflict()
	}
	probe := doc
	probe.Registries = prepared.Registries
	canonical, err = lockfile.Encode(probe)
	if err != nil || !bytes.Equal(canonical, published) || len(doc.Registries) <= len(prepared.Registries) {
		return conflict()
	}
	for _, retained := range prepared.Registries {
		found := false
		for _, registry := range doc.Registries {
			if reflect.DeepEqual(retained, registry) {
				found = true
				break
			}
		}
		if !found {
			return conflict()
		}
	}
	doc.Marketplaces = nil
	if len(before) > 0 {
		previous, err := lockfile.Decode(before)
		if err != nil {
			return nil, err
		}
		doc.Marketplaces = previous.Marketplaces
	}
	return lockfile.Encode(doc)
}

func marketplaceLayerLockPath(configPath string) (string, error) {
	resolve := config.ResolveConfigLockPaths
	if _, err := os.Lstat(configPath); errors.Is(err, os.ErrNotExist) {
		resolve = config.ResolveConfigLockPathsTolerateMissing
	}
	paths, err := resolve(configPath)
	if err != nil {
		return "", err
	}
	return dependencytxn.ValidateDestination(filepath.Clean(paths.CanonicalLock))
}

func readMarketplaceLayerLock(lockPath string, scope lockfile.Scope) ([]byte, dependencytxn.Generation, os.FileMode, lockfile.Document, error) {
	data, generation, mode, err := dependencytxn.ReadSnapshot(lockPath)
	if err != nil {
		return nil, generation, mode, lockfile.Document{}, err
	}
	if !generation.Exists {
		return nil, generation, mode, lockfile.Document{}, nil
	}
	doc, err := lockfile.Decode(data)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	if err != nil {
		return nil, generation, mode, lockfile.Document{}, fmt.Errorf("marketplace lock %q: %w", lockPath, err)
	}
	return data, generation, mode, doc, nil
}

func marketplaceLayerReadAssertions(ec config.EffectiveConfig, configEdit StagedConfigEdit, lockPath string, lockData []byte, lockGeneration dependencytxn.Generation) ([]dependencytxn.ReadAssertion, error) {
	byAlias := make(map[string]dependencytxn.ReadAssertion)
	byPath := make(map[string]string)
	lockDigest := dependencytxn.Digest(lockData)
	for _, layer := range ec.PresentLayers() {
		for _, read := range layer.Reads {
			if digest, exists := byPath[read.Path]; exists && digest != read.Digest {
				return nil, fmt.Errorf("%w: inconsistent config read at %s", dependencytxn.ErrGenerationConflict, read.Path)
			}
			byPath[read.Path] = read.Digest
			expected := dependencytxn.Generation{Exists: true, Digest: read.Digest}
			if read.Overlay {
				switch read.Path {
				case configEdit.Path:
					if dependencytxn.Digest(configEdit.Bytes) != read.Digest {
						return nil, fmt.Errorf("%w: unmatched config overlay at %s", dependencytxn.ErrGenerationConflict, read.Path)
					}
					expected = configEdit.Expected
				case lockPath:
					if lockDigest != read.Digest {
						return nil, fmt.Errorf("%w: unmatched lock overlay at %s", dependencytxn.ErrGenerationConflict, read.Path)
					}
					expected = lockGeneration
				default:
					continue
				}
			}
			assertion := dependencytxn.ReadAssertion{Path: read.Path, RequestedPath: read.RequestedPath, Expected: expected}
			if previous, exists := byAlias[read.RequestedPath]; exists && previous != assertion {
				return nil, fmt.Errorf("%w: config alias changed at %s", dependencytxn.ErrGenerationConflict, read.RequestedPath)
			}
			byAlias[read.RequestedPath] = assertion
		}
	}
	reads := make([]dependencytxn.ReadAssertion, 0, len(byAlias))
	for _, read := range byAlias {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].RequestedPath < reads[j].RequestedPath })
	return reads, nil
}

func ToggleMarketplacePin(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, enable bool) (SyncResult, error) {
	ws, err := NewWorkspace(repoRoot, localConfigDir)
	if err != nil {
		return SyncResult{}, err
	}
	var synced SyncResult
	_, err = MutateMarketplaceLayer(ctx, repoRoot, localConfigDir, configPath, source, func(m *MarketplaceLayerMutation) error {
		if enable {
			pin := pin
			pin.Direct = true
			pin.SelectedBy = ""
			return m.IncludePins(entry, []config.MarketplaceResourcePin{pin})
		}
		return m.RemoveDirectPin(entry, pin)
	}, func() ([]error, error) {
		var syncErr error
		synced, syncErr = silentSyncCollect(ctx, ws, withMarketplaceMode(resolver.MarketplaceResolveModeCachedNoFetch))
		return synced.Warnings, syncErr
	})
	return synced, err
}
