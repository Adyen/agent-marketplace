package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

var (
	CommitDependencyTransaction = dependencytxn.Commit
	LockSnapshotAcquiredHook    func()
)

type LockLifecycleError = pipeline.LockViewError

type LockMode struct {
	Locked                     bool
	Offline                    bool
	Upgrade                    bool
	Check                      bool
	Selects                    map[lockfile.Scope]bool
	ExplicitOrganization       bool
	UpgradeSelection           lockfile.UpgradeSelection
	UpgradeTargets             *UpgradeTargetSpec
	UpgradeResult              *UpgradeTargetResult
	IgnoreBlocked              bool
	ReconcileUnselected        bool
	StagedConfigEdits          []pipeline.StagedConfigEdit
	StagedLockEdits            []pipeline.StagedLockEdit
	CommitReads                []dependencytxn.ReadAssertion
	CommitScope                dependencytxn.CommitScope
	PrepareInstall             *PreparedInstallLocks
	ReuseInstall               *PreparedInstallLocks
	WarningSink                *[]error
	MarketplacePolicy          resolver.MarketplaceRevocationPolicy
	AllowTransientOrganization bool
}

type lockReconcileStats struct {
	retained int
	acquired int
}

func lockModeName(mode LockMode) string {
	switch {
	case mode.PrepareInstall != nil:
		return "install-prepare"
	case mode.ReuseInstall != nil:
		return "install-commit"
	case mode.Upgrade:
		return "upgrade"
	case mode.Locked:
		return "locked"
	case mode.Offline:
		return "offline"
	case mode.Check:
		return "check"
	default:
		return "sync"
	}
}

func selectedLockScopes(mode LockMode) []string {
	if mode.Selects == nil {
		return []string{"effective"}
	}
	scopes := make([]string, 0, len(mode.Selects))
	for _, scope := range []lockfile.Scope{lockfile.ScopeOrganization, lockfile.ScopeUser, lockfile.ScopeRepo, lockfile.ScopeLocal} {
		if mode.Selects[scope] {
			scopes = append(scopes, string(scope))
		}
	}
	return scopes
}

type UpgradeTargetSpec struct {
	Registry string
	Ref      string
	Path     string
	Kind     string
	Names    []string
}

type UpgradeTargetResult struct {
	Selection       lockfile.UpgradeSelection
	Notices         []string
	CheckUpgradeErr *LockLifecycleError
}

type exactLockSet struct {
	bySource   map[string]lockedGit
	byIdentity map[string][]lockedGit
	catalogs   map[string]map[string]*catalog.Catalog
}

type lockedGit struct {
	canonical string
	fetchURL  string
	ref       string
	commit    lockfile.GitOID
	snapshot  string
	unlocked  bool
}

func ParseLockSelection(value string, check bool) (map[lockfile.Scope]bool, bool, error) {
	selected := map[lockfile.Scope]bool{}
	switch value {
	case "", "all":
		selected[lockfile.ScopeUser] = true
		selected[lockfile.ScopeRepo] = true
		selected[lockfile.ScopeLocal] = true
		if check {
			selected[lockfile.ScopeOrganization] = true
		}
		return selected, false, nil
	case "organization":
		selected[lockfile.ScopeOrganization] = true
		return selected, true, nil
	default:
		scope, err := lockfile.ParseScope(value)
		if err != nil || scope == lockfile.ScopeOrganization {
			if err == nil {
				err = errors.New("organization must be selected exactly")
			}
			return nil, false, err
		}
		selected[scope] = true
		return selected, false, nil
	}
}

func resolveUpgradeTargets(ec config.EffectiveConfig, mode LockMode) (UpgradeTargetResult, error) {
	if !mode.Upgrade || mode.UpgradeTargets == nil {
		return UpgradeTargetResult{Selection: lockfile.UpgradeSelection{All: mode.Upgrade}}, nil
	}
	targets := mode.UpgradeTargets
	if targets.Registry == "" {
		return UpgradeTargetResult{Selection: lockfile.UpgradeSelection{All: true}}, nil
	}
	result := UpgradeTargetResult{}
	if targets.Registry != "" {
		canonical, err := CanonicalRegistryURL(targets.Registry)
		if err != nil {
			return result, err
		}
		type gitOccurrence struct {
			selector lockfile.GitUpgradeSelector
			root     lockfile.GitRoot
		}
		var occurrences []gitOccurrence
		for _, layer := range ec.PresentLayers() {
			scope := lockScopeForSource(layer.Source)
			if mode.Selects != nil && !mode.Selects[scope] {
				continue
			}
			current, err := loadExistingLayerDocument(layer, scope)
			if err != nil {
				return result, err
			}
			for _, entry := range pipeline.GitRegistryEntries(layer.Sources) {
				entryURL, err := CanonicalRegistryURL(entry.URL)
				if err != nil || entryURL != canonical || targets.Ref != "" && targets.Ref != entry.Ref {
					continue
				}
				rootPath := entry.Path
				if rootPath == "" {
					rootPath = "."
				}
				if targets.Path != "" && filepath.Clean(targets.Path) != rootPath {
					continue
				}
				if current == nil {
					return result, fmt.Errorf("git target %s requires an existing %s lock inventory", targets.Registry, scope)
				}
				locked, ok := findLockedGitRoot(*current, entryURL, entry.Ref, rootPath)
				if !ok {
					locked, ok = findLockedGitRootAnyRef(*current, entryURL, rootPath)
				}
				if !ok {
					return result, fmt.Errorf("git target %s@%s:%s is absent from the %s lock inventory", entryURL, entry.Ref, rootPath, scope)
				}
				occurrences = append(occurrences, gitOccurrence{selector: lockfile.GitUpgradeSelector{URL: entryURL, Ref: entry.Ref, Path: rootPath}, root: locked})
			}
		}
		if len(occurrences) == 0 {
			return result, fmt.Errorf("git target %s matched no configured registry; qualify it with --ref, --path, or --scope", targets.Registry)
		}
		if len(targets.Names) == 0 {
			for _, occurrence := range occurrences {
				selector := occurrence.selector
				selector.Path = ""
				result.Selection.Git = appendGitSelector(result.Selection.Git, selector)
			}
			if len(result.Selection.Git) > 1 {
				return result, fmt.Errorf("git target %s is ambiguous; qualify it with --ref, --path, or --scope", targets.Registry)
			}
			return result, nil
		}
		for _, name := range targets.Names {
			type gitCandidate struct {
				selector    lockfile.GitUpgradeSelector
				matchedPath string
			}
			var candidates []gitCandidate
			for _, occurrence := range occurrences {
				if !lockedRootHasResource(occurrence.root, targets.Kind, name) {
					continue
				}
				selector := occurrence.selector
				selector.Path = ""
				duplicate := false
				for _, candidate := range candidates {
					if candidate.selector == selector {
						duplicate = true
						break
					}
				}
				if !duplicate {
					candidates = append(candidates, gitCandidate{selector: selector, matchedPath: occurrence.root.Path})
				}
			}
			if len(candidates) == 0 {
				return result, fmt.Errorf("git target %s/%s does not exist in the selected locked root inventory", targets.Kind, name)
			}
			if len(candidates) > 1 {
				return result, fmt.Errorf("git target %s/%s is ambiguous; qualify it with --ref, --path, or --scope", targets.Kind, name)
			}
			candidate := candidates[0]
			result.Selection.Git = appendGitSelector(result.Selection.Git, candidate.selector)
			result.Notices = append(result.Notices, fmt.Sprintf("git target %s/%s expands to source occurrence %s@%s (all locked roots; matched path %s)", targets.Kind, name, candidate.selector.URL, candidate.selector.Ref, candidate.matchedPath))
		}
		return result, nil
	}

	return result, nil
}

func loadExistingLayerDocument(layer config.LayerResult, scope lockfile.Scope) (*lockfile.Document, error) {
	if len(pipeline.GitRegistryEntries(layer.Sources)) == 0 {
		return nil, nil
	}
	lockPath, err := lockfile.PathForConfig(layer.Path)
	if err != nil {
		name := "lock.yaml"
		if scope == lockfile.ScopeLocal {
			name = "lock.local.yaml"
		}
		lockPath = filepath.Join(filepath.Dir(layer.Path), name)
	}
	doc, _, _, err := pipeline.ReadLayerLock(lockPath, scope)
	return doc, err
}

func findLockedGitRoot(doc lockfile.Document, url, ref, rootPath string) (lockfile.GitRoot, bool) {
	for _, registry := range doc.Registries {
		if registry.URL != url || registry.Ref != ref {
			continue
		}
		for _, root := range registry.Roots {
			if root.Path == rootPath {
				return root, true
			}
		}
	}
	return lockfile.GitRoot{}, false
}

func findLockedGitRootAnyRef(doc lockfile.Document, url, rootPath string) (lockfile.GitRoot, bool) {
	var match lockfile.GitRoot
	found := 0
	for _, registry := range doc.Registries {
		if registry.URL != url {
			continue
		}
		for _, root := range registry.Roots {
			if root.Path != rootPath {
				continue
			}
			match = root
			found++
		}
	}
	return match, found == 1
}

func lockedRootHasResource(root lockfile.GitRoot, kind, name string) bool {
	for _, resource := range root.Resources {
		if resource.Kind == kind && resource.Name == name {
			return true
		}
	}
	return false
}

func appendGitSelector(selectors []lockfile.GitUpgradeSelector, incoming lockfile.GitUpgradeSelector) []lockfile.GitUpgradeSelector {
	for _, selector := range selectors {
		if selector == incoming {
			return selectors
		}
	}
	return append(selectors, incoming)
}

func DependencyStateDir() (string, error) {
	return dependencytxn.StateDir()
}

func (mode LockMode) ReadOnlyDependencies() bool {
	return mode.Check || mode.Locked || mode.Offline || mode.PrepareInstall != nil
}

func PrepareDependencyTransactions(ctx context.Context, readOnly bool) error {
	if !readOnly {
		return pipeline.RecoverDependencyTransactions(ctx)
	}
	stateDir, err := DependencyStateDir()
	if err != nil {
		return err
	}
	return dependencytxn.CheckPending(stateDir)
}

func rejectGeneratedLockSymlink(name string) (string, error) {
	return dependencytxn.ValidateDestination(filepath.Clean(name))
}

func canonicalConfigDependencyPath(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("resolve config dependency path %s: %w", name, err)
	}
	return dependencytxn.CanonicalConfigDestination(filepath.Clean(absolute))
}

func CanonicalizeFuturePath(name string) (string, error) {
	missing := []string{}
	current := filepath.Clean(name)
	for {
		canonical, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return canonical, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func validateStagedConfigPathIdentity(edit pipeline.StagedConfigEdit) (string, error) {
	requested, err := canonicalConfigDependencyPath(edit.RequestedPath)
	if err != nil {
		// A final config symlink can be retargeted after staging. Preserve the
		// generation-conflict contract for that race; ancestor symlinks remain
		// unsafe path errors because they can redirect the transaction layer.
		if info, lstatErr := os.Lstat(filepath.Clean(edit.RequestedPath)); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: staged config path identity changed at %s", dependencytxn.ErrGenerationConflict, edit.RequestedPath)
		}
		return "", fmt.Errorf("resolve requested staged config path %s: %w", edit.RequestedPath, err)
	}
	destination, err := canonicalConfigDependencyPath(edit.Path)
	if err != nil {
		return "", fmt.Errorf("resolve staged config destination %s: %w", edit.Path, err)
	}
	if requested != destination || destination != filepath.Clean(edit.Path) {
		return "", fmt.Errorf("%w: staged config path identity changed from %s to %s", dependencytxn.ErrGenerationConflict, edit.Path, requested)
	}
	return destination, nil
}

func validateStagedLockPathIdentity(edit pipeline.StagedLockEdit) (string, error) {
	destination := filepath.Clean(edit.Path)
	if edit.Path == "" || !filepath.IsAbs(destination) {
		return "", fmt.Errorf("%w: staged lock destination %q is not an absolute path", dependencytxn.ErrGenerationConflict, edit.Path)
	}
	validated, err := dependencytxn.ValidateDestination(destination)
	if err != nil {
		return "", fmt.Errorf("validate staged lock destination %s: %w", edit.Path, err)
	}
	return validated, nil
}

func PrepareExactLocks(ctx context.Context, repoRoot, localDir string, mode LockMode) (catalog.LoadOption, int, error) {
	option, changed, _, err := PrepareRuntimeLocks(ctx, repoRoot, localDir, mode)
	return option, changed, err
}

func ReconcileCommandDependencies(ctx context.Context, repoRoot, localDir string, ignoreBlocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
	var warnings []error
	option, _, lease, err := PrepareRuntimeLocks(ctx, repoRoot, localDir, LockMode{IgnoreBlocked: ignoreBlocked, StagedConfigEdits: edits, WarningSink: &warnings})
	if err != nil {
		return pipeline.DependencyLockView{}, err
	}
	return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, nil
}

func ReconcileCleanupDependencies(ctx context.Context, root string, edits []pipeline.StagedConfigEdit, reads []dependencytxn.ReadAssertion, scope dependencytxn.CommitScope) error {
	_, _, _, err := PrepareRuntimeLocks(ctx, root, "", LockMode{Offline: true, IgnoreBlocked: true, StagedConfigEdits: edits, CommitReads: reads, CommitScope: scope})
	return err
}

type configTransactionSnapshot struct {
	path         string
	generation   dependencytxn.Generation
	expectedMode os.FileMode
	bytes        []byte
	mode         os.FileMode
	reads        []config.ConfigRead
}

func SnapshotEffectiveConfigs(ec config.EffectiveConfig, overlays map[string][]byte) (map[config.Source]configTransactionSnapshot, error) {
	snapshots := make(map[config.Source]configTransactionSnapshot)
	writablePaths := make(map[string]config.Source)
	for _, layer := range ec.PresentLayers() {
		participantPath, err := canonicalConfigDependencyPath(layer.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve configuration transaction participant %s: %w", layer.Path, err)
		}
		if layer.Source.IsWritable() {
			if other, duplicate := writablePaths[participantPath]; duplicate {
				return nil, fmt.Errorf("%w: writable config layers %s and %s share destination %s", dependencytxn.ErrGenerationConflict, other, layer.Source, participantPath)
			}
			writablePaths[participantPath] = layer.Source
		}
		raw, generation, expectedMode, err := dependencytxn.ReadSnapshot(participantPath)
		if err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if generation.Exists {
			mode = expectedMode
		} else if _, staged := overlays[participantPath]; !staged {
			return nil, fmt.Errorf("configuration layer disappeared while loading: %s", layer.Path)
		}
		snapshots[layer.Source] = configTransactionSnapshot{path: participantPath, generation: generation, expectedMode: expectedMode, bytes: raw, mode: mode, reads: layer.Reads}
	}
	reads, err := effectiveReadAssertions(ec, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := dependencytxn.ValidateReadAssertions(reads); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func effectiveReadAssertions(ec config.EffectiveConfig, staged map[string]pipeline.StagedConfigEdit, stagedLocks map[string]pipeline.StagedLockEdit) ([]dependencytxn.ReadAssertion, error) {
	byAlias := make(map[string]dependencytxn.ReadAssertion)
	byPath := make(map[string]config.ConfigRead)
	for _, layer := range ec.PresentLayers() {
		for _, read := range layer.Reads {
			if previous, exists := byPath[read.Path]; exists {
				if dependencytxn.ClassifyDestination(previous.RequestedPath) != dependencytxn.AuthorityLegacy && dependencytxn.ClassifyDestination(read.RequestedPath) != dependencytxn.AuthorityLegacy {
					priorPath, priorErr := canonicalConfigDependencyPath(previous.RequestedPath)
					readPath, readErr := canonicalConfigDependencyPath(read.RequestedPath)
					if priorErr != nil || readErr != nil {
						return nil, errors.Join(priorErr, readErr)
					}
					if priorPath != readPath {
						return nil, fmt.Errorf("%w: layer destinations %s and %s resolve to the same object", dependencytxn.ErrUnsafePath, priorPath, readPath)
					}
				}
				if previous.Digest != read.Digest {
					return nil, fmt.Errorf("%w: inconsistent config read at %s", dependencytxn.ErrGenerationConflict, read.Path)
				}
			}
			byPath[read.Path] = read
			expected := dependencytxn.Generation{Exists: true, Digest: read.Digest}
			if read.Overlay {
				if staged == nil && stagedLocks == nil {
					continue
				}
				path := read.Path
				if _, exists := staged[path]; !exists {
					if canonical, err := canonicalConfigDependencyPath(read.RequestedPath); err == nil {
						path = canonical
					}
				}
				if edit, exists := staged[path]; exists && dependencytxn.Digest(edit.Bytes) == read.Digest {
					expected = edit.Expected
				} else if lockEdit, exists := stagedLocks[path]; exists && dependencytxn.Digest(lockEdit.Bytes) == read.Digest {
					expected = lockEdit.Expected
				} else {
					return nil, fmt.Errorf("%w: unmatched config overlay at %s", dependencytxn.ErrGenerationConflict, read.Path)
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

func SameConfigSnapshots(left, right map[config.Source]configTransactionSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for source, a := range left {
		b, ok := right[source]
		if !ok || a.path != b.path || a.generation != b.generation || !slices.Equal(a.reads, b.reads) {
			return false
		}
	}
	return true
}

func LoadStableEffectiveWith(ctx context.Context, options config.LoadEffectiveOptions, load func(context.Context, config.LoadEffectiveOptions) config.EffectiveConfig) (config.EffectiveConfig, map[config.Source]configTransactionSnapshot, error) {
	probe := load(ctx, options)
	before, err := SnapshotEffectiveConfigs(probe, options.FileOverlays)
	if err != nil {
		return probe, nil, err
	}
	fresh := load(ctx, options)
	after, err := SnapshotEffectiveConfigs(fresh, options.FileOverlays)
	if err != nil {
		return fresh, nil, err
	}
	if !SameConfigSnapshots(before, after) {
		return fresh, nil, fmt.Errorf("%w: configuration changed while loading", dependencytxn.ErrGenerationConflict)
	}
	return fresh, after, nil
}

type ExactLockLeaseFunc func(context.Context) (func(), error)

func PrepareRuntimeLocks(ctx context.Context, repoRoot, localDir string, mode LockMode) (resultOption catalog.LoadOption, changed int, resultLease ExactLockLeaseFunc, resultErr error) {
	started := time.Now()
	modeName := lockModeName(mode)
	scopes := selectedLockScopes(mode)
	slog.Info("lock lifecycle started", "mode", modeName, "scopes", scopes, "offline", mode.Offline, "check", mode.Check)
	defer func() {
		attrs := []any{"mode", modeName, "scopes", scopes, "duration_ms", time.Since(started).Milliseconds(), "changed", changed}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("lock lifecycle failed", attrs...)
			return
		}
		slog.Info("lock lifecycle completed", attrs...)
	}()

	stateDir, err := DependencyStateDir()
	if err != nil {
		return nil, 0, nil, err
	}
	if err := dependencytxn.ValidateReadAssertions(mode.CommitReads); err != nil {
		return nil, 0, nil, err
	}
	if err := PrepareDependencyTransactions(ctx, mode.ReadOnlyDependencies()); err != nil {
		return nil, 0, nil, err
	}
	overlays := make(map[string][]byte, len(mode.StagedConfigEdits)+len(mode.StagedLockEdits))
	stagedEdits := make(map[string]pipeline.StagedConfigEdit, len(mode.StagedConfigEdits))
	for _, edit := range mode.StagedConfigEdits {
		destination, err := validateStagedConfigPathIdentity(edit)
		if err != nil {
			return nil, 0, nil, err
		}
		if _, duplicate := stagedEdits[destination]; duplicate {
			return nil, 0, nil, fmt.Errorf("%w: duplicate staged config destination %s", dependencytxn.ErrGenerationConflict, destination)
		}
		edit.Path = destination
		stagedEdits[destination] = edit
		overlays[destination] = edit.Bytes
	}
	stagedLocks := make(map[string]pipeline.StagedLockEdit, len(mode.StagedLockEdits))
	for _, edit := range mode.StagedLockEdits {
		destination, err := validateStagedLockPathIdentity(edit)
		if err != nil {
			return nil, 0, nil, err
		}
		if _, duplicate := stagedLocks[destination]; duplicate {
			return nil, 0, nil, fmt.Errorf("%w: duplicate staged lock destination %s", dependencytxn.ErrGenerationConflict, destination)
		}
		if _, collision := stagedEdits[destination]; collision {
			return nil, 0, nil, fmt.Errorf("%w: staged lock destination %s collides with a staged config edit", dependencytxn.ErrGenerationConflict, destination)
		}
		edit.Path = destination
		stagedLocks[destination] = edit
		overlays[destination] = edit.Bytes
	}
	loadOptions := config.LoadEffectiveOptions{
		RepoRoot: repoRoot, LocalConfigDir: localDir,
		FileOverlays: overlays,
	}
	loadMode := pipeline.ConfigLoadStrict
	if mode.AllowTransientOrganization && !mode.Offline {
		loadMode = pipeline.ConfigLoadOnline
	} else if !mode.Check && mode.ReuseInstall != nil {
		loadMode = pipeline.ConfigLoadCached
	} else if !mode.Check && !mode.Offline && !mode.Locked {
		loadMode = pipeline.ConfigLoadOnline
	}
	loadEffective := func(ctx context.Context, opts config.LoadEffectiveOptions) config.EffectiveConfig {
		return pipeline.LoadEffective(ctx, loadMode, opts, mode.MarketplacePolicy)
	}
	ec, configSnapshots, err := LoadStableEffectiveWith(ctx, loadOptions, loadEffective)
	if err != nil {
		return nil, 0, nil, err
	}
	canBootstrapMarketplace := !mode.ReadOnlyDependencies() && mode.PrepareInstall == nil && mode.ReuseInstall == nil && len(mode.StagedConfigEdits) == 0 && len(mode.StagedLockEdits) == 0 && mode.CommitScope == nil
	if canBootstrapMarketplace && pipeline.NeedsLockBootstrap(ec.Errors) {
		bootstrapChanged, err := BootstrapMissingMarketplaceLocks(ctx, repoRoot, localDir, mode.Selects, mode.MarketplacePolicy)
		if err != nil {
			return nil, 0, nil, err
		}
		changed += bootstrapChanged
		ec, configSnapshots, err = LoadStableEffectiveWith(ctx, loadOptions, loadEffective)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	consumed := make(map[string]int, len(stagedEdits))
	consumedLocks := make(map[string]int, len(stagedLocks))
	for source, snapshot := range configSnapshots {
		if !source.IsWritable() {
			continue
		}
		edit, ok := stagedEdits[snapshot.path]
		if !ok {
			continue
		}
		consumed[snapshot.path]++
		// The snapshot was loaded from the staged overlay, so its digest describes
		// the desired bytes rather than the destination generation. Preserve the
		// generation captured before staging; the dependency transaction validates
		// it against the destination immediately before publication.
		snapshot.generation = edit.Expected
		if edit.Mode != 0 {
			snapshot.mode = edit.Mode
		}
		configSnapshots[source] = snapshot
	}
	for path := range stagedEdits {
		if consumed[path] != 1 {
			return nil, 0, nil, fmt.Errorf("%w: staged config %s matched %d effective writable layers", dependencytxn.ErrGenerationConflict, path, consumed[path])
		}
	}
	if mode.ReuseInstall != nil || mode.Check && !mode.Locked {
		ec.Errors = slices.DeleteFunc(ec.Errors, func(err error) bool {
			var lockErr *pipeline.LockViewError
			if !errors.As(err, &lockErr) || lockErr.Code != "lock.missing" {
				return false
			}
			if mode.ReuseInstall != nil {
				for _, path := range mode.ReuseInstall.LockPaths {
					if lockErr.Path == path {
						return true
					}
				}
			}
			if mode.Check && !mode.Locked {
				for _, layer := range ec.PresentLayers() {
					if lockScopeForSource(layer.Source) == lockErr.Scope {
						return true
					}
				}
			}
			return false
		})
	}
	if mode.Check && pipeline.NeedsLockBootstrap(ec.Errors) {
		changeErr := &LockLifecycleError{Code: "lock.change-required", Err: errors.Join(ec.Errors...)}
		for _, loadErr := range ec.Errors {
			var lockErr *pipeline.LockViewError
			if errors.As(loadErr, &lockErr) && lockErr.Code == "lock.missing" {
				changeErr.Scope = lockErr.Scope
				changeErr.Path = lockErr.Path
				break
			}
		}
		return nil, 1, nil, changeErr
	}
	if len(ec.Errors) > 0 && (!mode.IgnoreBlocked || !config.OnlyRegistryURLBlockedErrors(ec.Errors)) {
		return nil, 0, nil, errors.Join(ec.Errors...)
	}
	if mode.IgnoreBlocked && config.OnlyRegistryURLBlockedErrors(ec.Errors) {
		mode.Offline = true
		slog.Debug("blocked-registry removal reuses existing exact snapshots only")
	}
	if mode.PrepareInstall != nil {
		mode.PrepareInstall.reads, err = installPreparationReads(ec, stagedEdits, mode.PrepareInstall)
		if err != nil {
			return nil, 0, nil, err
		}
	}
	resolvedTargets, err := resolveUpgradeTargets(ec, mode)
	if err != nil {
		return nil, 0, nil, err
	}
	mode.UpgradeSelection = resolvedTargets.Selection
	if mode.UpgradeResult != nil {
		*mode.UpgradeResult = resolvedTargets
	}

	set := exactLockSet{bySource: map[string]lockedGit{}, byIdentity: map[string][]lockedGit{}}
	lockExpectations := map[string]dependencytxn.Generation{}
	lockModes := map[string]os.FileMode{}
	lockDesired := map[string][]byte{}
	type snapshotJob struct {
		source  config.Source
		entries []config.RegistryEntry
		doc     lockfile.Document
	}
	var snapshots []snapshotJob
	var desiredFiles []dependencytxn.DesiredFile
	configDesired := make(map[config.Source][]byte)
	for source, snapshot := range configSnapshots {
		if edit, ok := stagedEdits[snapshot.path]; ok {
			configDesired[source] = edit.Bytes
		}
	}
	organizationRequiresLock := false
	var checkUpgradeErr *LockLifecycleError
	checkHasDrift := false
	// Exact snapshot acquisition, verification, and dependency publication form
	// one GC-critical section. Preserve the lock order: namespace before
	// dependency transaction destinations.
	var releaseSnapshotLease func()
	snapshotLeaseAttempted := false
	ensureSnapshotLease := func() error {
		if snapshotLeaseAttempted {
			return nil
		}
		snapshotLeaseAttempted = true
		var err error
		if mode.Check || mode.Offline {
			releaseSnapshotLease, err = cache.AcquireExactGitNamespaceReadOnlyLeaseWithContext(ctx)
		} else {
			releaseSnapshotLease, err = cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
		}
		if err != nil {
			// A cold read-only check must not create coordination state. Let
			// addLayerSnapshots return its established snapshot-unavailable
			// diagnostic when there is no namespace to protect.
			if (mode.Check || mode.Offline) && errors.Is(err, cache.ErrExactGitCoordinationUnavailable) {
				return nil
			}
			return fmt.Errorf("acquire exact cache snapshot publication lease: %w", err)
		}
		ctx = cache.ContextWithExactGitNamespaceLease(ctx)
		return nil
	}
	defer func() {
		if releaseSnapshotLease != nil {
			releaseSnapshotLease()
		}
	}()
	for _, layer := range ec.PresentLayers() {
		scope := lockScopeForSource(layer.Source)
		remote := pipeline.GitRegistryEntries(layer.Sources)
		if scope == lockfile.ScopeOrganization && len(remote) > 0 {
			organizationRequiresLock = true
		}
		configPath := layer.Path
		if snapshot, ok := configSnapshots[layer.Source]; ok {
			configPath = snapshot.path
		}
		lockPath, pathErr := lockfile.PathForConfig(configPath)
		if pathErr != nil {
			name := "lock.yaml"
			if scope == lockfile.ScopeLocal {
				name = "lock.local.yaml"
			}
			lockPath = filepath.Join(filepath.Dir(configPath), name)
		}
		requestedLockPath := lockPath
		lockPath, pathErr = rejectGeneratedLockSymlink(lockPath)
		if pathErr != nil {
			return nil, changed, nil, fmt.Errorf("resolve lock destination %s: %w", requestedLockPath, pathErr)
		}
		selected := mode.Selects == nil || mode.Selects[scope]
		current, generation, exists, loadErr := pipeline.ReadLayerLock(lockPath, scope)
		if loadErr != nil {
			if mode.Upgrade && !selected {
				continue
			}
			return nil, changed, nil, loadErr
		}
		stagedLock, hasStagedLock := stagedLocks[lockPath]
		if hasStagedLock && !layer.Source.IsWritable() {
			hasStagedLock = false
		}
		if len(remote) > 0 || exists || hasStagedLock {
			lockExpectations[lockPath] = generation
			_, currentMode, stateErr := dependencytxn.ReadGenerationAndMode(lockPath)
			if stateErr != nil {
				return nil, changed, nil, stateErr
			}
			lockModes[lockPath] = currentMode
		}
		if len(remote) == 0 && !exists && !hasStagedLock {
			continue
		}
		implicitOrg := scope == lockfile.ScopeOrganization && !mode.ExplicitOrganization
		if implicitOrg && !exists && mode.PrepareInstall == nil {
			for _, entry := range remote {
				canonical, err := CanonicalRegistryURL(entry.URL)
				if err != nil {
					return nil, changed, nil, err
				}
				set.bySource[sourceLockKey(layer.Source, canonical, entry.Ref)] = lockedGit{unlocked: true}
			}
			slog.Debug("organization registry layer remains unlocked", "registries", len(remote))
			continue
		}
		if !selected && !mode.Upgrade {
			continue
		}
		if mode.Upgrade && !selected && !mode.ReconcileUnselected {
			if !exists || current == nil || !unselectedLayerIsFresh(scope, remote, *current) {
				continue
			}
			if err := ensureSnapshotLease(); err != nil {
				return nil, changed, nil, err
			}
			snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: *current})
			continue
		}
		if !exists && mode.Locked && !implicitOrg {
			return nil, changed, nil, &LockLifecycleError{Code: "lock.missing", Scope: scope, Path: lockPath, Err: os.ErrNotExist}
		}
		if mode.Check {
			fresh := exists && current != nil && unselectedLayerIsFresh(scope, remote, *current)
			if fresh && current.APIVersion == lockfile.APIVersionV1 && !implicitOrg && !mode.Locked {
				if checkUpgradeErr == nil {
					checkUpgradeErr = &LockLifecycleError{Code: "lock.version-upgrade-required", Scope: scope, Path: lockPath, Err: errors.New("lock schema upgrade to v2 required")}
				}
				fresh = false
			} else if !fresh {
				checkHasDrift = true
			}
			if !fresh {
				if mode.Locked {
					return nil, changed, nil, &LockLifecycleError{Code: "lock.stale", Scope: scope, Path: lockPath, Err: errors.New("lock does not match configuration")}
				}
				changed++
				continue
			}
			if err := ensureSnapshotLease(); err != nil {
				return nil, changed, nil, err
			}
			snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: *current})
			continue
		}

		if hasStagedLock {
			consumedLocks[lockPath]++
			stagedDoc, decodeErr := lockfile.Decode(stagedLock.Bytes)
			if decodeErr == nil {
				decodeErr = lockfile.ValidateScope(stagedDoc, scope)
			}
			if decodeErr != nil {
				return nil, changed, nil, &LockLifecycleError{Code: "lock.invalid", Scope: scope, Path: lockPath, Err: decodeErr}
			}
			current = &stagedDoc
		}
		operation := lockfile.OperationSync
		if mode.Upgrade && selected && !implicitOrg {
			operation = lockfile.OperationUpgrade
		}
		var gitDesired reconciledDocument
		var gitFresh bool
		if mode.ReuseInstall != nil {
			gitDesired, gitFresh = reuseInstallDocument(mode.ReuseInstall, lockPath, scope, remote, current)
		} else {
			gitDesired, gitFresh = reconcileLayerDocument(ctx, scope, remote, current, operation, mode.UpgradeSelection, mode.Offline, ensureSnapshotLease)
		}
		if gitDesired.err != nil {
			return nil, changed, nil, gitDesired.err
		}
		if exists && current != nil && current.APIVersion == lockfile.APIVersionV1 && writableV1RequiresUpgrade(scope, mode) {
			gitFresh = false
		}
		doc := gitDesired.doc
		if mode.PrepareInstall != nil {
			mode.PrepareInstall.Documents[lockPath] = doc
		}
		fresh := exists && gitFresh && !hasStagedLock
		if !fresh {
			if mode.Locked && !implicitOrg || implicitOrg && exists || !selected && !mode.Upgrade {
				return nil, changed, nil, &LockLifecycleError{Code: "lock.stale", Scope: scope, Path: lockPath, Err: errors.New("lock does not match configuration")}
			}
			if !implicitOrg {
				changed++
				data, err := lockfile.Encode(doc)
				if err != nil {
					return nil, changed, nil, err
				}
				lockDesired[lockPath] = data
				fileMode := lockModes[lockPath]
				if !generation.Exists {
					fileMode = 0o644
				}
				desiredFiles = append(desiredFiles, dependencytxn.DesiredFile{Path: lockPath, Expected: generation, ExpectedMode: lockModes[lockPath], Bytes: data, Mode: fileMode})
			}
		}
		layerChangedCount := 0
		if !fresh {
			layerChangedCount = 1
		}
		slog.Debug("lock layer reconciled", "scope", scope, "operation", operation, "retained", gitDesired.stats.retained, "acquired", gitDesired.stats.acquired, "changed", layerChangedCount)
		snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: doc})
	}
	for path := range stagedLocks {
		if consumedLocks[path] != 1 {
			return nil, changed, nil, fmt.Errorf("%w: staged lock %s matched %d effective writable layers", dependencytxn.ErrGenerationConflict, path, consumedLocks[path])
		}
	}
	for _, job := range snapshots {
		if len(job.doc.Registries) > 0 {
			if err := ensureSnapshotLease(); err != nil {
				return nil, changed, nil, err
			}
		}
		if err := addLayerSnapshots(ctx, &set, job.source, job.entries, job.doc, mode.Offline, mode.Check, mode.WarningSink); err != nil {
			return nil, changed, nil, err
		}
	}
	if !mode.Check {
		for _, layer := range ec.PresentLayers() {
			snapshot, ok := configSnapshots[layer.Source]
			if !ok {
				continue
			}
			if data, staged := configDesired[layer.Source]; staged {
				desiredFiles = append(desiredFiles, dependencytxn.DesiredFile{Path: snapshot.path, Expected: snapshot.generation, ExpectedMode: snapshot.expectedMode, Bytes: data, Mode: snapshot.mode})
				delete(configSnapshots, layer.Source)
			}
		}
	}
	if !mode.Check && mode.PrepareInstall == nil && (len(desiredFiles) > 0 || mode.CommitScope != nil) {
		for _, layer := range ec.PresentLayers() {
			if !layer.Source.IsWritable() && !mode.ExplicitOrganization {
				continue
			}
			snapshot, ok := configSnapshots[layer.Source]
			if !ok || !snapshot.generation.Exists {
				continue
			}
			desiredFiles = append(desiredFiles, dependencytxn.DesiredFile{Path: snapshot.path, Expected: snapshot.generation, ExpectedMode: snapshot.expectedMode, Bytes: snapshot.bytes, Mode: snapshot.mode})
		}
		for _, edit := range stagedEdits {
			if _, err := validateStagedConfigPathIdentity(edit); err != nil {
				return nil, changed, nil, err
			}
		}
		for _, file := range desiredFiles {
			if dependencytxn.ClassifyDestination(file.Path) != dependencytxn.AuthorityLegacy {
				// Managed layer directories are created below the captured
				// layer root so a replaced or symlinked .agents cannot
				// redirect the creation into another tree.
				if err := dependencytxn.EnsureDestinationDir(file.Path); err != nil {
					return nil, changed, nil, fmt.Errorf("create dependency destination directory: %w", err)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
				return nil, changed, nil, fmt.Errorf("create dependency destination directory: %w", err)
			}
		}
		reads, err := effectiveReadAssertions(ec, stagedEdits, stagedLocks)
		if err != nil {
			return nil, changed, nil, err
		}
		reads = append(reads, mode.CommitReads...)
		if mode.CommitScope != nil && len(desiredFiles) == 0 {
			return nil, changed, nil, errors.New("owned cleanup dependency transaction has no desired files")
		}
		commitOptions := []dependencytxn.Option{dependencytxn.WithReadAssertions(reads)}
		if mode.CommitScope != nil {
			commitOptions = append(commitOptions, dependencytxn.WithCommitScope(mode.CommitScope))
		}
		commitStarted := time.Now()
		commitResult, err := CommitDependencyTransaction(ctx, stateDir, desiredFiles, commitOptions...)
		if err != nil {
			slog.Error("lock transaction commit failed", "files", len(desiredFiles), "duration_ms", time.Since(commitStarted).Milliseconds(), "error", err)
			return nil, changed, nil, fmt.Errorf("commit dependency config and locks: %w", err)
		}
		slog.Info("lock transaction commit completed", "files", len(desiredFiles), "changed", len(commitResult.Changed), "duration_ms", time.Since(commitStarted).Milliseconds())
	} else {
		slog.Debug("lock transaction commit skipped", "check", mode.Check, "files", len(desiredFiles))
	}
	if mode.ExplicitOrganization && organizationRequiresLock && !mode.Check {
		verificationStarted := time.Now()
		if err := config.ValidateOrganizationTrustUnit(ec.Organization.Path, true, false); err != nil {
			slog.Error("organization lock verification failed", "duration_ms", time.Since(verificationStarted).Milliseconds(), "error", err)
			return nil, changed, nil, fmt.Errorf("verify resulting organization config and lock: %w", err)
		}
		slog.Info("organization lock verification completed", "duration_ms", time.Since(verificationStarted).Milliseconds())
	}
	if mode.PrepareInstall == nil {
		for path, data := range lockDesired {
			lockExpectations[path] = dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}
		}
	}
	validateExactLocks := func() error {
		paths := make([]string, 0, len(lockExpectations))
		for path := range lockExpectations {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			got, err := dependencytxn.ReadGeneration(path)
			if err != nil {
				return fmt.Errorf("read exact lock generation %s: %w", path, err)
			}
			if got != lockExpectations[path] {
				return fmt.Errorf("%w: exact lock changed at %s", dependencytxn.ErrGenerationConflict, path)
			}
		}
		return nil
	}

	option := catalog.WithVerifiedExactSnapshots(func(entry config.RegistryEntry) (string, string, error) {
		if err := validateExactLocks(); err != nil {
			return "", "", err
		}
		canonical, err := CanonicalRegistryURL(entry.URL)
		if err != nil {
			return "", "", err
		}
		key := sourceLockKey(entry.Source, canonical, entry.Ref)
		locked, ok := set.bySource[key]
		if !ok {
			candidates := set.byIdentity[gitLockIdentity(canonical, entry.Ref)]
			if len(candidates) != 1 {
				return "", "", &LockLifecycleError{Code: "lock.lookup", Err: fmt.Errorf("no unambiguous lock for %s@%s", canonical, entry.Ref)}
			}
			locked = candidates[0]
		}
		if locked.unlocked {
			return "", "", nil
		}
		catalogKey := canonical + "@" + locked.commit.String() + ":" + entry.Path
		return locked.snapshot, catalogKey, nil
	}, set.catalogs)
	hasExactConsumers := len(set.byIdentity) > 0
	resultLease = func(ctx context.Context) (func(), error) {
		if !hasExactConsumers {
			return func() {}, nil
		}
		var (
			release func()
			err     error
		)
		if mode.Check || mode.Offline {
			release, err = cache.AcquireExactGitNamespaceReadOnlyLeaseWithContext(ctx)
		} else {
			release, err = cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
		}
		if err != nil {
			return nil, err
		}
		if err := validateExactLocks(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
	if mode.PrepareInstall != nil {
		if err := dependencytxn.ValidateReadAssertions(mode.PrepareInstall.reads); err != nil {
			return nil, changed, nil, err
		}
		mode.PrepareInstall.ctx = ctx
		mode.PrepareInstall.discoveryConfig = ec.Merged
		mode.PrepareInstall.release = releaseSnapshotLease
		releaseSnapshotLease = nil
	}
	if mode.UpgradeResult != nil && !checkHasDrift {
		mode.UpgradeResult.CheckUpgradeErr = checkUpgradeErr
	}
	return option, changed, resultLease, nil
}

type reconciledDocument struct {
	doc   lockfile.Document
	stats lockReconcileStats
	err   error
}

func runRegistryJobs(count int, run func(int) error) error {
	started := time.Now()
	jobs := make(chan int)
	errs := make([]error, count)
	var workers sync.WaitGroup
	for range min(4, count) {
		workers.Go(func() {
			for i := range jobs {
				errs[i] = run(i)
			}
		})
	}
	for i := range count {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	err := errors.Join(errs...)
	slog.Debug("registry jobs completed", "registries", count, "concurrency", min(4, count), "duration_ms", time.Since(started).Milliseconds(), "success", err == nil)
	return err
}

func gitReconciliationDocument(scope lockfile.Scope, current *lockfile.Document) lockfile.Document {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: scope}
	if current != nil {
		doc.Marketplaces = current.Marketplaces
	}
	return doc
}

func reconcileLayerDocument(ctx context.Context, scope lockfile.Scope, entries []config.RegistryEntry, current *lockfile.Document, operation lockfile.Operation, selection lockfile.UpgradeSelection, offline bool, beforeSnapshot func() error) (reconciledDocument, bool) {
	doc := gitReconciliationDocument(scope, current)
	stats := lockReconcileStats{}
	old := map[string]lockfile.GitRegistry{}
	if current != nil {
		for _, registry := range current.Registries {
			old[gitLockIdentity(registry.URL, registry.Ref)] = registry
		}
	}
	grouped := groupRemoteEntries(entries)
	identities := make([]string, 0, len(grouped))
	for identity := range grouped {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	var acquisitions []remoteGroup
	fresh := current != nil
	for _, identity := range identities {
		group := grouped[identity]
		locked, ok := old[identity]
		rootRequirements := make([]lockfile.GitRootRequirement, 0, len(group.entries))
		for _, entry := range group.entries {
			rootPath := entry.Path
			if rootPath == "" {
				rootPath = "."
			}
			rootRequirements = append(rootRequirements, lockfile.GitRootRequirement{Path: rootPath, SourceHarness: entry.SourceHarness})
		}
		upgrade := operation == lockfile.OperationUpgrade && selection.SelectsGit(lockfile.GitRequirement{URL: group.canonical, Ref: group.ref, Roots: rootRequirements})
		if ok && !upgrade {
			if projected, matches := lockfile.ProjectGitRoots(locked.Roots, rootRequirements); matches {
				if len(projected) != len(locked.Roots) {
					fresh = false
				}
				locked.Roots = projected
				doc.Registries = append(doc.Registries, locked)
				stats.retained++
				continue
			}
		}
		fresh = false
		if offline {
			return reconciledDocument{stats: stats, err: &LockLifecycleError{Code: "lock.offline-resolution", Scope: scope, Err: errors.New("offline mode cannot resolve a stale lock")}}, false
		}
		acquisitions = append(acquisitions, group)
	}
	if len(acquisitions) > 0 {
		if err := beforeSnapshot(); err != nil {
			return reconciledDocument{stats: stats, err: err}, false
		}
		acquired := make([]lockfile.GitRegistry, len(acquisitions))
		err := runRegistryJobs(len(acquisitions), func(i int) error {
			group := acquisitions[i]
			registry, err := acquireLockedRegistry(ctx, group.canonical, group.fetchURL, group.ref, group.entries)
			acquired[i] = registry
			return err
		})
		if err != nil {
			return reconciledDocument{stats: stats, err: err}, false
		}
		doc.Registries = append(doc.Registries, acquired...)
		stats.acquired += len(acquired)
	}
	if len(old) != len(grouped) {
		fresh = false
	}
	normalized, err := lockfile.Normalize(doc)
	return reconciledDocument{doc: normalized, stats: stats, err: err}, fresh
}

type remoteGroup struct {
	canonical, fetchURL, ref string
	entries                  []config.RegistryEntry
}

func groupRemoteEntries(entries []config.RegistryEntry) map[string]remoteGroup {
	groups := map[string]remoteGroup{}
	for _, entry := range entries {
		canonical, _ := CanonicalRegistryURL(entry.URL)
		identity := gitLockIdentity(canonical, entry.Ref)
		group := groups[identity]
		group.canonical, group.fetchURL, group.ref = canonical, entry.URL, entry.Ref
		group.entries = append(group.entries, entry)
		groups[identity] = group
	}
	return groups
}

func acquireLockedRegistry(ctx context.Context, canonical, fetchURL, ref string, entries []config.RegistryEntry) (lockfile.GitRegistry, error) {
	if err := ctx.Err(); err != nil {
		return lockfile.GitRegistry{}, err
	}
	oid, err := cache.ResolveExactGitRefWithContext(ctx, canonical, fetchURL, ref)
	if err != nil {
		return lockfile.GitRegistry{}, fmt.Errorf("resolve exact Git ref %s@%s: %w", canonical, ref, err)
	}
	snapshot, err := cache.AcquireExactGitWithContext(ctx, canonical, fetchURL, oid)
	if err != nil {
		return lockfile.GitRegistry{}, fmt.Errorf("acquire exact Git snapshot %s: %w", oid.String(), err)
	}
	if LockSnapshotAcquiredHook != nil {
		LockSnapshotAcquiredHook()
	}
	commit, err := lockfile.ParseGitOID(oid.String())
	if err != nil {
		return lockfile.GitRegistry{}, err
	}
	registry := lockfile.GitRegistry{URL: canonical, Ref: ref, Commit: commit}
	seen := map[string]bool{}
	for _, entry := range entries {
		rootPath := entry.Path
		if rootPath == "" {
			rootPath = "."
		}
		built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: rootPath, SourceHarness: entry.SourceHarness})
		if err != nil {
			return lockfile.GitRegistry{}, fmt.Errorf("build source revision for %s/%s: %w", canonical, rootPath, err)
		}
		key := rootPath + "\x00" + built.SourceHarness
		if seen[key] {
			continue
		}
		seen[key] = true
		root := lockfile.GitRoot{Path: rootPath, SourceHarness: built.SourceHarness, RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
		for _, resource := range built.Manifest.Resources {
			if resource.ArchiveDigest == "" {
				continue
			}
			support := make(map[string]lockfile.Digest, len(resource.SupportFiles))
			for name, digest := range resource.SupportFiles {
				support[name] = lockfile.Digest(digest)
			}
			root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest), SupportFiles: support})
		}
		registry.Roots = append(registry.Roots, root)
	}
	return registry, nil
}

func writableV1RequiresUpgrade(scope lockfile.Scope, mode LockMode) bool {
	if mode.ReadOnlyDependencies() {
		return false
	}
	return scope != lockfile.ScopeOrganization || mode.ExplicitOrganization
}

func unselectedLayerIsFresh(_ lockfile.Scope, registries []config.RegistryEntry, doc lockfile.Document) bool {
	return documentMatchesEntries(doc, registries)
}

func documentMatchesEntries(doc lockfile.Document, entries []config.RegistryEntry) bool {
	groups := groupRemoteEntries(entries)
	if len(doc.Registries) != len(groups) {
		return false
	}
	for _, registry := range doc.Registries {
		group, ok := groups[gitLockIdentity(registry.URL, registry.Ref)]
		if !ok {
			return false
		}
		projected, matches := ProjectLockedRoots(registry.Roots, group.entries)
		if !matches || len(projected) != len(registry.Roots) {
			return false
		}
	}
	return true
}

func ProjectLockedRoots(roots []lockfile.GitRoot, entries []config.RegistryEntry) ([]lockfile.GitRoot, bool) {
	requirements := make([]lockfile.GitRootRequirement, len(entries))
	for i, entry := range entries {
		requirements[i] = lockfile.GitRootRequirement{Path: entry.Path, SourceHarness: entry.SourceHarness}
	}
	return lockfile.ProjectGitRoots(roots, requirements)
}

func addLayerSnapshots(ctx context.Context, set *exactLockSet, source config.Source, entries []config.RegistryEntry, doc lockfile.Document, offline, readOnly bool, warningSink *[]error) (resultErr error) {
	started := time.Now()
	verified := 0
	defer func() {
		attrs := []any{"scope", lockScopeForSource(source), "registries", len(doc.Registries), "verified", verified, "offline", offline, "duration_ms", time.Since(started).Milliseconds()}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("lock snapshot verification failed", attrs...)
			return
		}
		slog.Debug("lock snapshot verification completed", attrs...)
	}()
	fetchURLs := map[string]string{}
	for _, entry := range entries {
		canonical, err := CanonicalRegistryURL(entry.URL)
		if err != nil {
			return err
		}
		fetchURLs[gitLockIdentity(canonical, entry.Ref)] = entry.URL
	}
	results := make([]struct {
		locked   lockedGit
		catalogs map[string]map[string]*catalog.Catalog
		warnings []error
		verified int
	}, len(doc.Registries))
	err := runRegistryJobs(len(doc.Registries), func(i int) error {
		registry := doc.Registries[i]
		result := &results[i]
		result.catalogs = make(map[string]map[string]*catalog.Catalog)
		cacheOID, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			return err
		}
		var snapshot string
		consume := func(path string) error {
			snapshot = path
			for _, root := range registry.Roots {
				rootResult, verifyErr := sourcerevision.VerifyLockedGitRootWithCatalog(path, root)
				if verifyErr != nil {
					return &LockLifecycleError{Code: "lock.manifest-mismatch", Err: fmt.Errorf("verify %s@%s root %q: %w", registry.URL, registry.Commit.String(), root.Path, verifyErr)}
				}
				if rootResult.Catalog != nil {
					rootPath := filepath.Join(path, filepath.FromSlash(root.Path))
					if result.catalogs[rootPath] == nil {
						result.catalogs[rootPath] = make(map[string]*catalog.Catalog)
					}
					result.catalogs[rootPath][root.SourceHarness] = rootResult.Catalog
				}
				for _, warning := range rootResult.Warnings {
					result.warnings = append(result.warnings, warning)
				}
				result.verified++
			}
			return nil
		}
		if !offline && !readOnly {
			err = cache.WithAcquiredExactGit(ctx, registry.URL, fetchURLs[gitLockIdentity(registry.URL, registry.Ref)], cacheOID, consume)
		} else {
			err = cache.WithExactGitReadOnly(ctx, registry.URL, cacheOID, consume)
		}
		if err != nil {
			var lifecycleErr *LockLifecycleError
			if errors.As(err, &lifecycleErr) {
				return err
			}
			code := "lock.snapshot-unavailable"
			if offline {
				code = "lock.offline-snapshot-missing"
			}
			return &LockLifecycleError{Code: code, Err: err}
		}
		result.locked = lockedGit{canonical: registry.URL, fetchURL: fetchURLs[gitLockIdentity(registry.URL, registry.Ref)], ref: registry.Ref, commit: registry.Commit, snapshot: snapshot}
		return nil
	})
	for _, result := range results {
		verified += result.verified
	}
	if err != nil {
		return err
	}
	for _, result := range results {
		locked := result.locked
		set.bySource[sourceLockKey(source, locked.canonical, locked.ref)] = locked
		identity := gitLockIdentity(locked.canonical, locked.ref)
		set.byIdentity[identity] = append(set.byIdentity[identity], locked)
		if set.catalogs == nil {
			set.catalogs = make(map[string]map[string]*catalog.Catalog)
		}
		for path, catalogs := range result.catalogs {
			if set.catalogs[path] == nil {
				set.catalogs[path] = make(map[string]*catalog.Catalog)
			}
			for harness, loaded := range catalogs {
				set.catalogs[path][harness] = loaded
			}
		}
		if warningSink != nil {
			*warningSink = append(*warningSink, result.warnings...)
		}
	}
	return nil
}

func lockScopeForSource(source config.Source) lockfile.Scope {
	switch source {
	case config.Organization:
		return lockfile.ScopeOrganization
	case config.UserPersonal:
		return lockfile.ScopeUser
	case config.RepoCommitted:
		return lockfile.ScopeRepo
	default:
		return lockfile.ScopeLocal
	}
}

func CanonicalRegistryURL(raw string) (string, error) {
	return lockfile.CanonicalGitConfigURL(raw)
}

func sourceLockKey(source config.Source, canonical, ref string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", source, canonical, ref)
}
func gitLockIdentity(canonical, ref string) string { return canonical + "\x00" + ref }
