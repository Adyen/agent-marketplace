package claudedelivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudeplugins"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const minimumWriteInterval = time.Second

type ReconcileTarget struct {
	APIKeyID                      string
	MarketplaceID                 string
	PluginNamePrefix              string
	DefaultInstallationPreference string
}

type ReconcileOptions struct {
	Metadata            store.MetadataStore
	Blobs               store.BlobStore
	Client              *claudeplugins.Client
	SourceMarketplaceID store.MarketplaceID
	RegistryIDs         []store.RegistryID
	Target              ReconcileTarget
	Limiter             WriteLimiter
	Now                 func() time.Time
}

type WriteLimiter interface {
	Wait(context.Context, time.Duration) error
}

type ReconcileResult struct {
	Published int
	Retired   int
	Skipped   int
	Conflicts int
	Failed    int
}

type ReconcileError struct {
	code string
}

func (e *ReconcileError) Error() string     { return "Claude delivery reconcile failed: " + e.code }
func (e *ReconcileError) ErrorCode() string { return e.code }

type Reconciler struct {
	options ReconcileOptions
	mu      sync.Mutex
}

func NewReconciler(options ReconcileOptions) *Reconciler {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.Limiter == nil {
		options.Limiter = &writePacer{}
	}
	if options.Target.PluginNamePrefix == "" {
		options.Target.PluginNamePrefix = "am"
	}
	if options.Target.DefaultInstallationPreference == "" {
		options.Target.DefaultInstallationPreference = "available"
	}
	return &Reconciler{options: options}
}

func (r *Reconciler) Reconcile(ctx context.Context) (ReconcileResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.Metadata == nil || r.options.Blobs == nil || r.options.Client == nil {
		return ReconcileResult{}, &ReconcileError{code: "configuration.invalid"}
	}
	if r.options.Target.APIKeyID == "" {
		return ReconcileResult{}, &ReconcileError{code: "api-key-id.missing"}
	}
	state, ok := r.options.Metadata.(store.ClaudeDeliveryStateStore)
	if !ok {
		return ReconcileResult{}, &ReconcileError{code: "state-store.unavailable"}
	}
	publications, ok := r.options.Metadata.(store.PublicationStore)
	if !ok {
		return ReconcileResult{}, &ReconcileError{code: "publication-store.unavailable"}
	}
	marketplace, err := r.resolveMarketplace(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	inventory, err := r.options.Client.ListPlugins(ctx, claudeplugins.PluginListQuery{
		OwnerType: "organization", MarketplaceID: marketplace.ID,
	})
	if err != nil {
		if isThrottled(err) {
			return ReconcileResult{}, err
		}
		return ReconcileResult{}, &ReconcileError{code: "inventory.read"}
	}
	byName := make(map[string]claudeplugins.Plugin, len(inventory))
	for _, plugin := range inventory {
		if plugin.MarketplaceID == "" || plugin.MarketplaceID == marketplace.ID {
			byName[plugin.Name] = plugin
		}
	}
	desired, err := state.ListClaudeDeliveryDesired(ctx, publications, r.options.SourceMarketplaceID, r.options.RegistryIDs)
	if err != nil {
		return ReconcileResult{}, &ReconcileError{code: "desired-set.read"}
	}
	wanted := make(map[store.ResourceCoordinate]struct{}, len(desired))
	for _, item := range desired {
		wanted[item.Resource.Coordinate] = struct{}{}
	}
	rows, err := state.ListClaudeDeliveryPlugins(ctx)
	if err != nil {
		return ReconcileResult{}, &ReconcileError{code: "state.read"}
	}
	scopedRows := rows[:0]
	existing := make(map[store.ResourceCoordinate]store.ClaudeDeliveryPlugin, len(rows))
	for _, row := range rows {
		if row.Coordinate.Marketplace != r.options.SourceMarketplaceID {
			continue
		}
		if row.PluginID != "" && row.TargetMarketplaceID != marketplace.ID {
			return ReconcileResult{}, &ReconcileError{code: "marketplace.target-changed"}
		}
		scopedRows = append(scopedRows, row)
		existing[row.Coordinate] = row
	}
	rows = scopedRows
	claimedNames := make(map[string]store.ResourceCoordinate, len(rows)+len(desired))
	for _, row := range rows {
		if row.PluginID != "" && row.PluginName != "" {
			claimedNames[row.PluginName] = row.Coordinate
		}
	}
	result := ReconcileResult{}
	for _, item := range desired {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		row, hasState := existing[item.Resource.Coordinate]
		if !hasState || row.PluginName == "" {
			row.Coordinate = item.Resource.Coordinate
			registry, err := r.options.Metadata.GetRegistry(ctx, row.Coordinate.Registry)
			if err != nil {
				if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, "registry.read"); saveErr != nil {
					return result, saveErr
				}
				result.Failed++
				continue
			}
			row.PluginName, err = DerivePluginName(r.options.Target.PluginNamePrefix, ResourceIdentity{
				RegistryID: string(row.Coordinate.Registry), RegistrySlug: registry.Slug,
				Kind: row.Coordinate.Kind, Name: row.Coordinate.Name,
			})
			if err != nil {
				if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, "plugin-name.invalid"); saveErr != nil {
					return result, saveErr
				}
				result.Failed++
				continue
			}
		}
		if row.PluginID == "" {
			if owner, claimed := claimedNames[row.PluginName]; claimed && owner != row.Coordinate {
				if err := state.MarkClaudeDeliveryConflict(ctx, row.Coordinate, row.PluginName, "plugin.name-collision", r.options.Now()); err != nil {
					return result, &ReconcileError{code: "state.write"}
				}
				result.Conflicts++
				continue
			}
			claimedNames[row.PluginName] = row.Coordinate
		}
		archive, built, diagnostic, err := r.build(ctx, item, row.PluginName)
		if diagnostic != "" {
			if err := state.MarkClaudeDeliverySkipped(ctx, row.Coordinate, row.PluginName, diagnostic, r.options.Now()); err != nil {
				return result, &ReconcileError{code: "state.write"}
			}
			result.Skipped++
			continue
		}
		if err != nil {
			if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, "artifact.unavailable"); saveErr != nil {
				return result, saveErr
			}
			result.Failed++
			continue
		}
		plugin, owned, err := r.resolveOwnedPlugin(ctx, row, hasState, marketplace.ID, byName)
		if err != nil {
			if deliveryErrorCode(err) == "marketplace.target-changed" {
				return result, err
			}
			if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
				return result, stopErr
			}
			if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, "plugin.read"); saveErr != nil {
				return result, saveErr
			}
			result.Failed++
			continue
		}
		if plugin.ID != "" && !owned {
			if err := state.MarkClaudeDeliveryConflict(ctx, row.Coordinate, row.PluginName, "plugin.foreign", r.options.Now()); err != nil {
				return result, &ReconcileError{code: "state.write"}
			}
			result.Conflicts++
			continue
		}
		created := false
		if plugin.ID == "" {
			plugin, created, err = r.createPlugin(ctx, state, row, marketplace.ID, archive, built)
			if err != nil {
				if deliveryErrorCode(err) == "marketplace.target-changed" {
					return result, err
				}
				if errors.Is(err, errWriteRateLimit) {
					return result, &ReconcileError{code: "write.rate-limited"}
				}
				if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
					return result, stopErr
				}
				if deliveryErrorCode(err) == "plugin.foreign" {
					if markErr := state.MarkClaudeDeliveryConflict(ctx, row.Coordinate, row.PluginName, "plugin.foreign", r.options.Now()); markErr != nil {
						return result, &ReconcileError{code: "state.write"}
					}
					result.Conflicts++
					continue
				}
				if deliveryErrorCode(err) == "plugin.skill-name-taken" {
					if markErr := state.MarkClaudeDeliverySkipped(ctx, row.Coordinate, row.PluginName, "plugin.skill-name-taken", r.options.Now()); markErr != nil {
						return result, &ReconcileError{code: "state.write"}
					}
					result.Skipped++
					continue
				}
				if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, deliveryErrorCode(err)); saveErr != nil {
					return result, saveErr
				}
				result.Failed++
				continue
			}
		}
		if created {
			row = createdPluginRow(row, plugin, item.Resource.ContentDigest, built, marketplace.ID,
				r.options.Target.DefaultInstallationPreference, r.options.Now())
			if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
				return result, &ReconcileError{code: "state.write"}
			}
			if err := r.setInstallationPreference(ctx, plugin.ID, row.InstallationPreference); err != nil {
				if errors.Is(err, errWriteRateLimit) {
					return result, &ReconcileError{code: "write.rate-limited"}
				}
				if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
					return result, stopErr
				}
				if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, deliveryErrorCode(err)); saveErr != nil {
					return result, saveErr
				}
				result.Failed++
				continue
			}
			result.Published++
			continue
		}
		returning := hasState && existing[row.Coordinate].State == store.ClaudeDeliveryStateRetired
		row.PluginID = plugin.ID
		row.TargetMarketplaceID = marketplace.ID
		row.State = store.ClaudeDeliveryStatePublished
		if row.PublishedArchiveDigest == store.ArchiveDigest(built.Digest) {
			preferenceNeedsWrite := plugin.OrganizationInstallationPreference != r.options.Target.DefaultInstallationPreference ||
				plugin.OrganizationInstallationInherited
			if preferenceNeedsWrite || returning ||
				row.ContentScanStatus != plugin.ContentScan.Status ||
				row.Reach != plugin.Reach || row.ServedVersionID != plugin.ServedVersionID {
				row.InstallationPreference = r.options.Target.DefaultInstallationPreference
				row.ContentScanStatus = plugin.ContentScan.Status
				row.Reach = plugin.Reach
				row.ServedVersionID = plugin.ServedVersionID
				row.UpdatedAt = r.options.Now()
				if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
					return result, &ReconcileError{code: "state.write"}
				}
				if preferenceNeedsWrite {
					if err := r.setInstallationPreference(ctx, plugin.ID, row.InstallationPreference); err != nil {
						if errors.Is(err, errWriteRateLimit) {
							return result, &ReconcileError{code: "write.rate-limited"}
						}
						if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
							return result, stopErr
						}
						if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, deliveryErrorCode(err)); saveErr != nil {
							return result, saveErr
						}
						result.Failed++
						continue
					}
				}
				result.Published++
			}
			continue
		}
		if returning || row.InstallationPreference == "" {
			row.InstallationPreference = r.options.Target.DefaultInstallationPreference
		}
		preferenceNeedsWrite := !hasState || returning ||
			plugin.OrganizationInstallationPreference != row.InstallationPreference ||
			plugin.OrganizationInstallationInherited
		row, err = r.publishVersion(ctx, state, row, plugin, marketplace.ID,
			item.Resource.ContentDigest, archive, built)
		if err != nil {
			if errors.Is(err, errWriteRateLimit) {
				return result, &ReconcileError{code: "write.rate-limited"}
			}
			if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
				return result, stopErr
			}
			if deliveryErrorCode(err) == "plugin.skill-name-taken" {
				if markErr := state.MarkClaudeDeliverySkipped(ctx, row.Coordinate, row.PluginName, "plugin.skill-name-taken", r.options.Now()); markErr != nil {
					return result, &ReconcileError{code: "state.write"}
				}
				result.Skipped++
				continue
			}
			if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, deliveryErrorCode(err)); saveErr != nil {
				return result, saveErr
			}
			result.Failed++
			continue
		}
		if preferenceNeedsWrite {
			if err := r.setInstallationPreference(ctx, plugin.ID, row.InstallationPreference); err != nil {
				if errors.Is(err, errWriteRateLimit) {
					return result, &ReconcileError{code: "write.rate-limited"}
				}
				if stop, stopErr := r.recordThrottleFailure(ctx, state, row, marketplace.ID, &result, err); stop {
					return result, stopErr
				}
				if saveErr := r.recordFailure(ctx, state, row, marketplace.ID, deliveryErrorCode(err)); saveErr != nil {
					return result, saveErr
				}
				result.Failed++
				continue
			}
		}
		result.Published++
	}
	for _, row := range rows {
		if _, isWanted := wanted[row.Coordinate]; isWanted || row.PluginID == "" ||
			row.State == store.ClaudeDeliveryStateRetired {
			continue
		}
		if err := r.setInstallationPreference(ctx, row.PluginID, "not_available"); err != nil {
			if errors.Is(err, errWriteRateLimit) {
				return result, &ReconcileError{code: "write.rate-limited"}
			}
			if stop, stopErr := r.recordThrottleFailure(ctx, state, row, row.TargetMarketplaceID, &result, err); stop {
				return result, stopErr
			}
			if saveErr := r.recordFailure(ctx, state, row, row.TargetMarketplaceID, deliveryErrorCode(err)); saveErr != nil {
				return result, saveErr
			}
			result.Failed++
			continue
		}
		if err := state.MarkClaudeDeliveryRetired(ctx, row.Coordinate, r.options.Now()); err != nil {
			return result, &ReconcileError{code: "state.write"}
		}
		result.Retired++
	}
	return result, nil
}

func (r *Reconciler) resolveMarketplace(ctx context.Context) (claudeplugins.PluginMarketplace, error) {
	if r.options.Target.MarketplaceID != "" {
		marketplace, err := r.options.Client.GetMarketplace(ctx, r.options.Target.MarketplaceID)
		if err != nil {
			if isThrottled(err) {
				return claudeplugins.PluginMarketplace{}, err
			}
			return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.not-found"}
		}
		if marketplace.Owner.Type != "organization" || marketplace.Source != "manual" {
			return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.unusable"}
		}
		return marketplace, nil
	}
	marketplaces, err := r.options.Client.ListMarketplaces(ctx, claudeplugins.MarketplaceListQuery{OwnerType: "organization"})
	if err != nil {
		if isThrottled(err) {
			return claudeplugins.PluginMarketplace{}, err
		}
		return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.read"}
	}
	var manual []claudeplugins.PluginMarketplace
	organizationMarketplaces := 0
	for _, marketplace := range marketplaces {
		if marketplace.Owner.Type != "organization" {
			continue
		}
		organizationMarketplaces++
		if marketplace.Source == "manual" {
			manual = append(manual, marketplace)
		}
	}
	if len(manual) == 1 {
		return manual[0], nil
	}
	if len(manual) > 1 {
		return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.ambiguous"}
	}
	if organizationMarketplaces > 0 {
		return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.unusable"}
	}
	return claudeplugins.PluginMarketplace{}, &ReconcileError{code: "marketplace.not-found"}
}

func (r *Reconciler) build(ctx context.Context, item store.ClaudeDeliveryDesired, pluginName string) ([]byte, BuildResult, string, error) {
	if item.Resource.ArchiveDigest == "" {
		return nil, BuildResult{}, "", errors.New("artifact unavailable")
	}
	canonicalArtifact, err := r.options.Blobs.Get(ctx, item.Resource.ArchiveDigest)
	if err != nil {
		return nil, BuildResult{}, "", errors.New("artifact unavailable")
	}
	registry, err := r.options.Metadata.GetRegistry(ctx, item.Resource.Coordinate.Registry)
	if err != nil {
		return nil, BuildResult{}, "", errors.New("registry unavailable")
	}
	result, err := BuildArchive(BuildInput{
		CanonicalArtifact: canonicalArtifact,
		Identity: ResourceIdentity{
			RegistryID: string(item.Resource.Coordinate.Registry), RegistrySlug: registry.Slug,
			Kind: item.Resource.Coordinate.Kind, Name: item.Resource.Coordinate.Name,
		},
		PluginNamePrefix: r.options.Target.PluginNamePrefix,
		PluginName:       pluginName,
		PublicationID:    string(item.PublicationID),
		RegistryCommit:   item.Resource.Occurrence.Commit,
		PublisherName:    registry.Publisher.Name,
		PublisherURL:     registry.Publisher.URL,
		ContentDigest:    string(item.Resource.ContentDigest),
	})
	if err != nil {
		return nil, BuildResult{}, "archive.invalid", nil
	}
	diagnostics := ValidateArchive(result.Archive)
	if len(diagnostics) > 0 {
		return nil, result, diagnostics[0].Code, nil
	}
	return result.Archive, result, "", nil
}

func (r *Reconciler) resolveOwnedPlugin(ctx context.Context, row store.ClaudeDeliveryPlugin, hasState bool, marketplaceID string, inventory map[string]claudeplugins.Plugin) (claudeplugins.Plugin, bool, error) {
	if hasState && row.PluginID != "" {
		plugin, err := r.options.Client.GetPlugin(ctx, row.PluginID)
		if err != nil {
			return plugin, false, err
		}
		if err := requirePluginMarketplace(plugin, marketplaceID); err != nil {
			return claudeplugins.Plugin{}, false, err
		}
		return plugin, true, nil
	}
	plugin := inventory[row.PluginName]
	if plugin.ID == "" {
		return plugin, false, nil
	}
	if err := requirePluginMarketplace(plugin, marketplaceID); err != nil {
		return plugin, false, err
	}
	owned := plugin.CreatedBy != nil && plugin.CreatedBy.Type == "api_actor" &&
		plugin.CreatedBy.APIKeyID == r.options.Target.APIKeyID
	return plugin, owned, nil
}

func requirePluginMarketplace(plugin claudeplugins.Plugin, marketplaceID string) error {
	if plugin.MarketplaceID != marketplaceID {
		return &ReconcileError{code: "marketplace.target-changed"}
	}
	return nil
}

func (r *Reconciler) createPlugin(ctx context.Context, state store.ClaudeDeliveryStateStore, row store.ClaudeDeliveryPlugin, marketplaceID string, archive []byte, built BuildResult) (claudeplugins.Plugin, bool, error) {
	if err := r.writeWait(ctx); err != nil {
		return claudeplugins.Plugin{}, false, err
	}
	plugin, err := r.options.Client.CreatePlugin(ctx, archive, marketplaceID, built.ReleaseNotes)
	if err == nil {
		if err := requirePluginMarketplace(plugin, marketplaceID); err != nil {
			return plugin, false, err
		}
		if plugin.CreatedBy == nil || plugin.CreatedBy.Type != "api_actor" ||
			plugin.CreatedBy.APIKeyID != r.options.Target.APIKeyID {
			return claudeplugins.Plugin{}, false, &ReconcileError{code: "plugin.foreign"}
		}
		return plugin, true, nil
	}
	if isThrottled(err) {
		return claudeplugins.Plugin{}, false, err
	}
	var apiErr *claudeplugins.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "skill_name_taken" {
		return claudeplugins.Plugin{}, false, &ReconcileError{code: "plugin.skill-name-taken"}
	}
	if !errors.As(err, &apiErr) || (apiErr.Code != "registration_pending" && apiErr.Code != "plugin_name_taken") {
		return claudeplugins.Plugin{}, false, err
	}
	if apiErr.PluginID == "" {
		return claudeplugins.Plugin{}, false, &ReconcileError{code: "plugin.registration-incomplete"}
	}
	plugin, err = r.options.Client.GetPlugin(ctx, apiErr.PluginID)
	if err != nil {
		return claudeplugins.Plugin{}, false, err
	}
	owned := plugin.CreatedBy != nil && plugin.CreatedBy.Type == "api_actor" &&
		plugin.CreatedBy.APIKeyID == r.options.Target.APIKeyID
	if err := requirePluginMarketplace(plugin, marketplaceID); err != nil {
		return plugin, false, err
	}
	if !owned {
		return plugin, false, &ReconcileError{code: "plugin.foreign"}
	}
	if apiErr.Code == "plugin_name_taken" {
		return plugin, false, nil
	}
	row.PluginID = plugin.ID
	row.TargetMarketplaceID = marketplaceID
	row.LatestVersionID = plugin.LatestVersionID
	row.State = store.ClaudeDeliveryStatePublished
	row.UpdatedAt = r.options.Now()
	if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
		return claudeplugins.Plugin{}, false, &ReconcileError{code: "state.write"}
	}
	version, latest, err := r.uploadVersion(ctx, state, row, plugin, archive, built.Digest, built.ReleaseNotes)
	if err != nil {
		return claudeplugins.Plugin{}, false, err
	}
	plugin.LatestVersionID = latest.LatestVersionID
	plugin.ServedVersionID = latest.ServedVersionID
	plugin.ContentScan = version.ContentScan
	plugin.Reach = version.Reach
	return plugin, true, nil
}

func (r *Reconciler) publishVersion(ctx context.Context, state store.ClaudeDeliveryStateStore, row store.ClaudeDeliveryPlugin, plugin claudeplugins.Plugin, marketplaceID string, contentDigest store.RevisionDigest, archive []byte, built BuildResult) (store.ClaudeDeliveryPlugin, error) {
	row.PluginID = plugin.ID
	row.TargetMarketplaceID = marketplaceID

	if row.PendingArchiveDigest == store.ArchiveDigest(built.Digest) {
		if row.PendingVersionID == "" {
			return row, pendingRegistrationError()
		}
		version, matches, err := r.verifiedVersion(ctx, plugin.ID, row.PendingVersionID, built.Digest)
		if isThrottled(err) {
			return row, err
		}
		if err != nil || !matches || plugin.ServedVersionID != row.PendingVersionID {
			return row, pendingRegistrationError()
		}
		row = publishedRow(row, plugin, version, built, marketplaceID,
			contentDigest, row.InstallationPreference, r.options.Now())
		if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
			return row, &ReconcileError{code: "state.write"}
		}
		return row, nil
	}

	if plugin.LatestVersionID != row.LatestVersionID {
		if plugin.LatestVersionID == "" {
			return row, &ReconcileError{code: "version.latest-missing"}
		}
		version, matches, err := r.verifiedVersion(ctx, plugin.ID, plugin.LatestVersionID, built.Digest)
		if err != nil {
			return row, err
		}
		if matches && plugin.ServedVersionID != version.ID {
			var saveErr error
			row, saveErr = r.savePendingVersion(ctx, state, row, version.ID, built.Digest)
			if saveErr != nil {
				return row, saveErr
			}
			return row, pendingRegistrationError()
		}
		if matches {
			row = publishedRow(row, plugin, version, built, marketplaceID,
				contentDigest, row.InstallationPreference, r.options.Now())
			if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
				return row, &ReconcileError{code: "state.write"}
			}
			return row, nil
		}
	}
	row.State = store.ClaudeDeliveryStatePublished
	row.UpdatedAt = r.options.Now()
	version, latest, err := r.uploadVersion(ctx, state, row, plugin, archive, built.Digest, built.ReleaseNotes)
	if err != nil {
		return row, err
	}
	row = publishedRow(row, latest, version, built, marketplaceID,
		contentDigest, row.InstallationPreference, r.options.Now())
	if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
		return row, &ReconcileError{code: "state.write"}
	}
	return row, nil
}

func (r *Reconciler) uploadVersion(ctx context.Context, state store.ClaudeDeliveryStateStore, row store.ClaudeDeliveryPlugin, plugin claudeplugins.Plugin, archive []byte, archiveDigest, releaseNotes string) (claudeplugins.PluginVersion, claudeplugins.Plugin, error) {
	for attempt := 0; attempt < 2; attempt++ {
		row.LatestVersionID = plugin.LatestVersionID
		row.State = store.ClaudeDeliveryStatePublished
		row.UpdatedAt = r.options.Now()
		if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
			return claudeplugins.PluginVersion{}, claudeplugins.Plugin{}, &ReconcileError{code: "state.write"}
		}
		if err := r.writeWait(ctx); err != nil {
			return claudeplugins.PluginVersion{}, claudeplugins.Plugin{}, err
		}
		version, err := r.options.Client.CreateVersion(ctx, plugin.ID, archive, releaseNotes)
		if err == nil {
			latest, getErr := r.options.Client.GetPlugin(ctx, plugin.ID)
			if getErr != nil {
				_, saveErr := r.savePendingVersion(ctx, state, row, version.ID, archiveDigest)
				if saveErr != nil {
					return claudeplugins.PluginVersion{}, plugin, saveErr
				}
				return claudeplugins.PluginVersion{}, claudeplugins.Plugin{}, getErr
			}
			return version, latest, nil
		}
		if isThrottled(err) {
			return claudeplugins.PluginVersion{}, plugin, err
		}
		var apiErr *claudeplugins.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "skill_name_taken" {
			return claudeplugins.PluginVersion{}, plugin, &ReconcileError{code: "plugin.skill-name-taken"}
		}
		if errors.As(err, &apiErr) && apiErr.Code == "registration_pending" {
			var saveErr error
			row, saveErr = r.savePendingVersion(ctx, state, row, apiErr.PluginVersionID, archiveDigest)
			if saveErr != nil {
				return claudeplugins.PluginVersion{}, plugin, saveErr
			}
			if apiErr.ShouldRetry != nil && *apiErr.ShouldRetry && attempt == 0 {
				latest, readErr := r.options.Client.GetPlugin(ctx, plugin.ID)
				if readErr != nil {
					if isThrottled(readErr) {
						return claudeplugins.PluginVersion{}, plugin, readErr
					}
					return claudeplugins.PluginVersion{}, plugin, pendingRegistrationError()
				}
				plugin = latest
				continue
			}
			return claudeplugins.PluginVersion{}, plugin, pendingRegistrationError()
		}
		latest, readErr := r.options.Client.GetPlugin(ctx, plugin.ID)
		if isThrottled(readErr) {
			return claudeplugins.PluginVersion{}, plugin, readErr
		}
		if readErr != nil {
			return claudeplugins.PluginVersion{}, plugin, err
		}
		if latest.LatestVersionID != plugin.LatestVersionID {
			if latest.LatestVersionID == "" {
				return claudeplugins.PluginVersion{}, plugin, err
			}
			version, matches, verifyErr := r.verifiedVersion(ctx, plugin.ID, latest.LatestVersionID, archiveDigest)
			if verifyErr != nil {
				return claudeplugins.PluginVersion{}, plugin, verifyErr
			}
			if matches {
				if latest.ServedVersionID != version.ID {
					_, saveErr := r.savePendingVersion(ctx, state, row, version.ID, archiveDigest)
					if saveErr != nil {
						return claudeplugins.PluginVersion{}, plugin, saveErr
					}
					return claudeplugins.PluginVersion{}, plugin, pendingRegistrationError()
				}
				return version, latest, nil
			}
			return claudeplugins.PluginVersion{}, plugin, err
		}
		if attempt == 0 && !errors.As(err, &apiErr) {
			plugin = latest
			continue
		}
		return claudeplugins.PluginVersion{}, plugin, err
	}
	return claudeplugins.PluginVersion{}, plugin, &ReconcileError{code: "version.retry-exhausted"}
}

func (r *Reconciler) verifiedVersion(ctx context.Context, pluginID, versionID, archiveDigest string) (claudeplugins.PluginVersion, bool, error) {
	version, err := r.options.Client.GetVersion(ctx, pluginID, versionID)
	if err != nil {
		return claudeplugins.PluginVersion{}, false, err
	}
	if version.ID != versionID {
		return claudeplugins.PluginVersion{}, false, &ReconcileError{code: "version.read-mismatch"}
	}
	return version, strings.HasSuffix(version.ReleaseNotes, "; archive_digest="+archiveDigest), nil
}

func (r *Reconciler) savePendingVersion(ctx context.Context, state store.ClaudeDeliveryStateStore, row store.ClaudeDeliveryPlugin, versionID, archiveDigest string) (store.ClaudeDeliveryPlugin, error) {
	row.PendingVersionID = versionID
	row.PendingArchiveDigest = store.ArchiveDigest(archiveDigest)
	row.State = store.ClaudeDeliveryStatePublished
	row.UpdatedAt = r.options.Now()
	if err := state.UpsertClaudeDeliveryPublished(ctx, row); err != nil {
		return row, &ReconcileError{code: "state.write"}
	}
	return row, nil
}

func pendingRegistrationError() error {
	return &ReconcileError{code: "claude-api.registration_pending"}
}

func (r *Reconciler) writeWait(ctx context.Context) error {
	if err := r.options.Limiter.Wait(ctx, minimumWriteInterval); err != nil {
		return fmt.Errorf("%w: %v", errWriteRateLimit, err)
	}
	return nil
}

func (r *Reconciler) setInstallationPreference(ctx context.Context, pluginID, preference string) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.writeWait(ctx); err != nil {
			return err
		}
		_, err := r.options.Client.SetInstallationSetting(ctx, pluginID, preference)
		if err == nil {
			return nil
		}
		if isThrottled(err) {
			return err
		}
		var apiErr *claudeplugins.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
			return err
		}
	}
	return &ReconcileError{code: "installation-setting.unavailable"}
}

func (r *Reconciler) recordFailure(ctx context.Context, state store.ClaudeDeliveryStateStore, row store.ClaudeDeliveryPlugin, marketplaceID, code string) error {
	if err := state.RecordClaudeDeliveryFailure(ctx, row.Coordinate, row.PluginName, marketplaceID, code, r.options.Now()); err != nil {
		return &ReconcileError{code: "state.write"}
	}
	return nil
}

func (r *Reconciler) recordThrottleFailure(
	ctx context.Context,
	state store.ClaudeDeliveryStateStore,
	row store.ClaudeDeliveryPlugin,
	marketplaceID string,
	result *ReconcileResult,
	err error,
) (bool, error) {
	if !isThrottled(err) {
		return false, nil
	}
	if saveErr := r.recordFailure(ctx, state, row, marketplaceID, deliveryErrorCode(err)); saveErr != nil {
		return true, saveErr
	}
	result.Failed++
	return true, err
}

func isThrottled(err error) bool {
	var throttle *claudeplugins.ThrottledError
	return errors.As(err, &throttle)
}

func createdPluginRow(row store.ClaudeDeliveryPlugin, plugin claudeplugins.Plugin, contentDigest store.RevisionDigest, built BuildResult, marketplaceID, preference string, now time.Time) store.ClaudeDeliveryPlugin {
	row.PluginID = plugin.ID
	row.TargetMarketplaceID = marketplaceID
	row.PublishedArchiveDigest = store.ArchiveDigest(built.Digest)
	row.PublishedContentDigest = contentDigest
	row.PublishedVersionID = plugin.LatestVersionID
	row.LatestVersionID = plugin.LatestVersionID
	row.ServedVersionID = plugin.ServedVersionID
	row.ContentScanStatus = plugin.ContentScan.Status
	row.Reach = plugin.Reach
	row.InstallationPreference = preference
	row.State = store.ClaudeDeliveryStatePublished
	row.UpdatedAt = now
	return row
}

func publishedRow(row store.ClaudeDeliveryPlugin, plugin claudeplugins.Plugin, version claudeplugins.PluginVersion, built BuildResult, marketplaceID string, contentDigest store.RevisionDigest, preference string, now time.Time) store.ClaudeDeliveryPlugin {
	row.PluginID = plugin.ID
	row.TargetMarketplaceID = marketplaceID
	row.PublishedArchiveDigest = store.ArchiveDigest(built.Digest)
	row.PublishedContentDigest = contentDigest
	row.PendingVersionID = ""
	row.PendingArchiveDigest = ""
	row.PublishedVersionID = version.ID
	row.LatestVersionID = plugin.LatestVersionID
	row.ServedVersionID = plugin.ServedVersionID
	row.ContentScanStatus = version.ContentScan.Status
	row.Reach = version.Reach
	row.InstallationPreference = preference
	row.State = store.ClaudeDeliveryStatePublished
	row.LastErrorCode = ""
	row.LastErrorMessage = ""
	row.UpdatedAt = now
	return row
}

func deliveryErrorCode(err error) string {
	if err == nil {
		return "delivery.failed"
	}
	if isThrottled(err) {
		return "claude-api.rate-limited"
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	var apiErr *claudeplugins.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "plugin_name_taken", "registration_pending", "scan_pending", "skill_name_taken", "rate_limited":
			return "claude-api." + apiErr.Code
		}
		return fmt.Sprintf("claude-api.http-%d", apiErr.StatusCode)
	}
	return "delivery.failed"
}

var errWriteRateLimit = errors.New("claude delivery write limiter stopped the pass")

type writePacer struct {
	mu   sync.Mutex
	last time.Time
}

func (p *writePacer) Wait(ctx context.Context, interval time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !p.last.IsZero() {
		delay := p.last.Add(interval).Sub(now)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	p.last = time.Now()
	return ctx.Err()
}
