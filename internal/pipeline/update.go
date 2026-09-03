package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var dottedVersionRefPattern = regexp.MustCompile(`^(.*?)(\d+)$`)

// RefBump records a single registry ref update.
type RefBump struct {
	URL    string
	OldRef string
	NewRef string
}

// UpdateTarget carries direct Git selector intent to both ref bumping and lock
// upgrade, so --bump cannot widen a targeted update to unrelated registries.
type UpdateTarget struct {
	Registry string
	Ref      string
	Path     string
	Kind     string
	Names    []string
}

// UpdateOptions holds the arguments for an update operation.
type UpdateOptions struct {
	RepoRoot string
	// MaterializeRoot overrides where harness files are written. When empty,
	// update materializes at RepoRoot unless a monorepo local config directory
	// is supplied.
	MaterializeRoot string
	// LocalConfigDir is the directory containing the local config layer. It is
	// also the base for resolving relative paths from that layer.
	LocalConfigDir      string
	Bump                bool
	BumpScopes          map[lockfile.Scope]bool
	BumpTarget          *UpdateTarget
	MarketplaceResolver *marketplace.Resolver
	DependencyReconcile UpdateDependencyReconciler
}

type UpdateDependencyReconciler func(context.Context, string, string, []RefBump, []StagedConfigEdit) (DependencyLockView, error)

// MarketplacePinUpdate records the decision made for one configured
// Marketplace pin. OldPin is always the pin observed during planning; NewPin
// is populated for a prepared update or remains zero for a skipped pin.
type MarketplacePinUpdate struct {
	Entry         config.MarketplaceEntry
	OldPin        config.MarketplaceResourcePin
	NewPin        config.MarketplaceResourcePin
	ArchiveDigest string
	Source        config.Source
	Required      bool
	Updated       bool
	Skipped       bool
	Reason        string
}

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	UpdatedRegistries      int
	Bumps                  []RefBump
	MarketplacePins        []MarketplacePinUpdate
	UpdatedMarketplacePins int
	SkippedMarketplacePins int
	Warnings               []error
}

type marketplaceUpdatePlan struct {
	MarketplacePinUpdate
	ConfigPath       string
	ReplacementPins  []config.MarketplaceResourcePin
	CollectionUpdate bool
}

type configMutation struct {
	path     string
	snapshot *config.FileSnapshot
	commit   config.ConfigFileCommit
}

// Update refreshes selected dependency locks and syncs enabled resources.
func Update(ctx context.Context, opts UpdateOptions) (result *UpdateResult, retErr error) {
	tel := telemetry.TelemetryFromContext(ctx)
	var updateSpan telemetry.Span
	if tel != nil {
		ctx, updateSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.update", updateOptionAttrs(opts))
	}
	defer func() {
		if updateSpan != (telemetry.Span{}) {
			updateSpan.SetAttributes(updateResultAttrs(result, retErr))
			updateSpan.End()
		}
	}()

	repoRoot := opts.RepoRoot
	slog.Info("update start", "repo_root", repoRoot, "bump", opts.Bump)

	// Load the effective configuration to obtain the authoritative allowlists
	// and validate all configured endpoints before network access.
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: opts.LocalConfigDir})
	if len(ec.Errors) > 0 {
		slog.Error("update aborted on effective config errors", "error_count", len(ec.Errors))
		return nil, errors.Join(ec.Errors...)
	}
	if opts.DependencyReconcile == nil {
		return nil, errors.New("update requires dependency lock reconciliation")
	}
	staging, err := newConfigStagingManager()
	if err != nil {
		return nil, err
	}
	defer closeConfigStaging(staging, "update", &retErr)

	updatedRegistries := 0
	if ec.Merged != nil {
		updatedRegistries = len(ec.Merged.Registries)
	}
	var bumps []RefBump
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Bump {
		for _, layer := range ec.PresentLayers() {
			scope := updateScopeForSource(layer.Source)
			if !bumpScopeSelected(opts.BumpScopes, scope) {
				continue
			}
			registries := layerRegistriesForUpdate(layer)
			if len(registries) == 0 {
				continue
			}
			if err := config.ValidateRegistryURLs(registries, ec.Allowed); err != nil {
				slog.Error("update blocked by allowed_registries", "scope", scope, "error", err)
				return nil, err
			}
			configPath, err := staging.Path(layer.Path)
			if err != nil {
				return nil, err
			}
			layerBumps, err := bumpRepoRegistryRefs(ctx, configPath, &config.Config{Registries: registries}, opts.BumpTarget)
			if err != nil {
				return nil, err
			}
			bumps = append(bumps, layerBumps...)
		}
	}

	syncOpts := silentSyncOptsForUpdate(opts)
	syncOpts = append(syncOpts, withConfigStaging(staging), withDependencyReconciler(func(ctx context.Context, repoRoot, localDir string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		return opts.DependencyReconcile(ctx, repoRoot, localDir, bumps, edits)
	}))
	warnings, err := silentSyncCollect(ctx, repoRoot, syncOpts...)
	if err != nil {
		var committed *DependencyCommittedSyncError
		if errors.As(err, &committed) {
			slog.Error("update materialization failed after dependency commit", "repo_root", repoRoot, "error", err)
			return nil, err
		}
		slog.Error("update dependency reconciliation failed", "repo_root", repoRoot, "error", err)
		return nil, err
	}

	result = &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps, Warnings: warnings}
	slog.Info("update end", "updated_registries", updatedRegistries, "bumps", len(bumps))
	return result, nil
}

func layerRegistriesForUpdate(layer config.LayerResult) []config.RegistryEntry {
	if layer.Config != nil {
		return layer.Config.Registries
	}
	if layer.Local != nil {
		return layer.Local.Registries
	}
	return nil
}

func updateScopeForSource(source config.Source) lockfile.Scope {
	switch source {
	case config.UserPersonal:
		return lockfile.ScopeUser
	case config.RepoCommitted:
		return lockfile.ScopeRepo
	default:
		return lockfile.ScopeLocal
	}
}

func bumpScopeSelected(scopes map[lockfile.Scope]bool, scope lockfile.Scope) bool {
	if scopes == nil {
		return scope == lockfile.ScopeRepo
	}
	return scopes[scope]
}

func silentSyncOptsForUpdate(opts UpdateOptions) []silentSyncOpt {
	materializeRoot := opts.MaterializeRoot
	if materializeRoot == "" && opts.LocalConfigDir != "" && opts.LocalConfigDir != opts.RepoRoot {
		materializeRoot = opts.LocalConfigDir
	}
	return silentSyncOptsForPaths(opts.RepoRoot, materializeRoot, opts.LocalConfigDir)
}

func planMarketplaceUpdates(ctx context.Context, opts UpdateOptions, ec *config.EffectiveConfig) ([]marketplaceUpdatePlan, error) {
	if ec == nil || ec.Merged == nil || len(ec.Merged.Marketplaces) == 0 {
		return nil, nil
	}
	resolverMP := opts.MarketplaceResolver
	if resolverMP == nil {
		resolverMP = marketplace.NewResolver()
		resolverMP.AllowedRegistries = ec.Allowed
	}
	targets := hccTargetsFromStrings(ec.TargetHarnesses)
	plans := make([]marketplaceUpdatePlan, 0, len(ec.Merged.Marketplaces))
	collectionLeaves := make(map[string]struct{})
	for _, entry := range ec.Merged.Marketplaces {
		configPath := ""
		if entry.Source.IsWritable() {
			var err error
			configPath, err = marketplaceConfigPath(opts, ec, entry.Source)
			if err != nil {
				return nil, err
			}
		}
		for _, pin := range entry.Resources {
			if pin.Kind != "collection" {
				continue
			}
			item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Source == config.Organization, Skipped: true}
			if entry.Source == config.Organization {
				item.Reason = "organization pin is required and managed externally"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			expanded := false
			for _, leaf := range entry.Resources {
				for _, selector := range leaf.SelectedBy.List() {
					if selector.SourceID == pin.SourceID && selector.Name == pin.Name && selector.Revision == pin.Revision {
						expanded = true
						collectionLeaves[marketplaceUpdateKey(entry, leaf)] = struct{}{}
					}
				}
			}
			if !expanded {
				item.Reason = "collection pin updates are not supported"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			source, latest, err := resolverMP.LatestRevision(ctx, entry, pin)
			if err != nil {
				return nil, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, pin.Name, err)
			}
			if string(latest.ContentDigest) == pin.Digest {
				item.Reason = "content digest unchanged"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if len(targets) == 0 {
				return nil, &TargetHarnessSetMissingError{}
			}
			locator := marketplace.MarketplaceLocator{Source: string(source.ID), Kind: pin.Kind, Name: pin.Name, Revision: string(latest.ContentDigest), Digest: string(latest.ContentDigest)}
			prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, targets)
			if err != nil {
				return nil, fmt.Errorf("prepare Marketplace collection %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, err)
			}
			for _, leaf := range prepared.CollectionMembers {
				if err := validatePreparedMarketplaceUpdate(leaf); err != nil {
					return nil, fmt.Errorf("validate Marketplace collection leaf: %w", err)
				}
			}
			newPins := marketplaceInstallPins(prepared)
			item.Skipped = false
			item.NewPin = prepared.Pin
			item.ArchiveDigest = prepared.ArchiveDigest
			plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath, ReplacementPins: newPins, CollectionUpdate: true})
		}
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" {
				continue
			}
			if entry.Source == config.Organization {
				item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: true, Skipped: true, Reason: "organization pin is required and managed externally"}
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly && len(targets) == 0 {
				item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true, Reason: "index-only pin updates are not supported"}
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if _, selected := collectionLeaves[marketplaceUpdateKey(entry, pin)]; selected {
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true, Reason: "leaf is pinned by a collection revision"}, ConfigPath: configPath})
				continue
			}
			source, latest, err := resolverMP.LatestRevision(ctx, entry, pin)
			if err != nil {
				return nil, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, pin.Name, err)
			}
			item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true}
			if source.Acquisition != "" && string(source.Acquisition) != config.MarketplaceAcquisitionMirror && string(source.Acquisition) != config.MarketplaceAcquisitionIndexOnly {
				item.Reason = "source uses unsupported acquisition"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if string(latest.ContentDigest) == pin.Digest && !marketplace.IndexOnlyOccurrenceChanged(pin, latest) {
				item.Reason = "content digest unchanged"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if len(targets) == 0 {
				return nil, &TargetHarnessSetMissingError{}
			}
			locator := marketplace.MarketplaceLocator{Source: string(source.ID), Kind: pin.Kind, Name: pin.Name, Revision: string(latest.ContentDigest), Digest: string(latest.ContentDigest)}
			prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, targets)
			if err != nil {
				return nil, fmt.Errorf("prepare Marketplace pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, err)
			}
			if err := validateMarketplaceUpdateArtifact(prepared); err != nil {
				return nil, fmt.Errorf("validate Marketplace pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, err)
			}
			prepared.Pin.InstalledName = pin.InstalledName
			prepared.Pin.Direct = pin.Direct
			prepared.Pin.SelectedBy = pin.SelectedBy
			item.Skipped = false
			item.NewPin = prepared.Pin
			item.ArchiveDigest = prepared.ArchiveDigest
			plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
		}
	}
	return plans, nil
}

func marketplaceUpdateKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	return entry.URL + "\x00" + entry.InstanceID + "\x00" + pin.SourceID + "\x00" + pin.Kind + "\x00" + pin.Name
}

func validatePreparedMarketplaceUpdate(plan *marketplace.PreparedInstall) error {
	if plan == nil {
		return fmt.Errorf("prepared Marketplace leaf is nil")
	}
	return validateMarketplaceUpdateArtifact(plan)
}

func marketplaceConfigPath(opts UpdateOptions, ec *config.EffectiveConfig, source config.Source) (string, error) {
	switch source {
	case config.UserPersonal:
		if ec.User.Path != "" {
			return ec.User.Path, nil
		}
		return config.GlobalConfigPath()
	case config.RepoCommitted:
		return config.ProjectConfigPath(opts.RepoRoot), nil
	case config.RepoPersonal:
		dir := opts.LocalConfigDir
		if dir == "" {
			dir = config.FindLocalConfigDir()
		}
		return config.LocalConfigPath(dir), nil
	default:
		return "", fmt.Errorf("marketplace source %s is not writable", source)
	}
}

func validateMarketplaceUpdateArtifact(plan *marketplace.PreparedInstall) error {
	if plan == nil || plan.Artifact.Manifest.ContentDigest != plan.Pin.Digest {
		return fmt.Errorf("verified artifact content digest does not match prepared pin")
	}
	switch plan.Revision.Assessment.Decision {
	case store.AssessmentDecisionAllow, store.AssessmentDecisionWarn:
		return nil
	case store.AssessmentDecisionReject:
		return fmt.Errorf("latest revision is rejected by policy")
	default:
		return fmt.Errorf("latest revision has unknown assessment decision %q", marketplace.QuoteTerminal(string(plan.Revision.Assessment.Decision)))
	}
}

func rollbackConfigMutations(mutations []configMutation, cause error) error {
	var rollbackErrs []error
	for i := len(mutations) - 1; i >= 0; i-- {
		mutation := mutations[i]
		if err := mutation.snapshot.RestoreIfCurrent(mutation.commit); err != nil {
			rollbackErr := fmt.Errorf("rollback config %q: %w", mutation.path, err)
			slog.Error("Marketplace config rollback failed", "config_path", mutation.path, "error", rollbackErr)
			rollbackErrs = append(rollbackErrs, rollbackErr)
		}
	}
	if len(rollbackErrs) == 0 {
		return cause
	}
	return errors.Join(append([]error{cause}, rollbackErrs...)...)
}

func bumpRepoRegistryRefs(ctx context.Context, configPath string, repoCfg *config.Config, target *UpdateTarget) (bumps []RefBump, retErr error) {
	if repoCfg == nil || len(repoCfg.Registries) == 0 {
		return nil, nil
	}
	wantedURL := ""
	if target != nil && strings.TrimSpace(target.Registry) != "" {
		var err error
		wantedURL, err = canonicalUpdateRegistryURL(target.Registry)
		if err != nil {
			return nil, fmt.Errorf("canonicalize update registry target: %w", err)
		}
	}

	tel := telemetry.TelemetryFromContext(ctx)
	var bumpSpan telemetry.Span
	if tel != nil {
		_, bumpSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.update_refs", updateRefsAttrs(repoCfg))
	}
	defer func() {
		if bumpSpan != (telemetry.Span{}) {
			bumpSpan.SetAttributes(updateRefsResultAttrs(bumps, retErr))
			bumpSpan.End()
		}
	}()

	bumps = make([]RefBump, 0)
	planned := make(map[string]string)

	for _, entry := range repoCfg.Registries {
		if entry.IsLocal() || !updateTargetMatchesEntry(entry, target, wantedURL) {
			continue
		}

		cachePath, err := cache.CloneOrPull(entry.URL, entry.Ref)
		if err != nil {
			return nil, err
		}

		newRef, ok, err := discoverTagBump(cachePath, entry.Ref)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		key := entry.URL + "\x00" + entry.Ref
		if existing, exists := planned[key]; exists {
			if existing != newRef {
				return nil, fmt.Errorf("conflicting bump targets for %s@%s: %s vs %s", entry.URL, entry.Ref, existing, newRef)
			}
			continue
		}

		planned[key] = newRef
		bumps = append(bumps, RefBump{
			URL:    entry.URL,
			OldRef: entry.Ref,
			NewRef: newRef,
		})
	}

	for _, bump := range bumps {
		if _, err := config.UpdateRegistryRefInConfig(configPath, bump.URL, bump.OldRef, bump.NewRef); err != nil {
			return nil, err
		}
	}

	return bumps, nil
}

func canonicalUpdateRegistryURL(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		raw = (&url.URL{Scheme: "file", Path: filepath.ToSlash(raw)}).String()
	}
	return lockfile.CanonicalGitURL(strings.TrimSpace(raw))
}

func updateTargetMatchesEntry(entry config.RegistryEntry, target *UpdateTarget, wantedURL string) bool {
	if target == nil {
		return true
	}
	if wantedURL != "" {
		entryURL, err := canonicalUpdateRegistryURL(entry.URL)
		if err != nil || entryURL != wantedURL {
			return false
		}
	}
	if target.Ref != "" && target.Ref != entry.Ref {
		return false
	}
	if target.Path != "" && normalizeUpdateRoot(target.Path) != normalizeUpdateRoot(entry.Path) {
		return false
	}
	return true
}

func normalizeUpdateRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "."
	}
	return pathpkg.Clean(root)
}

func discoverTagBump(repoDir, currentRef string) (string, bool, error) {
	currentRef = strings.TrimSpace(currentRef)
	matches := dottedVersionRefPattern.FindStringSubmatch(currentRef)
	if len(matches) != 3 {
		return "", false, nil
	}

	currentNumber, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", false, fmt.Errorf("parse current ref %q: %w", currentRef, err)
	}

	tags, err := listGitTags(repoDir, matches[1]+"*")
	if err != nil {
		return "", false, err
	}

	bestRef := ""
	bestNumber := currentNumber
	for _, tag := range tags {
		suffix := strings.TrimPrefix(tag, matches[1])
		if suffix == "" || strings.ContainsFunc(suffix, func(r rune) bool { return r < '0' || r > '9' }) {
			continue
		}

		tagNumber, err := strconv.Atoi(suffix)
		if err != nil {
			return "", false, fmt.Errorf("parse tag %q: %w", tag, err)
		}

		if tagNumber > bestNumber || (tagNumber == bestNumber && tag > bestRef) {
			bestRef = tag
			bestNumber = tagNumber
		}
	}

	if bestRef == "" {
		return "", false, nil
	}

	return bestRef, true, nil
}

func listGitTags(repoDir, pattern string) ([]string, error) {
	args := []string{"tag", "-l"}
	if strings.TrimSpace(pattern) != "" {
		args = append(args, pattern)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), repoDir, err)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	return strings.Split(trimmed, "\n"), nil
}
