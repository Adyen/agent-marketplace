package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

var dottedVersionRefPattern = regexp.MustCompile(`^(.*?)(\d+)$`)

// RefBump records a single registry ref update.
type RefBump struct {
	URL    string
	OldRef string
	NewRef string
}

// UpdateOptions holds the arguments for an update operation.
type UpdateOptions struct {
	RepoRoot            string
	MaterializeRoot     string
	LocalConfigDir      string
	Bump                bool
	BumpScopes          map[lockfile.Scope]bool
	BumpTarget          *UpdateTarget
	DependencyReconcile UpdateDependencyReconciler
	MarketplaceResolver *marketplace.Resolver
}

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

type marketplaceUpdatePlan struct {
	MarketplacePinUpdate
	ConfigPath string
}

type UpdateTarget struct {
	Registry string
	Ref      string
	Path     string
	Kind     string
	Names    []string
}

type UpdateDependencyReconciler func(context.Context, string, string, []RefBump, []StagedConfigEdit) (DependencyLockView, error)

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	UpdatedRegistries      int
	Bumps                  []RefBump
	MarketplacePins        []MarketplacePinUpdate
	UpdatedMarketplacePins int
	SkippedMarketplacePins int
}

// Update refreshes selected Git dependency locks and syncs enabled resources.
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
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: opts.LocalConfigDir})
	if len(ec.Errors) > 0 {
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
	if updatedRegistries > 0 && len(ec.TargetHarnesses) == 0 {
		return nil, &TargetHarnessSetMissingError{}
	}
	plans, err := planMarketplaceUpdates(ctx, opts, &ec)
	if err != nil {
		slog.Error("update aborted while preparing Marketplace pins", "error", err)
		return nil, err
	}
	for i := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		plan := &plans[i]
		if plan.Skipped {
			continue
		}
		configPath, err := staging.Path(plan.ConfigPath)
		if err != nil {
			return nil, err
		}
		if err := config.UpdateMarketplacePinInConfig(configPath, plan.Entry.URL, plan.Entry.InstanceID, plan.OldPin, plan.NewPin); err != nil {
			return nil, fmt.Errorf("update Marketplace pin %s/%s/%s: %w", plan.OldPin.SourceID, plan.OldPin.Kind, plan.OldPin.Name, err)
		}
		plan.Updated = true
	}
	var bumps []RefBump
	if opts.Bump {
		for _, layer := range ec.PresentLayers() {
			if !layer.Source.IsWritable() {
				continue
			}
			scope := lockfile.Scope(layer.Source.LockScope())
			if !bumpScopeSelected(opts.BumpScopes, scope) {
				continue
			}
			registries := layerRegistriesForUpdate(layer)
			if len(registries) == 0 {
				continue
			}
			if err := config.ValidateRegistryURLs(registries, ec.Allowed); err != nil {
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

	syncOpts := silentSyncOptsForPaths(repoRoot, opts.MaterializeRoot, opts.LocalConfigDir)
	syncOpts = append(syncOpts, withConfigStaging(staging), withDependencyReconciler(func(ctx context.Context, repoRoot, localDir string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		return opts.DependencyReconcile(ctx, repoRoot, localDir, bumps, edits)
	}))
	if _, err := silentSyncCollect(ctx, repoRoot, syncOpts...); err != nil {
		if hasUnsupportedMarketplacePlan(plans) && isUnsupportedMarketplaceSyncError(err) {
			err = fmt.Errorf("sync was required, but unsupported Marketplace pins prevented synchronization; index-only and collection updates are not implemented — remove or disable those pins before retrying: %w", err)
		}
		return nil, err
	}
	result = &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps}
	for _, plan := range plans {
		result.MarketplacePins = append(result.MarketplacePins, plan.MarketplacePinUpdate)
		if plan.Updated {
			result.UpdatedMarketplacePins++
		} else if plan.Skipped {
			result.SkippedMarketplacePins++
		}
	}
	slog.Info("update end", "updated_registries", updatedRegistries, "bumps", len(bumps), "marketplace_updated", result.UpdatedMarketplacePins, "marketplace_skipped", result.SkippedMarketplacePins)
	return result, nil
}

func planMarketplaceUpdates(ctx context.Context, opts UpdateOptions, ec *config.EffectiveConfig) ([]marketplaceUpdatePlan, error) {
	if ec == nil || ec.Merged == nil || len(ec.Merged.Marketplaces) == 0 {
		return nil, nil
	}
	if len(ec.TargetHarnesses) == 0 {
		return nil, &TargetHarnessSetMissingError{}
	}

	resolverMP := opts.MarketplaceResolver
	if resolverMP == nil {
		resolverMP = marketplace.NewResolver()
	}
	targets := hccTargetsFromStrings(ec.TargetHarnesses)
	plans := make([]marketplaceUpdatePlan, 0, len(ec.Merged.Marketplaces))
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
			item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Source == config.Organization, Skipped: true}
			switch {
			case entry.Source == config.Organization:
				item.Reason = "organization pin is required and managed externally"
			case pin.Acquisition == config.MarketplaceAcquisitionIndex:
				item.Reason = "index-only pin updates are not supported"
			case pin.Kind == "collection":
				item.Reason = "collection pin updates are not supported"
			default:
				item.Skipped = false
			}
			if item.Skipped {
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}

			source, latest, err := resolverMP.LatestRevision(ctx, entry, pin)
			if err != nil {
				return nil, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, pin.Name, err)
			}
			if string(source.Acquisition) != config.MarketplaceAcquisitionMirror {
				item.Skipped = true
				item.Reason = "source is index-only"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if string(latest.ContentDigest) == pin.Digest {
				item.Skipped = true
				item.Reason = "content digest unchanged"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}

			locator := marketplace.MarketplaceLocator{Source: string(source.ID), Kind: pin.Kind, Name: pin.Name, Revision: string(latest.ContentDigest), Digest: string(latest.ContentDigest)}
			prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, targets)
			if err != nil {
				return nil, fmt.Errorf("prepare Marketplace pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, err)
			}
			if err := validateMarketplaceUpdateArtifact(prepared); err != nil {
				return nil, fmt.Errorf("validate Marketplace pin %s/%s/%s: %w", entry.URL, pin.SourceID, pin.Kind, err)
			}
			newPin := prepared.Pin
			newPin.InstalledName = pin.InstalledName
			item.NewPin = newPin
			item.ArchiveDigest = prepared.ArchiveDigest
			plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
		}
	}
	return plans, nil
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
	for _, finding := range plan.Artifact.Manifest.Findings {
		switch strings.ToLower(strings.TrimSpace(finding.PolicyOutcome)) {
		case "allow", "warn":
		case "reject":
			return fmt.Errorf("latest revision is rejected by policy (%s: %s)", marketplace.QuoteTerminal(finding.Code), marketplace.QuoteTerminal(finding.Message))
		default:
			return fmt.Errorf("latest revision has unknown policy outcome %q", marketplace.QuoteTerminal(finding.PolicyOutcome))
		}
	}
	return nil
}

func hasUnsupportedMarketplacePlan(plans []marketplaceUpdatePlan) bool {
	for _, plan := range plans {
		if plan.Skipped && (plan.Reason == "index-only pin updates are not supported" || plan.Reason == "collection pin updates are not supported" || plan.Reason == "source is index-only") {
			return true
		}
	}
	return false
}

func isUnsupportedMarketplaceSyncError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"index-only", "collection resolution is not supported", "collection marketplace", "acquisition mode \"index\" is not supported"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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

func bumpScopeSelected(scopes map[lockfile.Scope]bool, scope lockfile.Scope) bool {
	if scopes == nil {
		return scope == lockfile.ScopeRepo
	}
	return scopes[scope]
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
	return lockfile.CanonicalGitConfigURL(raw)
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
