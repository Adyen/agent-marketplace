package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
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

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	UpdatedRegistries      int
	Bumps                  []RefBump
	MarketplacePins        []MarketplacePinUpdate
	UpdatedMarketplacePins int
	SkippedMarketplacePins int
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

// Update refreshes pinned registries, explicitly advances eligible Marketplace
// pins, and syncs enabled resources. Marketplace plans are completely prepared
// before any config mutation.
func Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	repoRoot := opts.RepoRoot
	slog.Info("update start", "repo_root", repoRoot, "bump", opts.Bump)

	// Load the effective configuration to obtain the authoritative allowlists
	// and validate all configured endpoints before network access.
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{
		RepoRoot:       repoRoot,
		LocalConfigDir: opts.LocalConfigDir,
	})
	if len(ec.Errors) > 0 {
		slog.Error("update aborted on effective config errors", "error_count", len(ec.Errors))
		return nil, errors.Join(ec.Errors...)
	}

	updatedRegistries := 0
	if ec.Merged != nil {
		updatedRegistries = len(ec.Merged.Registries)
	}
	// A Git registry always causes the final sync to run. Fail before bumping
	// refs or refreshing Marketplace metadata when that sync cannot run.
	if updatedRegistries > 0 && len(ec.TargetHarnesses) == 0 {
		err := &TargetHarnessSetMissingError{}
		slog.Error("update requires target harness set for Git sync", "error", err)
		return nil, err
	}
	plans, err := planMarketplaceUpdates(ctx, opts, &ec)
	if err != nil {
		slog.Error("update aborted while preparing Marketplace pins", "error", err)
		return nil, err
	}

	var mutations []configMutation
	var bumps []RefBump
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Bump {
		repoCfg := ec.Repo.Config
		if repoCfg != nil && len(repoCfg.Registries) > 0 {
			if err := config.ValidateRegistryURLs(repoCfg.Registries, ec.Allowed); err != nil {
				slog.Error("update blocked by allowed_registries", "error", err)
				return nil, err
			}
			configPath := config.ProjectConfigPath(repoRoot)
			// The bump editor predates the guarded config-edit APIs, so take
			// the snapshot before it starts and capture its exact final bytes
			// afterward. This makes registry ref changes part of the same
			// rollback transaction as Marketplace pin changes.
			snapshot, err := config.SnapshotConfigFile(configPath)
			if err != nil {
				return nil, err
			}
			bumps, err = bumpRepoRegistryRefs(configPath, repoCfg)
			if err != nil {
				return nil, rollbackFailedConfigEdit(configPath, snapshot, err)
			}
			if len(bumps) > 0 {
				mutation, err := committedConfigMutation(configPath, snapshot)
				if err != nil {
					return nil, rollbackFailedConfigEdit(configPath, snapshot, err)
				}
				mutations = append(mutations, mutation)
			}
		} else {
			slog.Info("update skipping bump", "reason", "no repository registries")
		}
	}

	for i := range plans {
		if err := ctx.Err(); err != nil {
			return nil, rollbackConfigMutations(mutations, err)
		}
		plan := &plans[i]
		if plan.Skipped {
			continue
		}
		var snapshot *config.FileSnapshot
		var commit config.ConfigFileCommit
		if plan.CollectionUpdate {
			snapshot, commit, err = config.EnsureMarketplaceCollectionWithSnapshotAndCommit(plan.ConfigPath, plan.Entry.URL, plan.Entry.InstanceID, plan.OldPin, plan.ReplacementPins)
		} else {
			snapshot, commit, err = config.UpdateMarketplacePinInConfigWithSnapshotAndCommit(plan.ConfigPath, plan.Entry.URL, plan.Entry.InstanceID, plan.OldPin, plan.NewPin)
		}
		if err != nil {
			updateErr := fmt.Errorf("update Marketplace pin %s/%s/%s: %w", plan.OldPin.SourceID, plan.OldPin.Kind, plan.OldPin.Name, err)
			slog.Error("update aborted on Marketplace config conflict", "config_path", plan.ConfigPath, "source_id", plan.OldPin.SourceID, "kind", plan.OldPin.Kind, "name", plan.OldPin.Name, "error", updateErr)
			return nil, rollbackConfigMutations(mutations, updateErr)
		}
		mutations = append(mutations, configMutation{path: plan.ConfigPath, snapshot: snapshot, commit: commit})
		plan.Updated = true
	}

	syncRequired := updatedRegistries > 0 || len(bumps) > 0
	for _, plan := range plans {
		syncRequired = syncRequired || plan.Updated
	}
	if syncRequired {
		if err := silentSync(ctx, repoRoot, silentSyncOptsForUpdate(opts)...); err != nil {
			if hasUnsupportedMarketplacePlan(plans) && isUnsupportedMarketplaceSyncError(err) {
				err = fmt.Errorf("sync was required, but unsupported Marketplace pins prevented synchronization; index-only and collection updates are not implemented — remove or disable those pins before retrying: %w", err)
			}
			slog.Error("update sync failed; rolling back config", "repo_root", repoRoot, "error", err)
			return nil, rollbackConfigMutations(mutations, err)
		}
	} else {
		slog.Info("update skipping sync", "reason", "only unsupported or unchanged Marketplace pins")
	}

	result := &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps}
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

func committedConfigMutation(path string, snapshot *config.FileSnapshot) (configMutation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return configMutation{}, fmt.Errorf("read committed config %q: %w", path, err)
	}
	return configMutation{path: path, snapshot: snapshot, commit: config.NewConfigFileCommit(data)}, nil
}

func rollbackFailedConfigEdit(path string, snapshot *config.FileSnapshot, cause error) error {
	mutation, err := committedConfigMutation(path, snapshot)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("capture config %q for rollback: %w", path, err))
	}
	return rollbackConfigMutations([]configMutation{mutation}, cause)
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

func bumpRepoRegistryRefs(ctx context.Context, configPath string, repoCfg *config.Config) (bumps []RefBump, retErr error) {
	if repoCfg == nil || len(repoCfg.Registries) == 0 {
		return nil, nil
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
		if entry.IsLocal() {
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
