package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudedelivery"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudeplugins"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const claudeDeliveryJobKind = store.ClaudeDeliveryJobKind

func claudeEnterpriseEnabled(cfg config.Config) bool {
	return cfg.ClaudeEnterprise != nil && cfg.ClaudeEnterprise.Enabled
}

func claudeDeliveryJob(cfg config.Config, trigger string) (store.Job, error) {
	marketplace := store.MarketplaceID(cfg.Instance.ID)
	payload, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: marketplace})
	if err != nil {
		return store.Job{}, err
	}
	idempotency := "claude-delivery:" + string(marketplace) + ":" + trigger
	sum := sha256.Sum256([]byte(idempotency))
	id := "claude-delivery-" + hex.EncodeToString(sum[:])
	return store.Job{
		ID: store.JobID(id), Kind: claudeDeliveryJobKind, Payload: payload,
		IdempotencyKey: idempotency, MaxAttempts: 8,
	}, nil
}

func enqueueClaudeDelivery(ctx context.Context, deps Deps, trigger string) (store.JobID, error) {
	cfg := runtimeSnapshot(deps, deps.Config).Config
	if !claudeEnterpriseEnabled(cfg) {
		return "", nil
	}
	j, err := claudeDeliveryJob(cfg, trigger)
	if err != nil {
		return "", err
	}
	if err := deps.MetadataStore.EnqueueJob(ctx, j); errors.Is(err, store.ErrConflict) {
		return j.ID, nil
	} else if err != nil {
		return "", err
	}
	return j.ID, nil
}

func runClaudeDeliveryWorker(ctx context.Context, deps Deps) {
	jobs, ok := deps.MetadataStore.(store.ClaudeDeliveryJobStore)
	if !ok {
		return
	}
	workerID := "claude-delivery-" + uuid.NewString()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastConfig *config.ClaudeEnterprise
	var nextRepair time.Time
	var repairInterval time.Duration
	for {
		cfg := runtimeSnapshot(deps, deps.Config).Config
		current := cfg.ClaudeEnterprise
		if !reflect.DeepEqual(lastConfig, current) {
			if claudeEnterpriseEnabled(cfg) {
				if _, err := enqueueClaudeDelivery(ctx, deps, "configuration-"+uuid.NewString()); err != nil {
					slog.ErrorContext(ctx, "Claude delivery enqueue failed", "failure_code", "job.enqueue")
				}
				repairInterval = current.RepairInterval()
				nextRepair = time.Now().Add(repairInterval)
			} else {
				nextRepair = time.Time{}
			}
			if current == nil {
				lastConfig = nil
			} else {
				copy := *current
				lastConfig = &copy
			}
		} else if claudeEnterpriseEnabled(cfg) && nextRepair.IsZero() {
			repairInterval = current.RepairInterval()
			nextRepair = time.Now().Add(repairInterval)
		} else if claudeEnterpriseEnabled(cfg) && current.RepairInterval() != repairInterval {
			repairInterval = current.RepairInterval()
			nextRepair = time.Now().Add(repairInterval)
		}
		if claudeEnterpriseEnabled(cfg) && !time.Now().Before(nextRepair) {
			if _, err := enqueueClaudeDelivery(ctx, deps, fmt.Sprintf("repair-%d", time.Now().UnixNano())); err != nil {
				slog.ErrorContext(ctx, "Claude delivery enqueue failed", "failure_code", "job.enqueue")
			}
			nextRepair = time.Now().Add(current.RepairInterval())
		}

		if ctx.Err() != nil {
			return
		}
		if claudeEnterpriseEnabled(cfg) {
			j, err := jobs.LeaseClaudeDeliveryJob(ctx, store.MarketplaceID(cfg.Instance.ID), "", workerID, managedLeaseDuration(cfg))
			if err == nil {
				if _, err := processClaudeDelivery(ctx, deps, j, workerID); err != nil {
					slog.WarnContext(ctx, "Claude delivery job failed", "failure_code", deliveryFailureCode(err))
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				slog.ErrorContext(ctx, "Claude delivery lease failed", "failure_code", "job.lease")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processClaudeDelivery(ctx context.Context, deps Deps, j store.Job, workerID string) (claudedelivery.ReconcileResult, error) {
	var payload store.ClaudeDeliveryJobPayload
	if json.Unmarshal(j.Payload, &payload) != nil ||
		payload.Marketplace != store.MarketplaceID(deps.Config.Instance.ID) {
		return claudedelivery.ReconcileResult{}, finishClaudeDelivery(ctx, deps, j, workerID, errors.New("job.payload"))
	}
	if err := managedLease(ctx, deps.MetadataStore, j, workerID); err != nil {
		return claudedelivery.ReconcileResult{}, err
	}
	cfg := runtimeSnapshot(deps, deps.Config).Config
	if !claudeEnterpriseEnabled(cfg) {
		return claudedelivery.ReconcileResult{}, finishClaudeDelivery(ctx, deps, j, workerID, nil)
	}
	client, err := newClaudePluginsClient(cfg.ClaudeEnterprise, deps.Metrics)
	if err != nil {
		return claudedelivery.ReconcileResult{}, finishClaudeDelivery(ctx, deps, j, workerID, err)
	}
	registries, err := claudeDeliveryRegistries(ctx, deps, cfg)
	if err != nil {
		return claudedelivery.ReconcileResult{}, finishClaudeDelivery(ctx, deps, j, workerID, errors.New("configuration.invalid"))
	}
	target := cfg.ClaudeEnterprise
	reconciler := claudedelivery.NewReconciler(claudedelivery.ReconcileOptions{
		Metadata: deps.MetadataStore, Blobs: deps.BlobStore, Client: client,
		SourceMarketplaceID: store.MarketplaceID(cfg.Instance.ID), RegistryIDs: registries,
		Target: claudedelivery.ReconcileTarget{
			APIKeyID: target.APIKeyID, MarketplaceID: target.MarketplaceID,
			PluginNamePrefix:              target.PluginPrefix(),
			DefaultInstallationPreference: target.InstallationPreference(),
		},
	})
	started := time.Now()
	workCtx, cancel := context.WithDeadline(ctx, j.LeaseUntil)
	defer cancel()
	result, reconcileErr := reconciler.Reconcile(workCtx)
	outcome := "succeeded"
	if reconcileErr != nil {
		outcome = "failed"
	}
	if deps.Metrics != nil {
		deps.Metrics.RecordClaudeDeliveryReconcile(outcome, time.Since(started))
		recordClaudeDeliveryStateCounts(ctx, deps)
	}
	if err := finishClaudeDelivery(ctx, deps, j, workerID, reconcileErr); err != nil {
		return result, err
	}
	return result, nil
}

func finishClaudeDelivery(ctx context.Context, deps Deps, j store.Job, workerID string, cause error) error {
	err := deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := managedLease(ctx, tx, j, workerID); err != nil {
			return err
		}
		if cause == nil {
			return tx.CompleteJob(ctx, j.ID, workerID)
		}
		backoff := max(job.BaseBackoff, job.NextRetryDelay(j.Attempt))
		return tx.FailJob(ctx, j.ID, workerID, backoff)
	})
	if err != nil {
		return err
	}
	return cause
}

func newClaudePluginsClient(cfg *config.ClaudeEnterprise, metrics *Metrics) (*claudeplugins.Client, error) {
	if cfg == nil {
		return nil, errors.New("configuration.disabled")
	}
	key, err := resolveClaudeEnterpriseAPIKey(cfg.APIKey)
	if err != nil {
		return nil, errors.New("credentials.unavailable")
	}
	return claudeplugins.NewClient(claudeplugins.Options{
		BaseURL: claudePluginsEndpoint(), APIKey: claudeplugins.NewAPIKey(key),
		OnResponse: func(endpoint string, status int) {
			if metrics == nil {
				return
			}
			metrics.RecordClaudeAPIRequest(endpoint, status)
			if status == 429 {
				metrics.RecordClaudeDeliveryThrottled()
			}
		},
	})
}

func resolveClaudeEnterpriseAPIKey(reference config.ClaudeEnterpriseAPIKeyReference) (string, error) {
	var value string
	switch {
	case strings.TrimSpace(reference.File) != "":
		contents, err := os.ReadFile(strings.TrimSpace(reference.File))
		if err != nil {
			return "", errors.New("API key file unavailable")
		}
		value = string(contents)
	case strings.TrimSpace(reference.Env) != "":
		resolved, ok := os.LookupEnv(strings.TrimSpace(reference.Env))
		if !ok {
			return "", errors.New("API key environment value unavailable")
		}
		value = resolved
	default:
		return "", errors.New("API key reference unavailable")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("API key is empty")
	}
	return value, nil
}

func claudeDeliveryRegistries(ctx context.Context, deps Deps, cfg config.Config) ([]store.RegistryID, error) {
	if cfg.Management.Mode != "database" {
		ids := make([]store.RegistryID, 0, len(cfg.Registries))
		for _, registry := range cfg.Registries {
			ids = append(ids, store.RegistryID(registry.ID))
		}
		return ids, nil
	}
	view, err := managedView(ctx, deps)
	if err != nil {
		return nil, err
	}
	ids := make([]store.RegistryID, 0, len(view.Snapshot.Registries))
	for _, registry := range view.Snapshot.Registries {
		ids = append(ids, registry.ID)
	}
	return ids, nil
}

func recordClaudeDeliveryStateCounts(ctx context.Context, deps Deps) {
	state, ok := deps.MetadataStore.(store.ClaudeDeliveryStateStore)
	if !ok {
		return
	}
	rows, err := state.ListClaudeDeliveryPlugins(ctx)
	if err != nil {
		return
	}
	counts := make(map[string]int)
	for _, row := range rows {
		if row.Coordinate.Marketplace == store.MarketplaceID(deps.Config.Instance.ID) {
			counts[string(row.State)]++
		}
	}
	deps.Metrics.SetClaudeDeliveryPluginStates(counts)
}

func deliveryFailureCode(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && coded.ErrorCode() != "" {
		return coded.ErrorCode()
	}
	return "delivery.failed"
}

func RunClaudeDeliveryOnce(ctx context.Context, deps Deps) (claudedelivery.ReconcileResult, bool, error) {
	cfg := runtimeSnapshot(deps, deps.Config).Config
	if !claudeEnterpriseEnabled(cfg) {
		return claudedelivery.ReconcileResult{}, false, nil
	}
	jobs, ok := deps.MetadataStore.(store.ClaudeDeliveryJobStore)
	if !ok {
		return claudedelivery.ReconcileResult{}, true, store.ErrTopology
	}
	deps.Config = cfg
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		return claudedelivery.ReconcileResult{}, true, err
	}
	if err := EnsureRevocations(ctx, deps); err != nil {
		return claudedelivery.ReconcileResult{}, true, err
	}
	trigger := "operator-" + uuid.NewString()
	id, err := enqueueClaudeDelivery(ctx, deps, trigger)
	if err != nil {
		return claudedelivery.ReconcileResult{}, true, err
	}
	workerID := "claude-delivery-cli-" + uuid.NewString()
	j, err := jobs.LeaseClaudeDeliveryJob(ctx, store.MarketplaceID(cfg.Instance.ID), id, workerID, managedLeaseDuration(cfg))
	if errors.Is(err, store.ErrNotFound) {
		return claudedelivery.ReconcileResult{}, true, fmt.Errorf("delivery job remains queued: %w", err)
	}
	if err != nil {
		return claudedelivery.ReconcileResult{}, true, err
	}
	result, err := processClaudeDelivery(ctx, deps, j, workerID)
	return result, true, err
}
