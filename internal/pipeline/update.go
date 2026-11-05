package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	pathpkg "path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/giturl"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"gopkg.in/yaml.v3"
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
	LocalConfigDir      string
	Bump                bool
	BumpScopes          map[lockfile.Scope]bool
	BumpTarget          *UpdateTarget
	DependencyReconcile UpdateDependencyReconciler
	MarketplaceResolver *marketplace.Resolver
	// DryRun prepares candidate locks, pins, and ref edits without publishing
	// or materializing anything. The dependency reconciler still prepares the
	// exact candidate state its callback inspects; the prepared publication
	// is dropped instead of committed.
	DryRun bool
	// PreseedLockEdits seeds the staged Marketplace lock edits with prepared
	// in-memory documents (preview initial acquisition evidence) before
	// planned pin replacements merge on top of them.
	PreseedLockEdits []StagedLockEdit
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
	ConfigPath       string
	ReplacementPins  []config.MarketplaceResourcePin
	CollectionUpdate bool
}

// MissingMarketplaceResourcesError names every Marketplace pin whose
// latest-revision lookup returned 404. Update returns it together with a
// non-nil result after applying every other planned update, so callers can
// print the report before surfacing the error.
type MissingMarketplaceResourcesError struct {
	Pins []MarketplacePinUpdate
}

func (e *MissingMarketplaceResourcesError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d Marketplace resource(s) no longer exist:", len(e.Pins))
	for _, pin := range e.Pins {
		name := pin.OldPin.InstalledName
		if name == "" {
			name = pin.OldPin.Name
		}
		fmt.Fprintf(&b, "\nmarketplace %s pin %s/%s/%s: resource not found; remove the old pin with `agent-marketplace remove %s` and install the new name",
			pin.Entry.URL, pin.OldPin.RegistryID, pin.OldPin.Kind, pin.OldPin.Name, name)
	}
	return b.String()
}

func missingMarketplacePins(plans []marketplaceUpdatePlan) error {
	var missing *MissingMarketplaceResourcesError
	for i := range plans {
		if plans[i].Reason != marketplaceResourceMissingReason {
			continue
		}
		if missing == nil {
			missing = &MissingMarketplaceResourcesError{}
		}
		missing.Pins = append(missing.Pins, plans[i].MarketplacePinUpdate)
	}
	if missing == nil {
		return nil
	}
	return missing
}

type UpdateTarget struct {
	Registry string
	Ref      string
	Path     string
	Kind     string
	Names    []string
}

type UpdateDependencyReconciler func(context.Context, string, string, []RefBump, []StagedConfigEdit, []StagedLockEdit) (DependencyLockView, error)

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	Report                 *materialize.Report
	Warnings               []error
	UpdatedRegistries      int
	Bumps                  []RefBump
	SkippedBumps           []RefBump
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
	ws, err := workspaceForRoots(repoRoot, opts.LocalConfigDir)
	if err != nil {
		return nil, err
	}
	// A preview never replays pending work: read-only commands refuse a
	// pending journal, and the caller performs that check before preparing
	// anything. Only an applying update recovers admitted writes here.
	if !opts.DryRun {
		if err := RecoverDependencyTransactions(ctx); err != nil {
			return nil, err
		}
	}
	slog.Info("update start", "repo_root", repoRoot, "bump", opts.Bump)
	ec := LoadEffective(ctx, ConfigLoadOnline, config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir()}, nil)
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}
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
	actionable := updatedRegistries > 0
	for _, plan := range plans {
		actionable = actionable || !plan.Skipped
	}
	missing := missingMarketplacePins(plans)
	if !actionable {
		return updateResult(updatedRegistries, nil, plans), missing
	}
	if opts.DependencyReconcile == nil {
		return nil, errors.New("update requires dependency lock reconciliation")
	}
	staging, err := newConfigStagingManager()
	if err != nil {
		return nil, err
	}
	defer closeConfigStaging(staging, "update", &retErr)
	lockEdits := make(map[string]StagedLockEdit)
	var lockEditOrder []string
	for _, edit := range opts.PreseedLockEdits {
		if _, duplicate := lockEdits[edit.Path]; duplicate {
			return nil, fmt.Errorf("duplicate preseeded Marketplace lock edit %s", edit.Path)
		}
		lockEdits[edit.Path] = edit
		lockEditOrder = append(lockEditOrder, edit.Path)
	}
	for i := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		plan := &plans[i]
		if plan.Skipped {
			continue
		}
		v4, err := config.LoadV4ConfigIfPresent(plan.ConfigPath, plan.Source)
		if err != nil {
			return nil, err
		}
		if v4 == nil {
			return nil, fmt.Errorf("v4 Marketplace configuration disappeared: %s", plan.ConfigPath)
		}
		if err := stageV4MarketplaceLockUpdate(plan, v4, lockEdits, &lockEditOrder); err != nil {
			updateErr := fmt.Errorf("update Marketplace pin %s/%s/%s: %w", plan.OldPin.RegistryID, plan.OldPin.Kind, plan.OldPin.Name, err)
			slog.Error("update aborted on Marketplace lock conflict", "config_path", plan.ConfigPath, "registry_id", plan.OldPin.RegistryID, "kind", plan.OldPin.Kind, "name", plan.OldPin.Name, "error", updateErr)
			return nil, updateErr
		}
		plan.Updated = true
	}
	var bumps, skippedBumps []RefBump
	if opts.Bump {
		for _, layer := range ec.PresentLayers() {
			if !layer.Source.IsWritable() {
				continue
			}
			scope := lockfile.Scope(layer.Source.LockScope())
			if !bumpScopeSelected(opts.BumpScopes, scope) {
				continue
			}
			registries := GitRegistryEntries(layer.Sources)
			if len(registries) == 0 {
				continue
			}
			if err := config.ValidateRegistryURLs(registries, ec.Allowed); err != nil {
				return nil, err
			}
			// Includes and source ownership belong to the declaring root, not
			// the private shadow used for dependency publication.
			v4, err := config.LoadV4ConfigIfPresent(layer.Path, layer.Source)
			if err != nil {
				return nil, err
			}
			if v4 == nil {
				return nil, fmt.Errorf("configuration disappeared: %s", layer.Path)
			}
			configPath, err := staging.Path(layer.Path)
			if err != nil {
				return nil, err
			}
			layerTarget := config.LayerTarget{Source: layer.Source, StagedPath: configPath, DeclaringPath: layer.Path}
			layerBumps, layerSkipped, err := bumpRepoRegistryRefs(ctx, layerTarget, v4, &config.Config{Registries: registries}, opts.BumpTarget)
			if err != nil {
				return nil, err
			}
			bumps = append(bumps, layerBumps...)
			skippedBumps = append(skippedBumps, layerSkipped...)
		}
	}

	syncOpts := []silentSyncOpt{withConfigStaging(staging), withDependencyReconciler(func(ctx context.Context, repoRoot, localDir string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		ordered := make([]StagedLockEdit, 0, len(lockEditOrder))
		for _, path := range lockEditOrder {
			ordered = append(ordered, lockEdits[path])
		}
		return opts.DependencyReconcile(ctx, repoRoot, localDir, bumps, edits, ordered)
	})}
	if opts.DryRun {
		// Preview: reconcile and validate the prepared candidate state without
		// publishing config or locks and without materializing. The dependency
		// reconciler's preview callback computes the report from the same
		// prepared candidates an applying invocation would publish.
		if err := silentReconcile(ctx, ws, syncOpts...); err != nil {
			if hasUnsupportedMarketplacePlan(plans) && isUnsupportedMarketplaceSyncError(err) {
				err = fmt.Errorf("sync would be required, but unsupported Marketplace pins prevent synchronization; index-only and collection updates are not implemented — remove or disable those pins before retrying: %w", err)
			}
			return nil, err
		}
		result = updateResult(updatedRegistries, bumps, plans)
		result.SkippedBumps = skippedBumps
		slog.Info("update preview completed", "updated_registries", updatedRegistries, "bumps", len(bumps), "marketplace_updated", result.UpdatedMarketplacePins, "marketplace_skipped", result.SkippedMarketplacePins)
		return result, missing
	}
	synced, syncErr := silentSyncCollect(ctx, ws, syncOpts...)
	result = updateResult(updatedRegistries, bumps, plans)
	result.Report, result.Warnings = synced.Report, synced.Warnings
	result.SkippedBumps = skippedBumps
	if err := syncErr; err != nil {
		if hasUnsupportedMarketplacePlan(plans) && isUnsupportedMarketplaceSyncError(err) {
			err = fmt.Errorf("sync was required, but unsupported Marketplace pins prevented synchronization; index-only and collection updates are not implemented — remove or disable those pins before retrying: %w", err)
		}
		if result.Report == nil {
			return nil, err
		}
		return result, err
	}
	slog.Info("update end", "updated_registries", updatedRegistries, "bumps", len(bumps), "marketplace_updated", result.UpdatedMarketplacePins, "marketplace_skipped", result.SkippedMarketplacePins)
	return result, missing
}

func updateResult(updatedRegistries int, bumps []RefBump, plans []marketplaceUpdatePlan) *UpdateResult {
	result := &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps}
	for _, plan := range plans {
		result.MarketplacePins = append(result.MarketplacePins, plan.MarketplacePinUpdate)
		if plan.Updated {
			result.UpdatedMarketplacePins++
		} else if plan.Skipped {
			result.SkippedMarketplacePins++
		}
	}
	return result
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
	collectionLeaves := make(map[string]bool)
	for _, entry := range ec.Merged.Marketplaces {
		configPath := ""
		if entry.Source.IsWritable() {
			var err error
			configPath, err = marketplaceConfigPath(opts, ec, entry.Source)
			if err != nil {
				return nil, err
			}
		}
		constrained, err := marketplaceV4ConstrainedRequests(configPath, entry.Source)
		if err != nil {
			return nil, err
		}
		for _, pin := range entry.Resources {
			if pin.Kind != "collection" {
				continue
			}
			var leaves []string
			for _, leaf := range entry.Resources {
				for _, selector := range leaf.SelectedBy.List() {
					if selector.RegistryID == pin.RegistryID && selector.Name == pin.Name && selector.Revision == pin.Revision {
						leaves = append(leaves, marketplaceUpdateKey(entry, leaf))
					}
				}
			}
			plan, err := planMarketplaceCollectionUpdate(ctx, resolverMP, entry, pin, configPath, constrained[marketplaceV4PinKey(entry.Alias, pin)], len(leaves) > 0, targets)
			if err != nil {
				return nil, err
			}
			plans = append(plans, plan)
			unchanged := plan.Skipped && plan.Reason == contentDigestUnchangedReason
			for _, key := range leaves {
				refreshable, seen := collectionLeaves[key]
				collectionLeaves[key] = unchanged && (refreshable || !seen)
			}
		}
		for _, pin := range entry.Resources {
			if pin.Kind == "collection" {
				continue
			}
			if constrained[marketplaceV4PinKey(entry.Alias, pin)] {
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Required, Skipped: true, Reason: "request pins an explicit revision"}, ConfigPath: configPath})
				continue
			}
			if entry.Source == config.Organization {
				item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Required, Skipped: true, Reason: "organization pin is managed externally"}
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if pin.Acquisition == config.MarketplaceAcquisitionIndexOnly && len(targets) == 0 {
				item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true, Reason: "index-only pin updates are not supported"}
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			refreshable, selected := collectionLeaves[marketplaceUpdateKey(entry, pin)]
			if selected && !refreshable {
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true, Reason: collectionLeafPinnedReason}, ConfigPath: configPath})
				continue
			}
			source, latest, err := resolverMP.LatestRevision(ctx, entry, pin)
			if err != nil {
				var missingErr *marketplace.MissingResourceError
				if errors.As(err, &missingErr) {
					item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Required, Skipped: true, Reason: marketplaceResourceMissingReason}
					plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
					continue
				}
				return nil, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, err)
			}
			item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Skipped: true}
			if source.Acquisition != "" && string(source.Acquisition) != config.MarketplaceAcquisitionMirror && string(source.Acquisition) != config.MarketplaceAcquisitionIndexOnly {
				item.Reason = "source uses unsupported acquisition"
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			unchanged := string(latest.ContentDigest) == pin.Digest && !marketplace.IndexOnlyOccurrenceChanged(pin, latest)
			if unchanged {
				verified, refreshed, err := refreshDriftedArchiveEvidence(ctx, resolverMP, entry, pin, latest)
				if err != nil {
					return nil, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, err)
				}
				if refreshed {
					item.Skipped = false
					item.NewPin = pin
					item.ArchiveDigest = verified
					item.Reason = fmt.Sprintf("archive evidence refreshed %s -> %s", pin.LockArchiveDigest, verified)
					plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
					continue
				}
			}
			if selected || unchanged {
				item.Reason = contentDigestUnchangedReason
				if selected {
					item.Reason = collectionLeafPinnedReason
				}
				plans = append(plans, marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath})
				continue
			}
			if len(targets) == 0 {
				return nil, &TargetHarnessSetMissingError{}
			}
			locator := marketplace.MarketplaceLocator{Registry: string(source.ID), Kind: pin.Kind, Name: pin.Name, Revision: string(latest.ContentDigest), Digest: string(latest.ContentDigest)}
			prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, targets)
			if err != nil {
				return nil, fmt.Errorf("prepare Marketplace pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, err)
			}
			if err := validateMarketplaceUpdateArtifact(prepared); err != nil {
				return nil, fmt.Errorf("validate Marketplace pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, err)
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

const (
	contentDigestUnchangedReason     = "content digest unchanged"
	collectionLeafPinnedReason       = "leaf is pinned by a collection revision"
	marketplaceResourceMissingReason = "resource no longer exists on the Marketplace"
)

func planMarketplaceCollectionUpdate(ctx context.Context, resolverMP *marketplace.Resolver, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, configPath string, constrained, expanded bool, targets []hcc.Target) (marketplaceUpdatePlan, error) {
	item := MarketplacePinUpdate{Entry: entry, OldPin: pin, Source: entry.Source, Required: entry.Required, Skipped: true}
	skip := func(reason string) (marketplaceUpdatePlan, error) {
		item.Reason = reason
		return marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath}, nil
	}
	switch {
	case constrained:
		return skip("request pins an explicit revision")
	case entry.Source == config.Organization:
		return skip("organization pin is managed externally")
	case !expanded:
		return skip("collection pin updates are not supported")
	}
	source, latest, err := resolverMP.LatestRevision(ctx, entry, pin)
	if err != nil {
		var missingErr *marketplace.MissingResourceError
		if errors.As(err, &missingErr) {
			item.Reason = marketplaceResourceMissingReason
			return marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath}, nil
		}
		return marketplaceUpdatePlan{}, fmt.Errorf("marketplace %s pin %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, pin.Name, err)
	}
	if string(latest.ContentDigest) == pin.Digest {
		return skip(contentDigestUnchangedReason)
	}
	if len(targets) == 0 {
		return marketplaceUpdatePlan{}, &TargetHarnessSetMissingError{}
	}
	locator := marketplace.MarketplaceLocator{Registry: string(source.ID), Kind: pin.Kind, Name: pin.Name, Revision: string(latest.ContentDigest), Digest: string(latest.ContentDigest)}
	prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, targets)
	if err != nil {
		return marketplaceUpdatePlan{}, fmt.Errorf("prepare Marketplace collection %s/%s/%s: %w", entry.URL, pin.RegistryID, pin.Kind, err)
	}
	for _, leaf := range prepared.CollectionMembers {
		if err := validatePreparedMarketplaceUpdate(leaf); err != nil {
			return marketplaceUpdatePlan{}, fmt.Errorf("validate Marketplace collection leaf: %w", err)
		}
	}
	item.Skipped = false
	item.NewPin = prepared.Pin
	item.ArchiveDigest = prepared.ArchiveDigest
	return marketplaceUpdatePlan{MarketplacePinUpdate: item, ConfigPath: configPath, ReplacementPins: marketplaceInstallPins(prepared), CollectionUpdate: true}, nil
}

func refreshDriftedArchiveEvidence(ctx context.Context, resolverMP *marketplace.Resolver, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin, latest store.AssessedResourceRevision) (string, bool, error) {
	if pin.LockArchiveDigest == "" || latest.ArchiveDigest == "" || string(latest.ArchiveDigest) == pin.LockArchiveDigest {
		return "", false, nil
	}
	verified, err := resolverMP.RefreshArchiveEvidence(ctx, entry, pin)
	if err != nil {
		return "", false, err
	}
	if verified == "" || verified == pin.LockArchiveDigest {
		return "", false, nil
	}
	return verified, true, nil
}

func marketplaceUpdateKey(entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	return entry.URL + "\x00" + entry.InstanceID + "\x00" + pin.RegistryID + "\x00" + pin.Kind + "\x00" + pin.Name
}

func validatePreparedMarketplaceUpdate(plan *marketplace.PreparedInstall) error {
	if plan == nil {
		return fmt.Errorf("prepared Marketplace leaf is nil")
	}
	return validateMarketplaceUpdateArtifact(plan)
}

func stageV4MarketplaceLockUpdate(plan *marketplaceUpdatePlan, v4 *config.V4Config, lockEdits map[string]StagedLockEdit, order *[]string) error {
	lockPath, err := marketplaceLayerLockPath(plan.ConfigPath)
	if err != nil {
		return err
	}
	scope := lockfile.Scope(plan.Source.LockScope())
	var current lockfile.Document
	var generation dependencytxn.Generation
	var mode os.FileMode
	if previous, ok := lockEdits[lockPath]; ok {
		generation = previous.Expected
		mode = previous.ExpectedMode
		current, err = lockfile.Decode(previous.Bytes)
		if err != nil {
			return fmt.Errorf("decode staged Marketplace lock %s: %w", lockPath, err)
		}
	} else {
		_, generation, mode, current, err = readMarketplaceLayerLock(lockPath, scope)
		if err != nil {
			return err
		}
	}
	if current.Scope == "" {
		current.Scope = scope
	}
	replacements := plan.ReplacementPins
	if !plan.CollectionUpdate {
		replacements = []config.MarketplaceResourcePin{plan.NewPin}
	}
	entry := plan.Entry
	entry.Source = plan.Source
	merged, err := marketplacelock.ReplacePinsToV4(v4, current, entry, plan.OldPin, replacements, marketplace.DefaultCache())
	if err != nil {
		return err
	}
	data, err := lockfile.Encode(merged)
	if err != nil {
		return err
	}
	fileMode := mode
	if !generation.Exists {
		fileMode = 0o644
	}
	if _, ok := lockEdits[lockPath]; !ok {
		*order = append(*order, lockPath)
	}
	lockEdits[lockPath] = StagedLockEdit{Path: lockPath, Expected: generation, ExpectedMode: mode, Bytes: data, Mode: fileMode}
	return nil
}

func marketplaceV4ConstrainedRequests(configPath string, source config.Source) (map[string]bool, error) {
	if configPath == "" {
		return nil, nil
	}
	v4, err := config.LoadV4ConfigIfPresent(configPath, source)
	if err != nil || v4 == nil {
		return nil, err
	}
	constrained := make(map[string]bool)
	for i := range v4.Requests {
		request := &v4.Requests[i]
		if request.Options.Revision == "" {
			continue
		}
		source := v4.Sources[request.SourceName]
		if source.Kind != config.V4SourceMarketplace {
			continue
		}
		constrained[request.SourceName+"\x00"+request.Registry+"\x00"+v4MarketplaceRequestLockKind(request.Kind)+"\x00"+request.Name] = true
	}
	return constrained, nil
}

func v4MarketplaceRequestLockKind(kind string) string {
	if kind == "mcp" {
		return "mcpserver"
	}
	return kind
}

func marketplaceV4PinKey(sourceName string, pin config.MarketplaceResourcePin) string {
	registry := pin.RegistrySlug
	if registry == "" {
		registry = pin.RegistryID
	}
	return sourceName + "\x00" + registry + "\x00" + pin.Kind + "\x00" + pin.Name
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
		if plan.Revision.EffectiveAvailability == string(mpapi.AvailabilityOperatorExposed) {
			return nil
		}
		return fmt.Errorf("latest revision is rejected by policy")
	default:
		return fmt.Errorf("latest revision has unknown assessment decision %q", marketplace.QuoteTerminal(string(plan.Revision.Assessment.Decision)))
	}
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

func bumpScopeSelected(scopes map[lockfile.Scope]bool, scope lockfile.Scope) bool {
	if scopes == nil {
		return scope == lockfile.ScopeRepo
	}
	return scopes[scope]
}

func bumpRepoRegistryRefs(ctx context.Context, layerTarget config.LayerTarget, v4 *config.V4Config, repoCfg *config.Config, target *UpdateTarget) (bumps, skipped []RefBump, retErr error) {
	if repoCfg == nil || len(repoCfg.Registries) == 0 {
		return nil, nil, nil
	}
	wantedURL := ""
	if target != nil && strings.TrimSpace(target.Registry) != "" {
		var err error
		wantedURL, err = canonicalUpdateRegistryURL(target.Registry)
		if err != nil {
			return nil, nil, fmt.Errorf("canonicalize update registry target: %w", err)
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
		// A full commit ID is an exact pin, never a version, whatever tags the
		// registry publishes.
		if entry.IsLocal() || giturl.IsFullObjectID(strings.TrimSpace(entry.Ref)) || !updateTargetMatchesEntry(entry, target, wantedURL) {
			continue
		}

		cachePath, err := cache.CloneOrPull(entry.URL, entry.Ref)
		if err != nil {
			return nil, nil, err
		}

		newRef, ok, err := discoverTagBump(cachePath, entry.Ref)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}

		key := entry.URL + "\x00" + entry.Ref
		if existing, exists := planned[key]; exists {
			if existing != newRef {
				return nil, nil, fmt.Errorf("conflicting bump targets for %s@%s: %s vs %s", entry.URL, entry.Ref, existing, newRef)
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

	applied := bumps[:0]
	rootRefs := make(map[string]RefBump)
	for _, bump := range bumps {
		owned := false
		for name, source := range v4.Sources {
			// A source declared identically in an included fragment is not
			// root-owned: advancing only the root copy would leave the tree
			// with conflicting declarations.
			if source.Kind == config.V4SourceGit && source.File == v4.Files[len(v4.Files)-1] && len(source.AlsoDeclaredIn) == 0 &&
				config.NormalizeGitURL(source.URL) == config.NormalizeGitURL(bump.URL) && source.Ref == bump.OldRef {
				rootRefs[name] = bump
				owned = true
			}
		}
		if !owned {
			slog.Warn("update bump skipped: ref is not owned by the root config file alone", "url", bump.URL, "old_ref", bump.OldRef, "new_ref", bump.NewRef)
			skipped = append(skipped, bump)
			continue
		}
		applied = append(applied, bump)
	}
	if len(rootRefs) > 0 {
		if err := stageRootRegistryRefs(layerTarget, rootRefs); err != nil {
			return nil, nil, err
		}
	}

	return applied, skipped, nil
}

// stageRootRegistryRefs changes only root-owned scalar refs. LayerEditor keeps
// full-tree validation anchored to the authored path before and after the edit;
// only the private shadow changes here, and dependency reconciliation publishes it.
func stageRootRegistryRefs(target config.LayerTarget, refs map[string]RefBump) error {
	editor, err := config.OpenLayerEditor(target)
	if err != nil {
		return err
	}
	candidate, err := editor.Render()
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(candidate.Bytes, &doc); err != nil {
		return fmt.Errorf("parse staged config %s: %w", target.DeclaringPath, err)
	}
	sources := updateMappingValue(doc.Content[0], "sources")
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bump := refs[name]
		source := updateMappingValue(sources, name)
		gitNode, refNode := updateMappingValue(source, "git"), updateMappingValue(source, "ref")
		if gitNode == nil || refNode == nil || config.NormalizeGitURL(gitNode.Value) != config.NormalizeGitURL(bump.URL) || strings.TrimSpace(refNode.Value) != bump.OldRef {
			return fmt.Errorf("%w: source %s changed while staging %s", dependencytxn.ErrGenerationConflict, name, target.DeclaringPath)
		}
		refNode.Value = bump.NewRef
	}
	var data bytes.Buffer
	encoder := yaml.NewEncoder(&data)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return fmt.Errorf("encode staged config %s: %w", target.DeclaringPath, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close staged config encoder %s: %w", target.DeclaringPath, err)
	}
	if err := os.WriteFile(target.StagedPath, data.Bytes(), candidate.Mode); err != nil {
		return fmt.Errorf("write staged config %s: %w", target.DeclaringPath, err)
	}
	_, err = config.OpenLayerEditor(target)
	return err
}

func updateMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
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

	args := []string{"tag", "-l", matches[1] + "*"}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	var stderr tagBumpStderr
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, fmt.Errorf("prepare git tag output: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", false, fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), repoDir, err)
	}

	bestRef, scanErr := selectTagBump(stdout, matches[1], currentNumber)
	// Drain after a parse failure so Git can finish; its exit error still takes
	// precedence, as it did when Output collected everything before parsing.
	if scanErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			exit.Stderr = stderr.Bytes()
		}
		return "", false, fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), repoDir, err)
	}
	if scanErr != nil {
		return "", false, scanErr
	}
	return bestRef, bestRef != "", nil
}

func selectTagBump(r io.Reader, prefix string, currentNumber int) (string, error) {
	scanner := bufio.NewScanner(r)
	// Do not turn Scanner's default token limit into a new Git tag policy.
	scanner.Buffer(make([]byte, 32*1024), math.MaxInt)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	bestRef := ""
	bestNumber := currentNumber
	consider := func(tag []byte) error {
		suffix := bytes.TrimPrefix(tag, []byte(prefix))
		if len(suffix) == 0 {
			return nil
		}
		for _, digit := range suffix {
			if digit < '0' || digit > '9' {
				return nil
			}
		}

		tagNumber, err := strconv.Atoi(string(suffix))
		if err != nil {
			return fmt.Errorf("parse tag %q: %w", tag, err)
		}

		if tagNumber > bestNumber || (tagNumber == bestNumber && string(tag) > bestRef) {
			bestRef = string(tag)
			bestNumber = tagNumber
		}
		return nil
	}
	var pending []byte
	leading := true
	for scanner.Scan() {
		tag := scanner.Bytes()
		if leading {
			tag = bytes.TrimLeftFunc(tag, unicode.IsSpace)
		}
		if len(bytes.TrimSpace(tag)) == 0 {
			continue
		}
		leading = false
		if err := consider(pending); err != nil {
			return "", err
		}
		// Delay the last nonblank line to retain TrimSpace's original treatment
		// of the entire output, including Unicode whitespace in valid Git refs.
		pending = append(pending[:0], tag...)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read git tags: %w", err)
	}
	if err := consider(bytes.TrimRightFunc(pending, unicode.IsSpace)); err != nil {
		return "", err
	}
	return bestRef, nil
}

const tagBumpStderrLimit = 32 * 1024

// Retain Output's bounded stderr payload on ExitError without buffering stdout.
type tagBumpStderr struct {
	prefix       []byte
	suffix       []byte
	suffixOffset int
	size         int64
}

func (w *tagBumpStderr) ReadFrom(r io.Reader) (int64, error) {
	// Avoid a second 32 KiB copy buffer even when Git emits no stderr.
	var buffer [4096]byte
	return io.CopyBuffer(struct{ io.Writer }{w}, struct{ io.Reader }{r}, buffer[:])
}

func (w *tagBumpStderr) Write(p []byte) (int, error) {
	n := len(p)
	w.size += int64(n)
	head := min(len(p), tagBumpStderrLimit-len(w.prefix))
	w.prefix = append(w.prefix, p[:head]...)
	p = p[head:]
	if len(p) == 0 {
		return n, nil
	}
	if w.suffix == nil {
		w.suffix = make([]byte, tagBumpStderrLimit)
	}
	if len(p) >= tagBumpStderrLimit {
		copy(w.suffix, p[len(p)-tagBumpStderrLimit:])
		w.suffixOffset = 0
	} else {
		written := copy(w.suffix[w.suffixOffset:], p)
		copy(w.suffix, p[written:])
		w.suffixOffset = (w.suffixOffset + len(p)) % tagBumpStderrLimit
	}
	return n, nil
}

func (w *tagBumpStderr) Bytes() []byte {
	if w.size <= tagBumpStderrLimit {
		return w.prefix
	}
	if w.size <= 2*tagBumpStderrLimit {
		return append(w.prefix, w.suffix[:int(w.size)-tagBumpStderrLimit]...)
	}
	out := fmt.Appendf(w.prefix, "\n... omitting %d bytes ...\n", w.size-2*tagBumpStderrLimit)
	out = append(out, w.suffix[w.suffixOffset:]...)
	return append(out, w.suffix[:w.suffixOffset]...)
}
