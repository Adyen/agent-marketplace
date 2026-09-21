package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

type MarketplaceLayerMutation struct {
	editor    *config.LayerEditor
	legacy    bool
	endpoint  config.MarketplaceEntry
	endpointS bool
	pins      []config.MarketplaceResourcePin
	pinsS     bool
	removal   *marketplaceRemoval
}

type marketplaceRemoval struct {
	entry      config.MarketplaceEntry
	pin        config.MarketplaceResourcePin
	collection bool
}

func (m *MarketplaceLayerMutation) EnsureEndpoint(entry config.MarketplaceEntry) error {
	if m == nil {
		return errors.New("marketplace layer mutation is nil")
	}
	if m.legacy {
		m.endpoint = entry
		m.endpointS = true
		return nil
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
	if m.legacy {
		m.endpoint = entry
		m.endpointS = true
		m.pins = append(m.pins, pins...)
		m.pinsS = true
		return nil
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
	return m.removeMarketplacePin(entry, pin, false)
}

func (m *MarketplaceLayerMutation) RemoveCollection(entry config.MarketplaceEntry, collection config.MarketplaceResourcePin) error {
	return m.removeMarketplacePin(entry, collection, true)
}

func (m *MarketplaceLayerMutation) removeMarketplacePin(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, collection bool) error {
	if m == nil {
		return errors.New("marketplace layer mutation is nil")
	}
	if m.legacy {
		if m.removal != nil {
			return errors.New("marketplace layer mutation supports one legacy removal")
		}
		m.removal = &marketplaceRemoval{entry: entry, pin: pin, collection: collection}
		return nil
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
	return nil
}

func MutateMarketplaceLayer(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, mutate func(*MarketplaceLayerMutation) error, sync func() ([]error, error)) ([]error, error) {
	if mutate == nil {
		return nil, errors.New("marketplace layer mutation callback is required")
	}
	info, err := os.Lstat(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return mutateV4MarketplaceLayer(ctx, repoRoot, localConfigDir, configPath, source, mutate, sync)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config %s is a symlink to a missing target: %w", configPath, err)
		}
	}
	v4, err := config.LoadV4ConfigIfPresent(configPath, source)
	if err != nil {
		return nil, err
	}
	if v4 == nil {
		return mutateLegacyMarketplaceLayer(configPath, source, mutate, sync)
	}
	return mutateV4MarketplaceLayer(ctx, repoRoot, localConfigDir, configPath, source, mutate, sync)
}

func commitMarketplaceConfigRemoval(configPath string, mutate func() (*config.FileSnapshot, config.ConfigFileCommit, error), sync func() ([]error, error)) (warnings []error, retErr error) {
	snapshot, commit, err := mutate()
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := snapshot.RestoreIfCurrent(commit); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace config: %w", rollbackErr))
		}
	}()
	if sync != nil {
		warnings, err = sync()
		if err != nil {
			return nil, err
		}
	}
	current, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read Marketplace config after remove: %w", err)
	}
	if !bytes.Equal(current, commit.Bytes) {
		return nil, fmt.Errorf("marketplace config changed concurrently during remove")
	}
	return warnings, nil
}

func mutateLegacyMarketplaceLayer(configPath string, source config.Source, mutate func(*MarketplaceLayerMutation) error, sync func() ([]error, error)) ([]error, error) {
	m := &MarketplaceLayerMutation{legacy: true}
	if err := mutate(m); err != nil {
		return nil, err
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	warnings, err := runLegacyMarketplaceMutation(configPath, m, sync)
	if err != nil {
		return nil, err
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(before, after) {
		warnLegacyLayerMutation(source, configPath)
	}
	return warnings, nil
}

func runLegacyMarketplaceMutation(configPath string, m *MarketplaceLayerMutation, sync func() ([]error, error)) ([]error, error) {
	if r := m.removal; r != nil {
		return commitMarketplaceConfigRemoval(configPath, func() (*config.FileSnapshot, config.ConfigFileCommit, error) {
			if r.collection {
				return config.RemoveMarketplaceCollectionWithSnapshotAndCommit(configPath, r.entry.URL, r.entry.InstanceID, r.pin)
			}
			return config.RemoveMarketplaceDirectPinWithSnapshotAndCommit(configPath, r.entry.URL, r.entry.InstanceID, r.pin)
		}, sync)
	}
	if m.pinsS {
		if sync == nil {
			sync = func() ([]error, error) { return nil, nil }
		}
		return ensureMarketplacePinsAndSync(configPath, m.endpoint, m.pins, sync)
	}
	if m.endpointS {
		if err := config.EnsureMarketplaceEntryInConfig(configPath, m.endpoint); err != nil {
			return nil, err
		}
	}
	if sync == nil {
		return nil, nil
	}
	return sync()
}

func mutateV4MarketplaceLayer(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, mutate func(*MarketplaceLayerMutation) error, sync func() ([]error, error)) (warnings []error, retErr error) {
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
	_, lockGeneration, lockMode, lockDoc, err := readMarketplaceLayerLock(lockPath, scope)
	if err != nil {
		return nil, err
	}
	if lockDoc.Scope == "" {
		lockDoc.Scope = scope
	}
	_, configExpectedMode, err := dependencytxn.ReadGenerationAndMode(configEdit.Path)
	if err != nil {
		return nil, err
	}
	configSnapshot, err := config.SnapshotConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	lockSnapshot, err := config.SnapshotConfigFile(lockPath)
	if err != nil {
		return nil, err
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
	finalEC := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localConfigDir, FileOverlays: overlays}, nil)
	if len(finalEC.Errors) > 0 {
		return nil, errors.Join(finalEC.Errors...)
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
	configCommit := config.NewConfigFileCommit(configEdit.Bytes)
	lockCommit := config.NewConfigFileCommit(lockData)
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := configSnapshot.RestoreIfCurrent(configCommit); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace config: %w", rollbackErr))
		}
		if rollbackErr := lockSnapshot.RestoreIfCurrent(lockCommit); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace lock: %w", rollbackErr))
		}
	}()
	warnings, err = sync()
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read Marketplace config after sync: %w", err)
	}
	if !bytes.Equal(current, configEdit.Bytes) {
		return nil, fmt.Errorf("marketplace config changed concurrently during install")
	}
	return warnings, nil
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
			if read.RuntimeLock {
				assertion := dependencytxn.ReadAssertion{Path: read.Path, RequestedPath: read.RequestedPath}
				if previous, exists := byAlias[read.RequestedPath]; exists && previous != assertion {
					return nil, fmt.Errorf("%w: config alias changed at %s", dependencytxn.ErrGenerationConflict, read.RequestedPath)
				}
				byAlias[read.RequestedPath] = assertion
				continue
			}
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

func ToggleMarketplacePin(ctx context.Context, repoRoot, localConfigDir, configPath string, source config.Source, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, enable bool) error {
	ws, err := NewWorkspace(repoRoot, localConfigDir)
	if err != nil {
		return err
	}
	_, err = MutateMarketplaceLayer(ctx, repoRoot, localConfigDir, configPath, source, func(m *MarketplaceLayerMutation) error {
		if enable {
			pin := pin
			pin.Direct = true
			pin.SelectedBy = ""
			return m.IncludePins(entry, []config.MarketplaceResourcePin{pin})
		}
		return m.RemoveDirectPin(entry, pin)
	}, func() ([]error, error) {
		return nil, silentSync(ctx, ws, withMarketplaceMode(resolver.MarketplaceResolveModeCachedNoFetch))
	})
	return err
}
