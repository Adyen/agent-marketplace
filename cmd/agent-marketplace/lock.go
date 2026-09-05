package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

var commitDependencyTransaction = dependencytxn.Commit

type lockLifecycleError struct {
	Code  string
	Scope lockfile.Scope
	Path  string
	Err   error
}

func (e *lockLifecycleError) Error() string {
	return fmt.Sprintf("%s: %s lock %q: %v", e.Code, e.Scope, e.Path, e.Err)
}
func (e *lockLifecycleError) Unwrap() error { return e.Err }

type lockMode struct {
	locked               bool
	offline              bool
	upgrade              bool
	check                bool
	selects              map[lockfile.Scope]bool
	explicitOrganization bool
	upgradeSelection     lockfile.UpgradeSelection
	upgradeTargets       *upgradeTargetSpec
	upgradeResult        *upgradeTargetResult
	ignoreBlocked        bool
	reconcileUnselected  bool
	stagedConfigEdits    []pipeline.StagedConfigEdit
	warningSink          *[]error
}

type lockReconcileStats struct {
	retained int
	acquired int
}

func lockModeName(mode lockMode) string {
	switch {
	case mode.upgrade:
		return "upgrade"
	case mode.locked:
		return "locked"
	case mode.offline:
		return "offline"
	case mode.check:
		return "check"
	default:
		return "sync"
	}
}

func selectedLockScopes(mode lockMode) []string {
	if mode.selects == nil {
		return []string{"effective"}
	}
	scopes := make([]string, 0, len(mode.selects))
	for _, scope := range []lockfile.Scope{lockfile.ScopeOrganization, lockfile.ScopeUser, lockfile.ScopeRepo, lockfile.ScopeLocal} {
		if mode.selects[scope] {
			scopes = append(scopes, string(scope))
		}
	}
	return scopes
}

type upgradeTargetSpec struct {
	registry string
	ref      string
	path     string
	kind     string
	names    []string
}

type upgradeTargetResult struct {
	selection lockfile.UpgradeSelection
	notices   []string
}

type exactLockSet struct {
	bySource   map[string]lockedGit
	byIdentity map[string][]lockedGit
}

type lockedGit struct {
	canonical string
	fetchURL  string
	ref       string
	commit    lockfile.GitOID
	snapshot  string
}

func newLockCmd() *cobra.Command {
	var scope, registry, ref, registryPath, kind string
	var check, upgrade bool
	cmd := &cobra.Command{
		Use:   "lock [resource ...]",
		Short: "Resolve and verify exact Git dependency locks",
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, explicitOrg, err := parseLockSelection(scope, check)
			if err != nil {
				return err
			}
			targets, err := parseUpgradeTargets(upgrade, args, registry, ref, registryPath, kind)
			if err != nil {
				return err
			}
			result := &upgradeTargetResult{}
			var warnings []error
			repoRoot := config.FindRepoRoot()
			_, changed, err := prepareExactLocks(cmd.Context(), repoRoot, config.FindLocalConfigDir(), lockMode{
				upgrade: upgrade, check: check, selects: selected, explicitOrganization: explicitOrg, upgradeTargets: targets, upgradeResult: result, warningSink: &warnings,
			})
			if err != nil {
				return err
			}
			if check && changed > 0 {
				return &lockLifecycleError{Code: "lock.change-required", Err: errors.New("lock files would change")}
			}
			if err := writeGroupedWarnings(cmd.OutOrStdout(), warnings, false); err != nil {
				return err
			}
			for _, notice := range result.notices {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), notice); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Locks: %d changed\n", changed)
			return err
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "all", "Lock scope: user, repo, local, all, or organization")
	cmd.Flags().BoolVar(&check, "check", false, "Check whether selected locks would change without writing")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "Refresh selected locks to current Git refs")
	cmd.Flags().StringVar(&registry, "registry", "", "Target a configured Git registry URL")
	cmd.Flags().StringVar(&ref, "ref", "", "Narrow --registry to a configured Git ref")
	cmd.Flags().StringVar(&registryPath, "path", "", "Narrow --registry to a locked root path")
	cmd.Flags().StringVar(&kind, "kind", "", "Resource kind for direct git targets")
	return cmd
}

func parseUpgradeTargets(upgrade bool, args []string, registry, ref, registryPath, kind string) (*upgradeTargetSpec, error) {
	hasGit := registry != "" || ref != "" || registryPath != "" || kind != ""
	if !upgrade && (len(args) > 0 || hasGit) {
		return nil, errors.New("upgrade targets require --upgrade")
	}
	if registry == "" && (len(args) > 0 || ref != "" || registryPath != "" || kind != "") {
		return nil, errors.New("resource targets, --ref, --path, and --kind require --registry")
	}
	if len(args) > 0 && kind == "" {
		return nil, errors.New("named --registry targets require --kind")
	}
	return &upgradeTargetSpec{registry: registry, ref: ref, path: registryPath, kind: kind, names: append([]string(nil), args...)}, nil
}

func parseLockSelection(value string, check bool) (map[lockfile.Scope]bool, bool, error) {
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

func resolveUpgradeTargets(ec config.EffectiveConfig, mode lockMode) (upgradeTargetResult, error) {
	if !mode.upgrade || mode.upgradeTargets == nil {
		return upgradeTargetResult{selection: lockfile.UpgradeSelection{All: mode.upgrade}}, nil
	}
	targets := mode.upgradeTargets
	if targets.registry == "" {
		return upgradeTargetResult{selection: lockfile.UpgradeSelection{All: true}}, nil
	}
	result := upgradeTargetResult{}
	if targets.registry != "" {
		canonical, err := canonicalRegistryURL(targets.registry)
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
			if mode.selects != nil && !mode.selects[scope] {
				continue
			}
			current, err := loadExistingLayerDocument(layer, scope)
			if err != nil {
				return result, err
			}
			for _, entry := range remoteEntries(layerRegistries(layer)) {
				entryURL, err := canonicalRegistryURL(entry.URL)
				if err != nil || entryURL != canonical || targets.ref != "" && targets.ref != entry.Ref {
					continue
				}
				rootPath := entry.Path
				if rootPath == "" {
					rootPath = "."
				}
				if targets.path != "" && filepath.Clean(targets.path) != rootPath {
					continue
				}
				if current == nil {
					return result, fmt.Errorf("git target %s requires an existing %s lock inventory", targets.registry, scope)
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
			return result, fmt.Errorf("git target %s matched no configured registry; qualify it with --ref, --path, or --scope", targets.registry)
		}
		if len(targets.names) == 0 {
			for _, occurrence := range occurrences {
				selector := occurrence.selector
				selector.Path = ""
				result.selection.Git = appendGitSelector(result.selection.Git, selector)
			}
			if len(result.selection.Git) > 1 {
				return result, fmt.Errorf("git target %s is ambiguous; qualify it with --ref, --path, or --scope", targets.registry)
			}
			return result, nil
		}
		for _, name := range targets.names {
			type gitCandidate struct {
				selector    lockfile.GitUpgradeSelector
				matchedPath string
			}
			var candidates []gitCandidate
			for _, occurrence := range occurrences {
				if !lockedRootHasResource(occurrence.root, targets.kind, name) {
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
				return result, fmt.Errorf("git target %s/%s does not exist in the selected locked root inventory", targets.kind, name)
			}
			if len(candidates) > 1 {
				return result, fmt.Errorf("git target %s/%s is ambiguous; qualify it with --ref, --path, or --scope", targets.kind, name)
			}
			candidate := candidates[0]
			result.selection.Git = appendGitSelector(result.selection.Git, candidate.selector)
			result.notices = append(result.notices, fmt.Sprintf("git target %s/%s expands to source occurrence %s@%s (all locked roots; matched path %s)", targets.kind, name, candidate.selector.URL, candidate.selector.Ref, candidate.matchedPath))
		}
		return result, nil
	}

	return result, nil
}

func loadExistingLayerDocument(layer config.LayerResult, scope lockfile.Scope) (*lockfile.Document, error) {
	if len(remoteEntries(layerRegistries(layer))) == 0 {
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
	doc, _, _, err := loadLayerLock(lockPath, scope)
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

func dependencyStateDir() (string, error) {
	if root := os.Getenv("XDG_STATE_HOME"); root != "" {
		if !filepath.IsAbs(root) {
			return "", errors.New("XDG_STATE_HOME must be absolute")
		}
		canonical, err := canonicalizeFuturePath(root)
		if err != nil {
			return "", err
		}
		return filepath.Join(canonical, "agent-marketplace", "dependencies"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(home)
	if err == nil {
		home = canonical
	}
	return filepath.Join(home, ".local", "state", "agent-marketplace", "dependencies"), nil
}

func recoverDependencyTransactions(ctx context.Context) error {
	stateDir, err := dependencyStateDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
		slog.Debug("lock transaction recovery skipped", "reason", "state absent")
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect dependency transaction state: %w", err)
	}

	// Recovery can publish the same config/lock destinations as a normal
	// dependency transaction. Hold the shared exact-cache namespace lease in
	// the same order as normal publication so exclusive GC cannot inventory a
	// half-recovered transaction and delete its newly reachable snapshots.
	releaseExactNamespace, err := cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
	if err != nil {
		return fmt.Errorf("acquire exact cache recovery lease: %w", err)
	}
	defer releaseExactNamespace()

	started := time.Now()
	if err := dependencytxn.Recover(ctx, stateDir); err != nil {
		slog.Error("lock transaction recovery failed", "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return fmt.Errorf("recover dependency lock transaction: %w", err)
	}
	slog.Debug("lock transaction recovery completed", "duration_ms", time.Since(started).Milliseconds())
	return nil
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

func canonicalizeFuturePath(name string) (string, error) {
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

func prepareExactLocks(ctx context.Context, repoRoot, localDir string, mode lockMode) (catalog.LoadOption, int, error) {
	option, changed, _, err := prepareRuntimeLocks(ctx, repoRoot, localDir, mode)
	return option, changed, err
}

func reconcileCommandDependencies(ctx context.Context, repoRoot, localDir string, ignoreBlocked bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
	var warnings []error
	option, _, lease, err := prepareRuntimeLocks(ctx, repoRoot, localDir, lockMode{ignoreBlocked: ignoreBlocked, stagedConfigEdits: edits, warningSink: &warnings})
	if err != nil {
		return pipeline.DependencyLockView{}, err
	}
	return pipeline.DependencyLockView{CatalogOption: option, ExactLockLease: lease, Warnings: warnings}, nil
}

type configTransactionSnapshot struct {
	path         string
	generation   dependencytxn.Generation
	expectedMode os.FileMode
	bytes        []byte
	mode         os.FileMode
}

func snapshotEffectiveConfigs(ec config.EffectiveConfig, overlays map[string][]byte) (map[config.Source]configTransactionSnapshot, error) {
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
		generation, expectedMode, err := dependencytxn.ReadGenerationAndMode(participantPath)
		if err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		var raw []byte
		if generation.Exists {
			raw, err = os.ReadFile(participantPath)
			if err != nil {
				return nil, fmt.Errorf("read configuration transaction participant %s: %w", participantPath, err)
			}
			mode = expectedMode
		} else if _, staged := overlays[participantPath]; !staged {
			return nil, fmt.Errorf("configuration layer disappeared while loading: %s", layer.Path)
		}
		snapshots[layer.Source] = configTransactionSnapshot{path: participantPath, generation: generation, expectedMode: expectedMode, bytes: raw, mode: mode}
	}
	return snapshots, nil
}

func sameConfigSnapshots(left, right map[config.Source]configTransactionSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for source, a := range left {
		b, ok := right[source]
		if !ok || a.path != b.path || a.generation != b.generation {
			return false
		}
	}
	return true
}

func loadStableEffective(ctx context.Context, options config.LoadEffectiveOptions) (config.EffectiveConfig, map[config.Source]configTransactionSnapshot, error) {
	probe := config.LoadEffective(ctx, options)
	before, err := snapshotEffectiveConfigs(probe, options.FileOverlays)
	if err != nil {
		return probe, nil, err
	}
	fresh := config.LoadEffective(ctx, options)
	after, err := snapshotEffectiveConfigs(fresh, options.FileOverlays)
	if err != nil {
		return fresh, nil, err
	}
	if !sameConfigSnapshots(before, after) {
		return fresh, nil, fmt.Errorf("%w: configuration changed while loading", dependencytxn.ErrGenerationConflict)
	}
	return fresh, after, nil
}

type exactLockLeaseFunc func(context.Context) (func(), error)

func prepareRuntimeLocks(ctx context.Context, repoRoot, localDir string, mode lockMode) (resultOption catalog.LoadOption, changed int, resultLease exactLockLeaseFunc, resultErr error) {
	started := time.Now()
	modeName := lockModeName(mode)
	scopes := selectedLockScopes(mode)
	slog.Info("lock lifecycle started", "mode", modeName, "scopes", scopes, "offline", mode.offline, "check", mode.check)
	defer func() {
		attrs := []any{"mode", modeName, "scopes", scopes, "duration_ms", time.Since(started).Milliseconds(), "changed", changed}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("lock lifecycle failed", attrs...)
			return
		}
		slog.Info("lock lifecycle completed", attrs...)
	}()

	stateDir, err := dependencyStateDir()
	if err != nil {
		return nil, 0, nil, err
	}
	if !mode.check {
		if err := recoverDependencyTransactions(ctx); err != nil {
			return nil, 0, nil, err
		}
	}
	overlays := make(map[string][]byte, len(mode.stagedConfigEdits))
	stagedEdits := make(map[string]pipeline.StagedConfigEdit, len(mode.stagedConfigEdits))
	for _, edit := range mode.stagedConfigEdits {
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
	loadOptions := config.LoadEffectiveOptions{
		RepoRoot: repoRoot, LocalConfigDir: localDir,
		FileOverlays: overlays,
	}
	ec, configSnapshots, err := loadStableEffective(ctx, loadOptions)
	if err != nil {
		return nil, 0, nil, err
	}
	consumed := make(map[string]int, len(stagedEdits))
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
	if len(ec.Errors) > 0 && (!mode.ignoreBlocked || !config.OnlyRegistryURLBlockedErrors(ec.Errors)) {
		return nil, 0, nil, errors.Join(ec.Errors...)
	}
	resolvedTargets, err := resolveUpgradeTargets(ec, mode)
	if err != nil {
		return nil, 0, nil, err
	}
	mode.upgradeSelection = resolvedTargets.selection
	if mode.upgradeResult != nil {
		*mode.upgradeResult = resolvedTargets
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
	for _, layer := range ec.PresentLayers() {
		scope := lockScopeForSource(layer.Source)
		entries := layerRegistries(layer)
		remote := remoteEntries(entries)
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
		selected := mode.selects == nil || mode.selects[scope]
		current, generation, exists, loadErr := loadLayerLock(lockPath, scope)
		if loadErr != nil {
			if mode.upgrade && !selected {
				continue
			}
			return nil, changed, nil, loadErr
		}
		if len(remote) > 0 {
			lockExpectations[lockPath] = generation
			_, currentMode, stateErr := dependencytxn.ReadGenerationAndMode(lockPath)
			if stateErr != nil {
				return nil, changed, nil, stateErr
			}
			lockModes[lockPath] = currentMode
		}
		if len(remote) == 0 && !exists {
			continue
		}
		if !selected && !mode.upgrade {
			continue
		}
		if mode.upgrade && !selected && !mode.reconcileUnselected {
			if !exists || current == nil || !unselectedLayerIsFresh(scope, remote, *current) {
				continue
			}
			snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: *current})
			continue
		}
		implicitOrg := scope == lockfile.ScopeOrganization && !mode.explicitOrganization
		if !exists && mode.locked {
			return nil, changed, nil, &lockLifecycleError{Code: "lock.missing", Scope: scope, Path: lockPath, Err: os.ErrNotExist}
		}
		if mode.check {
			fresh := exists && current != nil && unselectedLayerIsFresh(scope, remote, *current)
			if !fresh {
				if mode.locked {
					return nil, changed, nil, &lockLifecycleError{Code: "lock.stale", Scope: scope, Path: lockPath, Err: errors.New("lock does not match configuration")}
				}
				changed++
				continue
			}
			snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: *current})
			continue
		}

		operation := lockfile.OperationSync
		if mode.upgrade && selected && !implicitOrg {
			operation = lockfile.OperationUpgrade
		}
		gitDesired, gitFresh := reconcileLayerDocument(ctx, scope, remote, current, operation, mode.upgradeSelection, mode.offline)
		if gitDesired.err != nil {
			return nil, changed, nil, gitDesired.err
		}
		doc := gitDesired.doc
		fresh := exists && gitFresh
		if !fresh {
			if mode.locked || implicitOrg && exists || !selected && !mode.upgrade {
				return nil, changed, nil, &lockLifecycleError{Code: "lock.stale", Scope: scope, Path: lockPath, Err: errors.New("lock does not match configuration")}
			}
			if !implicitOrg {
				changed++
				data, err := lockfile.Encode(doc)
				if err != nil {
					return nil, changed, nil, err
				}
				lockDesired[lockPath] = data
				desiredFiles = append(desiredFiles, dependencytxn.DesiredFile{Path: lockPath, Expected: generation, ExpectedMode: lockModes[lockPath], Bytes: data, Mode: 0o644})
			}
		}
		layerChangedCount := 0
		if !fresh {
			layerChangedCount = 1
		}
		slog.Debug("lock layer reconciled", "scope", scope, "operation", operation, "retained", gitDesired.stats.retained, "acquired", gitDesired.stats.acquired, "changed", layerChangedCount)
		snapshots = append(snapshots, snapshotJob{source: layer.Source, entries: remote, doc: doc})
	}
	for _, job := range snapshots {
		if err := addLayerSnapshots(ctx, &set, job.source, job.entries, job.doc, mode.offline, mode.check, mode.warningSink); err != nil {
			return nil, changed, nil, err
		}
	}
	if !mode.check {
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
	if !mode.check && len(desiredFiles) > 0 {
		for _, layer := range ec.PresentLayers() {
			if !layer.Source.IsWritable() && !mode.explicitOrganization {
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
			if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
				return nil, changed, nil, fmt.Errorf("create dependency destination directory: %w", err)
			}
		}
		// Exact-cache GC re-reads lock documents while holding the namespace
		// exclusive lease. Coordinate publication with its shared counterpart so
		// a commit cannot complete between that re-read and deletion.
		releaseExactNamespace, err := cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
		if err != nil {
			return nil, changed, nil, fmt.Errorf("acquire exact cache publication lease: %w", err)
		}
		commitStarted := time.Now()
		commitResult, err := commitDependencyTransaction(ctx, stateDir, desiredFiles)
		releaseExactNamespace()
		if err != nil {
			slog.Error("lock transaction commit failed", "files", len(desiredFiles), "duration_ms", time.Since(commitStarted).Milliseconds(), "error", err)
			return nil, changed, nil, fmt.Errorf("commit dependency config and locks: %w", err)
		}
		slog.Info("lock transaction commit completed", "files", len(desiredFiles), "changed", len(commitResult.Changed), "duration_ms", time.Since(commitStarted).Milliseconds())
	} else {
		slog.Debug("lock transaction commit skipped", "check", mode.check, "files", len(desiredFiles))
	}
	if mode.explicitOrganization && organizationRequiresLock && !mode.check {
		verificationStarted := time.Now()
		if err := config.ValidateOrganizationTrustUnit(ec.Organization.Path, true, false); err != nil {
			slog.Error("organization lock verification failed", "duration_ms", time.Since(verificationStarted).Milliseconds(), "error", err)
			return nil, changed, nil, fmt.Errorf("verify resulting organization config and lock: %w", err)
		}
		slog.Info("organization lock verification completed", "duration_ms", time.Since(verificationStarted).Milliseconds())
	}
	for path, data := range lockDesired {
		lockExpectations[path] = dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}
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

	option := catalog.WithExactSnapshots(func(entry config.RegistryEntry) (string, string, error) {
		if err := validateExactLocks(); err != nil {
			return "", "", err
		}
		canonical, err := canonicalRegistryURL(entry.URL)
		if err != nil {
			return "", "", err
		}
		key := sourceLockKey(entry.Source, canonical, entry.Ref)
		locked, ok := set.bySource[key]
		if !ok {
			candidates := set.byIdentity[gitLockIdentity(canonical, entry.Ref)]
			if len(candidates) != 1 {
				return "", "", &lockLifecycleError{Code: "lock.lookup", Err: fmt.Errorf("no unambiguous lock for %s@%s", canonical, entry.Ref)}
			}
			locked = candidates[0]
		}
		catalogKey := canonical + "@" + locked.commit.String() + ":" + entry.Path
		return locked.snapshot, catalogKey, nil
	})
	hasExactConsumers := len(set.bySource) > 0
	resultLease = func(ctx context.Context) (func(), error) {
		if !hasExactConsumers {
			return func() {}, nil
		}
		var (
			release func()
			err     error
		)
		if mode.check {
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
	return option, changed, resultLease, nil
}

type reconciledDocument struct {
	doc   lockfile.Document
	stats lockReconcileStats
	err   error
}

func reconcileLayerDocument(ctx context.Context, scope lockfile.Scope, entries []config.RegistryEntry, current *lockfile.Document, operation lockfile.Operation, selection lockfile.UpgradeSelection, offline bool) (reconciledDocument, bool) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: scope}
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
			if projected, matches := projectLockedRoots(locked.Roots, group.entries); matches {
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
			return reconciledDocument{stats: stats, err: &lockLifecycleError{Code: "lock.offline-resolution", Scope: scope, Err: errors.New("offline mode cannot resolve a stale lock")}}, false
		}
		registry, err := acquireLockedRegistry(ctx, group.canonical, group.fetchURL, group.ref, group.entries)
		if err != nil {
			return reconciledDocument{stats: stats, err: err}, false
		}
		doc.Registries = append(doc.Registries, registry)
		stats.acquired++
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
		canonical, _ := canonicalRegistryURL(entry.URL)
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
		root := lockfile.GitRoot{Path: rootPath, SourceHarness: built.SourceHarness, RevisionSchema: "v1", RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
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
		projected, matches := projectLockedRoots(registry.Roots, group.entries)
		if !matches || len(projected) != len(registry.Roots) {
			return false
		}
	}
	return true
}

func projectLockedRoots(roots []lockfile.GitRoot, entries []config.RegistryEntry) ([]lockfile.GitRoot, bool) {
	selected := make([]bool, len(roots))
	for _, entry := range entries {
		rootPath := entry.Path
		if rootPath == "" {
			rootPath = "."
		}
		match := -1
		for i, root := range roots {
			if root.Path != rootPath || entry.SourceHarness != "" && root.SourceHarness != entry.SourceHarness {
				continue
			}
			match = i
			if entry.SourceHarness != "" || selected[i] {
				break
			}
		}
		if match < 0 {
			return nil, false
		}
		selected[match] = true
	}
	projected := make([]lockfile.GitRoot, 0, len(roots))
	for i, root := range roots {
		if selected[i] {
			projected = append(projected, root)
		}
	}
	return projected, true
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
		canonical, err := canonicalRegistryURL(entry.URL)
		if err != nil {
			return err
		}
		fetchURLs[gitLockIdentity(canonical, entry.Ref)] = entry.URL
	}
	for _, registry := range doc.Registries {
		cacheOID, err := cache.ParseGitOID(registry.Commit.String())
		if err != nil {
			return err
		}
		if !offline && !readOnly {
			if _, err := cache.AcquireExactGitWithContext(ctx, registry.URL, fetchURLs[gitLockIdentity(registry.URL, registry.Ref)], cacheOID); err != nil {
				return &lockLifecycleError{Code: "lock.snapshot-unavailable", Err: err}
			}
		}
		var snapshot string
		err = cache.WithExactGitReadOnly(ctx, registry.URL, cacheOID, func(path string) error {
			snapshot = path
			for _, root := range registry.Roots {
				warnings, verifyErr := verifyLockedGitRoot(path, root)
				if verifyErr != nil {
					return &lockLifecycleError{Code: "lock.manifest-mismatch", Err: fmt.Errorf("verify %s@%s root %q: %w", registry.URL, registry.Commit.String(), root.Path, verifyErr)}
				}
				if warningSink != nil {
					for _, warning := range warnings {
						*warningSink = append(*warningSink, warning)
					}
				}
				verified++
			}
			return nil
		})
		if err != nil {
			var lifecycleErr *lockLifecycleError
			if errors.As(err, &lifecycleErr) {
				return err
			}
			code := "lock.snapshot-unavailable"
			if offline {
				code = "lock.offline-snapshot-missing"
			}
			return &lockLifecycleError{Code: code, Err: err}
		}
		locked := lockedGit{canonical: registry.URL, fetchURL: fetchURLs[gitLockIdentity(registry.URL, registry.Ref)], ref: registry.Ref, commit: registry.Commit, snapshot: snapshot}
		set.bySource[sourceLockKey(source, registry.URL, registry.Ref)] = locked
		identity := gitLockIdentity(registry.URL, registry.Ref)
		set.byIdentity[identity] = append(set.byIdentity[identity], locked)
	}
	return nil
}

func verifyLockedGitRoot(snapshot string, root lockfile.GitRoot) ([]sourcerevision.ResourceWarning, error) {
	return sourcerevision.VerifyLockedGitRoot(snapshot, root)
}

func loadLayerLock(path string, scope lockfile.Scope) (*lockfile.Document, dependencytxn.Generation, bool, error) {
	generation, err := dependencytxn.ReadGeneration(path)
	if err != nil {
		return nil, generation, false, err
	}
	if !generation.Exists {
		return nil, generation, false, nil
	}
	data, err := lockfile.ReadBytes(path)
	if err != nil {
		return nil, generation, true, err
	}
	doc, err := lockfile.Decode(data)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	if err != nil {
		return nil, generation, true, &lockLifecycleError{Code: "lock.invalid", Scope: scope, Path: path, Err: err}
	}
	return &doc, generation, true, nil
}

func layerRegistries(layer config.LayerResult) []config.RegistryEntry {
	if layer.Config != nil {
		return layer.Config.Registries
	}
	if layer.Local != nil {
		return layer.Local.Registries
	}
	return nil
}

func remoteEntries(entries []config.RegistryEntry) []config.RegistryEntry {
	out := make([]config.RegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsLocal() {
			out = append(out, entry)
		}
	}
	return out
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

func canonicalRegistryURL(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		raw = (&url.URL{Scheme: "file", Path: filepath.ToSlash(raw)}).String()
	}
	return lockfile.CanonicalGitURL(raw)
}

func sourceLockKey(source config.Source, canonical, ref string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", source, canonical, ref)
}
func gitLockIdentity(canonical, ref string) string { return canonical + "\x00" + ref }
