// Package worker implements the bounded Marketplace indexing scheduler.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/revocation"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Deps carries the runtime dependencies for worker operations.
type Deps struct {
	Config        config.Config
	Marketplace   store.Marketplace
	MetadataStore store.MetadataStore
	BlobStore     store.BlobStore
	ReindexActor  store.ManagedActor
	ResolveRef    func(context.Context, string, string, acquire.Credentials, time.Duration) (string, error)
	Clone         func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error)
}

// Refresh performs a single refresh of the configured registry.
// It upserts the registry record, clones the repository, builds a registry revision,
// and publishes it atomically. The registry status is updated to reflect the
// outcome of the refresh.
func Refresh(ctx context.Context, deps Deps, registry store.Registry) error {
	if err := checkAuthority(ctx, deps); err != nil {
		return err
	}
	if deps.Config.Management.Mode == "database" {
		return reindexManaged(ctx, deps, string(registry.ID))
	}
	cfgRegistry, ok := registryByID(deps.Config, string(registry.ID))
	if !ok {
		return fmt.Errorf("registry %q is not present in the operator config", registry.ID)
	}

	existing, err := deps.MetadataStore.GetRegistry(ctx, registry.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load registry %q: %w", registry.ID, err)
	}

	merged := mergeStoreRegistry(existing, cfgRegistry, deps.Config.AcquisitionDefault)
	if err := deps.MetadataStore.PutRegistry(ctx, merged); err != nil {
		return fmt.Errorf("upsert registry %q: %w", registry.ID, err)
	}

	if len(merged.Origins) == 0 {
		return fmt.Errorf("registry %q has no origin", registry.ID)
	}
	expectedHead := ""
	head, err := deps.MetadataStore.LatestRegistryOccurrence(ctx, registry.ID)
	if err == nil {
		expectedHead = head.Commit
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load registry %q head: %w", registry.ID, err)
	}
	origin := merged.Origins[len(merged.Origins)-1]

	creds, err := toAcquireCredentials(cfgRegistry.Credentials, cfgRegistry.CAFile)
	if err != nil {
		return fmt.Errorf("resolve credentials for registry %q: %w", registry.ID, err)
	}

	limits := acquire.Limits{
		RepositoryBytes: deps.Config.Policy.Limits.RepositoryBytes,
		FileCount:       deps.Config.Policy.Limits.FileCount,
		FileBytes:       deps.Config.Policy.Limits.FileBytes,
	}
	roots := make([]string, len(merged.Roots))
	for i := range merged.Roots {
		roots[i] = merged.Roots[i].Path
	}

	clone := deps.Clone
	if clone == nil {
		clone = acquire.Clone
	}
	res, err := clone(ctx, origin, merged.Ref, creds, limits, deps.Config.Policy.Limits.CloneDuration.Duration(), roots...)
	if err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}
	defer func() { _ = res.Release() }()

	rev, err := ingest.BuildRegistryRevision(ctx, deps.Config, deps.Marketplace.ID, merged, res.Dir, res.Commit)
	if err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}
	if err := ingest.ReuseStoredArchives(ctx, deps.MetadataStore, deps.BlobStore, &rev); err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}

	pub := store.RegistryRevisionPublication{
		Occurrence:          rev.Occurrence,
		ExpectedHeadCommit:  expectedHead,
		Resources:           rev.Resources,
		ResourceOccurrences: rev.ResourceOccurrences,
		Assessments:         rev.Assessments,
		Collections:         rev.Collections,
		ManifestPayload:     rev.ManifestPayload,
		ExtraBlobs:          rev.ExtraBlobs,
		InspectionBlobs:     rev.InspectionBlobs,
	}
	if err := store.PublishRegistryRevision(ctx, deps.MetadataStore, deps.BlobStore, pub); err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}

	slog.Info("refreshed registry", "registry", registry.ID, "commit", res.Commit, "resources", len(rev.Resources))
	return recordRefreshSuccess(ctx, deps.MetadataStore, merged)
}

// EnsureMarketplace loads or creates the Marketplace instance record.
func EnsureMarketplace(ctx context.Context, deps *Deps) error {
	if err := checkAuthority(ctx, *deps); err != nil {
		return err
	}
	mp, err := deps.MetadataStore.GetMarketplace(ctx, store.MarketplaceID(deps.Config.Instance.ID))
	if deps.Config.Management.Mode == "database" {
		if err != nil {
			return fmt.Errorf("load managed marketplace: %w", err)
		}
		deps.Marketplace = mp
		return nil
	}
	if err == nil {
		if mp.Name != deps.Config.Instance.Name || mp.CanonicalURL != deps.Config.Instance.CanonicalURL {
			mp.Name = deps.Config.Instance.Name
			mp.CanonicalURL = deps.Config.Instance.CanonicalURL
			if err := deps.MetadataStore.PutMarketplace(ctx, mp); err != nil {
				return fmt.Errorf("update marketplace: %w", err)
			}
		}
		deps.Marketplace = mp
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load marketplace: %w", err)
	}

	mp = store.Marketplace{
		ID:           store.MarketplaceID(deps.Config.Instance.ID),
		Name:         deps.Config.Instance.Name,
		CanonicalURL: deps.Config.Instance.CanonicalURL,
	}
	if err := deps.MetadataStore.PutMarketplace(ctx, mp); err != nil {
		return fmt.Errorf("create marketplace: %w", err)
	}
	deps.Marketplace = mp
	slog.Info("created marketplace", "id", mp.ID, "name", mp.Name)
	return nil
}

// EnsureRevocations reconciles the complete configured desired set. The active
// table is deliberately separate from immutable resource revisions: withdrawal
// removes only the active declaration and never edits revision/history rows.
func EnsureRevocations(ctx context.Context, deps Deps) error {
	if err := checkAuthority(ctx, deps); err != nil {
		return err
	}
	evaluator, err := revocation.New(deps.Config)
	if err != nil {
		return fmt.Errorf("build revocation evaluator: %w", err)
	}
	desired := evaluator.Declarations()
	desiredByKey := make(map[string]struct{}, len(desired))
	for _, item := range desired {
		desiredByKey[revocationStoreKey(item)] = struct{}{}
	}
	now := time.Now().UTC()
	slog.Info("reconciling Marketplace revocations", "desired", len(desired))
	return deps.MetadataStore.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		for _, kind := range []store.RevocationKind{store.RevocationRegistry, store.RevocationCoordinate, store.RevocationRevision} {
			active, listErr := tx.ListRevocations(txCtx, kind)
			if listErr != nil {
				return fmt.Errorf("list active %s revocations: %w", kind, listErr)
			}
			for _, current := range active {
				if _, wanted := desiredByKey[revocationStoreKey(current)]; wanted {
					continue
				}
				if deleteErr := tx.DeleteRevocation(txCtx, current.Kind, current.TargetID); deleteErr != nil {
					return fmt.Errorf("withdraw %s revocation %q: %w", current.Kind, current.TargetID, deleteErr)
				}
			}
		}
		for _, item := range desired {
			item.AppliedAt = now
			if putErr := tx.PutRevocation(txCtx, item); putErr != nil {
				return fmt.Errorf("apply %s revocation %q: %w", item.Kind, item.TargetID, putErr)
			}
		}
		return nil
	})
}

func revocationStoreKey(r store.Revocation) string { return string(r.Kind) + "\x00" + r.TargetID }

// Run starts the background indexing worker, observes configured sources, and
// processes refresh jobs until ctx is cancelled.
func Run(ctx context.Context, deps Deps, cfg config.Config) error {
	deps.Config = cfg
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		return err
	}
	if err := EnsureRevocations(ctx, deps); err != nil {
		slog.Error("failed to apply revocations", "err", err)
		return err
	}

	if deps.Config.Management.Mode == "database" {
		return runManaged(ctx, deps)
	}
	if err := initializeObservationState(ctx, deps, cfg); err != nil {
		return err
	}

	var wg sync.WaitGroup
	concurrency := cfgRefreshConcurrency(cfg)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			workerLoop(ctx, deps, workerIndex)
		}(i)
	}

	observer := newObserver(deps, cfg, time.Now)
	ticker := time.NewTicker(observationPollDelay)
	defer ticker.Stop()

	for {
		if err := observer.runCycle(ctx); err != nil && ctx.Err() == nil {
			slog.Error("source observation cycle failed", "err", err)
		}

		select {
		case <-ctx.Done():
			ticker.Stop()
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

const (
	observationConcurrency = 4
	observationBatchSize   = 128
	observationPollDelay   = 100 * time.Millisecond
	registryLeaseDuration  = 2 * time.Minute
	hostBudgetDelay        = 100 * time.Millisecond
)

func sourceHostBudgetLimit() store.HostBudgetLimit {
	return store.HostBudgetLimit{Operations: 2, Acquisitions: 1}
}

type observer struct {
	deps  Deps
	cfg   config.Config
	now   func() time.Time
	owner string
}

func newObserver(deps Deps, cfg config.Config, now func() time.Time) *observer {
	if now == nil {
		now = time.Now
	}
	return &observer{deps: deps, cfg: cfg, now: now, owner: "observer-" + uuid.NewString()}
}

func initializeObservationState(ctx context.Context, deps Deps, cfg config.Config) error {
	for _, src := range cfg.Registries {
		existing, err := deps.MetadataStore.GetRegistry(ctx, store.RegistryID(src.ID))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("load registry %q: %w", src.ID, err)
		}
		merged := mergeStoreRegistry(existing, src, cfg.AcquisitionDefault)
		if err := deps.MetadataStore.PutRegistry(ctx, merged); err != nil {
			return fmt.Errorf("upsert registry %q: %w", src.ID, err)
		}
		if err := ensureObservationRecord(ctx, deps.MetadataStore, store.RegistryID(src.ID)); err != nil {
			return fmt.Errorf("initialize observation for registry %q: %w", src.ID, err)
		}
	}
	return nil
}

func ensureObservationRecord(ctx context.Context, meta store.MetadataStore, id store.RegistryID) error {
	_, err := meta.GetRegistryObservation(ctx, id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err = meta.CreateRegistryObservation(ctx, store.RegistryObservation{RegistryID: id})
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	return err
}

func (o *observer) runCycle(ctx context.Context) error {
	now := o.now()
	due, err := o.deps.MetadataStore.ListDueRegistryObservations(ctx, now, observationBatchSize)
	if err != nil {
		return fmt.Errorf("list due registry observations: %w", err)
	}
	if len(due) == 0 {
		return nil
	}

	sem := make(chan struct{}, observationConcurrency)
	errs := make(chan error, len(due))
	var wg sync.WaitGroup
	for _, item := range due {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(observation store.RegistryObservation) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := o.observeOne(ctx, observation); err != nil {
				errs <- err
			}
		}(item)
	}
	wg.Wait()
	close(errs)
	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (o *observer) observeOne(ctx context.Context, due store.RegistryObservation) error {
	id := due.RegistryID
	owner := o.owner + "-" + uuid.NewString()
	lease, err := o.deps.MetadataStore.AcquireRegistryLease(ctx, id, owner, registryLeaseDuration)
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lease registry %q for observation: %w", id, err)
	}
	defer func() {
		if err := o.deps.MetadataStore.ReleaseRegistryLease(ctx, id, lease); err != nil && ctx.Err() == nil {
			slog.Warn("release registry observation lease failed", "registry", id, "err", err)
		}
	}()

	current, err := o.deps.MetadataStore.GetRegistryObservation(ctx, id)
	if err != nil {
		return fmt.Errorf("load observation for registry %q: %w", id, err)
	}
	now := o.now().UTC()
	if current.NextProbeAt.After(now) {
		return nil
	}
	src, ok := registryByID(o.cfg, string(id))
	if !ok {
		nextProbe := nextPhaseSlot(id, now, o.cfg.Refresh.Interval.Duration())
		if _, err := mutateObservation(ctx, o.deps.MetadataStore, id, func(next *store.RegistryObservation) {
			next.NextProbeAt = nextProbe
		}); err != nil {
			return fmt.Errorf("advance observation for inactive registry %q: %w", id, err)
		}
		slog.Debug("source observation skipped: registry not in startup config", "registry", id)
		return nil
	}
	started := time.Now()
	origin := src.Origin
	commit := src.Ref
	var probeErr error
	if !acquire.IsFullCommit(src.Ref) {
		host, hostErr := acquire.SourceHost(origin)
		if hostErr != nil {
			probeErr = hostErr
		} else {
			hostLease, budgetErr := o.deps.MetadataStore.AcquireHostBudget(ctx, host, owner, false, sourceHostBudgetLimit(), registryLeaseDuration)
			if errors.Is(budgetErr, store.ErrConflict) {
				overdue := time.Duration(0)
				if !current.NextProbeAt.IsZero() {
					overdue = max(now.Sub(current.NextProbeAt), 0)
				}
				slog.Info("source observation delayed by host budget",
					"registry", id, "host", redact.URL(host), "overdue", overdue,
					"origin", redact.URL(origin))
				return nil
			}
			if budgetErr != nil {
				return fmt.Errorf("admit source observation for registry %q: %w", id, budgetErr)
			}
			resolve := o.deps.ResolveRef
			if resolve == nil {
				resolve = acquire.ResolveRemoteRef
			}
			creds, credErr := toAcquireCredentials(src.Credentials, src.CAFile)
			if credErr != nil {
				probeErr = credErr
			} else {
				commit, probeErr = resolve(ctx, origin, src.Ref, creds, config.DefaultObservationProbeTimeout)
			}
			if releaseErr := o.deps.MetadataStore.ReleaseHostBudget(ctx, hostLease); releaseErr != nil && probeErr == nil {
				probeErr = fmt.Errorf("release host budget: %w", releaseErr)
			}
		}
	}
	now = o.now().UTC()
	duration := time.Since(started)
	if probeErr != nil {
		if err := recordProbeFailure(ctx, o.deps.MetadataStore, id, now, o.cfg.Refresh.Interval.Duration(), probeErr); err != nil {
			return fmt.Errorf("record probe failure for registry %q: %w", id, err)
		}
		slog.Warn("source observation",
			"registry", id, "outcome", "failed", "origin", redact.URL(origin),
			"duration", duration, "error", boundedLogError(probeErr))
		return nil
	}

	generation, err := startupGeneration(src, o.cfg)
	if err != nil {
		return fmt.Errorf("compute startup generation for registry %q: %w", id, err)
	}
	reconciliationDue := !current.NextReconcileAt.IsZero() && !now.Before(current.NextReconcileAt)
	nextProbe := nextPhaseSlot(id, now, o.cfg.Refresh.Interval.Duration())
	_, err = mutateObservation(ctx, o.deps.MetadataStore, id, func(next *store.RegistryObservation) {
		next.ObservedCommit = commit
		next.ObservedAt = now
		next.ProbeStatus = store.RegistryProbeStatusOK
		next.ProbeError = ""
		next.ConsecutiveProbeFailures = 0
		next.NextProbeAt = nextProbe
		if next.NextReconcileAt.IsZero() || reconciliationDue {
			next.NextReconcileAt = nextPhaseSlot(id, now, config.DefaultObservationReconciliationInterval)
		}
	})
	if err != nil {
		return fmt.Errorf("record source observation for registry %q: %w", id, err)
	}

	trigger := ""
	switch {
	case commit != current.SuccessfulCommit:
		trigger = "source-change"
	case generation != current.SuccessfulGeneration:
		trigger = "config-change"
	case reconciliationDue:
		trigger = "reconciliation"
	}
	outcome := "unchanged"
	if commit != current.ObservedCommit {
		outcome = "changed"
	}
	slog.Info("source observation",
		"registry", id, "outcome", outcome, "commit", commit,
		"origin", redact.URL(origin), "duration", duration)
	if trigger == "" {
		return nil
	}
	if _, err := requestRefresh(ctx, o.deps, src, commit, generation, trigger); err != nil {
		return fmt.Errorf("request %s refresh for registry %q: %w", trigger, id, err)
	}
	return nil
}

func workerLoop(ctx context.Context, deps Deps, workerIndex int) {
	workerID := fmt.Sprintf("worker-%d-%s", workerIndex, uuid.NewString())
	leaseDuration := refreshLeaseDuration(deps.Config)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		j, err := job.Lease(ctx, deps.MetadataStore, "refresh", workerID, leaseDuration)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			slog.Error("lease job failed", "worker", workerID, "err", err)
			time.Sleep(time.Second)
			continue
		}

		if err := processRefreshJob(ctx, deps, j, workerID); err != nil {
			slog.Error("process refresh job failed", "worker", workerID, "job", j.ID, "err", err)
		}
	}
}

func processRefreshJob(ctx context.Context, deps Deps, j job.Job, workerID string) error {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in refresh job", "job", j.ID, "recover", r)
			_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		}
	}()

	var payload refreshPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("decode job payload: %w", err)
	}

	registry, err := loadRegistryForJob(ctx, deps, payload.RegistryID)
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return err
	}

	leaseDuration := refreshLeaseDuration(deps.Config)
	registryLease, err := deps.MetadataStore.AcquireRegistryLease(ctx, registry.ID, workerID, leaseDuration)
	if errors.Is(err, store.ErrConflict) {
		return releaseRefreshAndPause(ctx, deps, j.ID, workerID)
	}
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("lease registry %q for refresh: %w", registry.ID, err)
	}
	defer func() {
		if err := deps.MetadataStore.ReleaseRegistryLease(ctx, registry.ID, registryLease); err != nil && ctx.Err() == nil {
			slog.Warn("release registry refresh lease failed", "registry", registry.ID, "err", err)
		}
	}()

	if len(registry.Origins) == 0 {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("registry %q has no origin", registry.ID)
	}
	origin := registry.Origins[len(registry.Origins)-1]
	host, err := acquire.SourceHost(origin)
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("resolve source host for registry %q: %w", registry.ID, err)
	}
	hostLease, err := deps.MetadataStore.AcquireHostBudget(ctx, host, workerID, true, sourceHostBudgetLimit(), leaseDuration)
	if errors.Is(err, store.ErrConflict) {
		overdue := time.Duration(0)
		if !j.CreatedAt.IsZero() {
			overdue = max(time.Since(j.CreatedAt), 0)
		}
		slog.Info("refresh delayed by host budget", "registry", registry.ID,
			"host", redact.URL(host), "overdue", overdue, "job", j.ID)
		return releaseRefreshAndPause(ctx, deps, j.ID, workerID)
	}
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("admit acquisition for registry %q: %w", registry.ID, err)
	}
	defer func() {
		if err := deps.MetadataStore.ReleaseHostBudget(ctx, hostLease); err != nil && ctx.Err() == nil {
			slog.Warn("release source host budget failed", "registry", registry.ID, "host", redact.URL(host), "err", err)
		}
	}()

	if err := Refresh(ctx, deps, registry); err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return err
	}
	head, err := deps.MetadataStore.LatestRegistryOccurrence(ctx, registry.ID)
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("load published commit for registry %q: %w", registry.ID, err)
	}
	if payload.Generation == "" {
		src, ok := registryByID(deps.Config, payload.RegistryID)
		if !ok {
			_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
			return fmt.Errorf("registry %q is not in startup config", payload.RegistryID)
		}
		payload.Generation, err = startupGeneration(src, deps.Config)
		if err != nil {
			_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
			return fmt.Errorf("compute successful generation for registry %q: %w", registry.ID, err)
		}
	}
	if err := recordSuccessfulIndex(ctx, deps.MetadataStore, registry.ID, head.Commit, payload.Generation, time.Now().UTC()); err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return fmt.Errorf("record successful index for registry %q: %w", registry.ID, err)
	}

	if err := job.Complete(ctx, deps.MetadataStore, j.ID, workerID); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

func refreshLeaseDuration(cfg config.Config) time.Duration {
	duration := 5 * time.Minute
	if cloneDuration := cfg.Policy.Limits.CloneDuration.Duration(); cloneDuration > 0 && cloneDuration > duration {
		duration = cloneDuration + 5*time.Minute
	}
	return duration
}

func releaseRefreshAndPause(ctx context.Context, deps Deps, id store.JobID, workerID string) error {
	if err := job.Release(ctx, deps.MetadataStore, id, workerID); err != nil {
		return fmt.Errorf("release refresh job %q after admission refusal: %w", id, err)
	}
	timer := time.NewTimer(hostBudgetDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func loadRegistryForJob(ctx context.Context, deps Deps, id string) (store.Registry, error) {
	cfgRegistry, ok := registryByID(deps.Config, id)
	if !ok {
		return store.Registry{}, fmt.Errorf("registry %q not in config", id)
	}

	existing, err := deps.MetadataStore.GetRegistry(ctx, store.RegistryID(id))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Registry{}, fmt.Errorf("get registry %q: %w", id, err)
	}
	return mergeStoreRegistry(existing, cfgRegistry, deps.Config.AcquisitionDefault), nil
}

// Reindex performs a one-shot durable enqueue of the requested registry and
// processes refresh jobs until the requested registry's refresh is complete.
func Reindex(ctx context.Context, deps Deps, cfg config.Config, registrySlugOrID string) error {
	deps.Config = cfg
	if err := checkAuthority(ctx, deps); err != nil {
		return err
	}
	if deps.Config.Management.Mode == "database" {
		return reindexManaged(ctx, deps, registrySlugOrID)
	}
	cfgRegistry, ok := registryBySlugOrID(cfg, registrySlugOrID)
	if !ok {
		return fmt.Errorf("registry %q not found in config", registrySlugOrID)
	}

	if deps.Marketplace.ID == "" {
		if err := EnsureMarketplace(ctx, &deps); err != nil {
			return err
		}
	}

	existing, err := deps.MetadataStore.GetRegistry(ctx, store.RegistryID(cfgRegistry.ID))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get registry %q: %w", cfgRegistry.ID, err)
	}

	registry := mergeStoreRegistry(existing, cfgRegistry, cfg.AcquisitionDefault)
	if err := deps.MetadataStore.PutRegistry(ctx, registry); err != nil {
		return fmt.Errorf("upsert registry %q: %w", registry.ID, err)
	}
	if err := ensureObservationRecord(ctx, deps.MetadataStore, registry.ID); err != nil {
		return fmt.Errorf("initialize observation for registry %q: %w", registry.ID, err)
	}
	observation, err := deps.MetadataStore.GetRegistryObservation(ctx, registry.ID)
	if err != nil {
		return fmt.Errorf("load observation for registry %q: %w", registry.ID, err)
	}
	generation, err := startupGeneration(cfgRegistry, cfg)
	if err != nil {
		return fmt.Errorf("compute startup generation for registry %q: %w", registry.ID, err)
	}
	decision, err := requestRefresh(ctx, deps, cfgRegistry, observation.ObservedCommit, generation, "manual")
	if err != nil {
		return fmt.Errorf("request manual refresh for registry %q: %w", registry.ID, err)
	}
	jobID := decision.Job.ID

	workerID := fmt.Sprintf("reindex-%s", uuid.NewString())
	leaseDuration := refreshLeaseDuration(cfg)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		requested, err := deps.MetadataStore.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("load reindex job: %w", err)
		}
		switch requested.Status {
		case job.Completed:
			return nil
		case job.Failed:
			registry, registryErr := deps.MetadataStore.GetRegistry(ctx, store.RegistryID(cfgRegistry.ID))
			if registryErr == nil && registry.RefreshError != "" {
				return errors.New(registry.RefreshError)
			}
			return fmt.Errorf("reindex job %q failed", jobID)
		}

		leased, err := deps.MetadataStore.LeaseJobByID(ctx, jobID, workerID, leaseDuration)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return fmt.Errorf("lease reindex job: %w", err)
		}

		if err := processRefreshJob(ctx, deps, leased, workerID); err != nil {
			return err
		}
	}
}

type refreshPayload struct {
	RegistryID string `json:"registry_id"`
	Commit     string `json:"commit"`
	Generation string `json:"generation"`
	Trigger    string `json:"trigger"`
}

func requestRefresh(ctx context.Context, deps Deps, src config.Registry, commit, generation, trigger string) (store.RefreshDecision, error) {
	jobID := store.JobID(uuid.NewString())
	payload, err := json.Marshal(refreshPayload{
		RegistryID: src.ID,
		Commit:     commit,
		Generation: generation,
		Trigger:    trigger,
	})
	if err != nil {
		return store.RefreshDecision{}, fmt.Errorf("marshal refresh payload: %w", err)
	}
	maxAttempts := 3
	if trigger == "manual" {
		maxAttempts = 1
	}
	now := time.Now().UTC()
	j := job.Job{
		ID:             jobID,
		Kind:           "refresh",
		Payload:        payload,
		MaxAttempts:    maxAttempts,
		IdempotencyKey: fmt.Sprintf("refresh-%s-%s", src.ID, uuid.NewString()),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	decision, err := deps.MetadataStore.RequestRefresh(ctx, store.RegistryID(src.ID), store.RefreshRequest{
		Commit: commit, Generation: generation, Trigger: trigger, Job: j,
	})
	if err != nil {
		return store.RefreshDecision{}, err
	}
	slog.Info("refresh requested", "registry", src.ID, "trigger", trigger,
		"coalesced", decision.Coalesced, "job", decision.Job.ID,
		"commit", commit, "origin", redact.URL(src.Origin))
	return decision, nil
}

func startupGeneration(src config.Registry, cfg config.Config) (string, error) {
	registry := src
	if src.Credentials.SSHKey != nil {
		credential := *src.Credentials.SSHKey
		credential.Value = ""
		registry.Credentials.SSHKey = &credential
	}
	if src.Credentials.Token != nil {
		credential := *src.Credentials.Token
		credential.Value = ""
		registry.Credentials.Token = &credential
	}
	input := struct {
		Registry    config.Registry `json:"registry"`
		Acquisition string          `json:"resolved_acquisition"`
		Policy      config.Policy   `json:"policy"`
	}{Registry: registry, Acquisition: defaultString(src.Acquisition, cfg.AcquisitionDefault), Policy: cfg.Policy}
	data, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal startup configuration: %w", err)
	}
	digest := sha256.Sum256(data)
	return "startup-v1:" + hex.EncodeToString(digest[:]), nil
}

func recordProbeFailure(ctx context.Context, meta store.MetadataStore, id store.RegistryID, now time.Time, interval time.Duration, cause error) error {
	message := boundedLogError(cause)
	_, err := mutateObservation(ctx, meta, id, func(next *store.RegistryObservation) {
		next.ObservedAt = now
		next.ProbeStatus = store.RegistryProbeStatusFailed
		next.ProbeError = message
		next.ConsecutiveProbeFailures++
		next.NextProbeAt = now.Add(probeBackoffDelay(id, next.ConsecutiveProbeFailures, interval))
	})
	return err
}

func recordSuccessfulIndex(ctx context.Context, meta store.MetadataStore, id store.RegistryID, commit, generation string, indexedAt time.Time) error {
	_, err := mutateObservation(ctx, meta, id, func(next *store.RegistryObservation) {
		next.SuccessfulCommit = commit
		next.SuccessfulGeneration = generation
		next.LastSuccessfulIndexAt = indexedAt
		if next.NextReconcileAt.IsZero() {
			next.NextReconcileAt = nextPhaseSlot(id, indexedAt, config.DefaultObservationReconciliationInterval)
		}
	})
	return err
}

func mutateObservation(ctx context.Context, meta store.MetadataStore, id store.RegistryID, mutate func(*store.RegistryObservation)) (store.RegistryObservation, error) {
	for attempt := 0; attempt < 8; attempt++ {
		current, err := meta.GetRegistryObservation(ctx, id)
		if err != nil {
			return store.RegistryObservation{}, err
		}
		next := current
		mutate(&next)
		updated, err := meta.UpdateRegistryObservation(ctx, current.Version, next)
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		return updated, err
	}
	return store.RegistryObservation{}, fmt.Errorf("update registry observation %q: %w", id, store.ErrConflict)
}

func nextPhaseSlot(id store.RegistryID, now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return now
	}
	intervalNanos := int64(interval)
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(id))
	phase := int64(hash.Sum64() % uint64(interval))
	offset := now.UnixNano() % intervalNanos
	if offset < 0 {
		offset += intervalNanos
	}
	delta := (phase - offset + intervalNanos) % intervalNanos
	if delta == 0 {
		delta = intervalNanos
	}
	return now.Add(time.Duration(delta))
}

func probeBackoffDelay(id store.RegistryID, failure int, interval time.Duration) time.Duration {
	cap := config.DefaultObservationReconciliationInterval
	if interval <= 0 {
		interval = cap
	}
	delay := min(interval, cap)
	for attempt := 1; attempt < failure && delay < cap; attempt++ {
		if delay > cap/2 {
			delay = cap
		} else {
			delay *= 2
		}
	}
	lower := delay / 2
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(id))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(failure))
	_, _ = hash.Write(encoded[:])
	return lower + time.Duration(hash.Sum64()%uint64(delay-lower+1))
}

func boundedLogError(err error) string {
	if err == nil {
		return ""
	}
	message := redact.URL(err.Error())
	const maxLogErrorRunes = 200
	runes := []rune(message)
	if len(runes) > maxLogErrorRunes {
		return string(runes[:maxLogErrorRunes-1]) + "…"
	}
	return message
}

func cfgRefreshConcurrency(cfg config.Config) int {
	if c := cfg.RefreshConcurrency; c > 0 {
		return c
	}
	return 1
}

func registryByID(cfg config.Config, id string) (config.Registry, bool) {
	for _, src := range cfg.Registries {
		if src.ID == id {
			return src, true
		}
	}
	return config.Registry{}, false
}

func registryBySlugOrID(cfg config.Config, s string) (config.Registry, bool) {
	for _, src := range cfg.Registries {
		if src.ID == s || src.Slug == s {
			return src, true
		}
	}
	return config.Registry{}, false
}

func mergeStoreRegistry(existing store.Registry, cfg config.Registry, defaultAcquisition string) store.Registry {
	s := existing
	s.ID = store.RegistryID(cfg.ID)
	s.Slug = cfg.Slug
	s.Ref = cfg.Ref
	s.Roots = toStoreRoots(cfg.Roots)
	s.Acquisition = store.AcquisitionMode(defaultString(cfg.Acquisition, defaultAcquisition))
	s.Publisher = store.Publisher{Name: cfg.Publisher.Name, URL: cfg.Publisher.URL}
	s.License = cfg.License
	s.PolicyOverrides = cfg.PolicyOverrides
	s.Listed = true

	now := time.Now().UTC()
	if existing.ID == "" {
		s.CreatedAt = now
	}
	s.UpdatedAt = now

	if cfg.Origin != "" {
		if len(s.Origins) == 0 || s.Origins[len(s.Origins)-1] != cfg.Origin {
			s.Origins = append(s.Origins, cfg.Origin)
		}
	}

	if existing.Slug != "" && existing.Slug != cfg.Slug {
		if !slices.Contains(s.Aliases, existing.Slug) {
			s.Aliases = append(s.Aliases, existing.Slug)
		}
	}
	for _, alias := range cfg.Aliases {
		if !slices.Contains(s.Aliases, alias) {
			s.Aliases = append(s.Aliases, alias)
		}
	}

	return s
}

func toStoreRoots(roots []config.RegistryRoot) []store.RegistryRoot {
	out := make([]store.RegistryRoot, 0, len(roots))
	for _, r := range roots {
		out = append(out, store.RegistryRoot{Path: r.Path, SourceHarness: r.SourceHarness})
	}
	return out
}

func toAcquireCredentials(cred config.Credentials, caFile string) (acquire.Credentials, error) {
	out := acquire.Credentials{CAFile: caFile}
	if cred.SSHKey != nil {
		out.SSHKeyFile = cred.SSHKey.File
		return out, nil
	}
	if cred.Token != nil {
		token, err := resolveToken(*cred.Token)
		if err != nil {
			return acquire.Credentials{}, err
		}
		out.Token = token
	}
	return out, nil
}

func resolveToken(t config.TokenCredential) (string, error) {
	if t.Env != "" {
		return os.Getenv(t.Env), nil
	}
	if t.File != "" {
		data, err := os.ReadFile(t.File)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func recordRefreshSuccess(ctx context.Context, s store.MetadataStore, src store.Registry) error {
	src.RefreshStatus = store.RegistryRefreshStatusOK
	src.RefreshError = ""
	src.RefreshedAt = time.Now().UTC()
	if err := s.PutRegistry(ctx, src); err != nil {
		slog.Error("failed to record refresh success", "registry", src.ID, "error", err)
		return err
	}
	return nil
}

func recordRefreshFailure(ctx context.Context, s store.MetadataStore, src store.Registry, err error) error {
	src.RefreshStatus = store.RegistryRefreshStatusFailed
	src.RefreshError = err.Error()
	src.RefreshedAt = time.Now().UTC()
	if putErr := s.PutRegistry(ctx, src); putErr != nil {
		slog.Error("failed to record refresh failure", "registry", src.ID, "error", putErr)
	}
	return err
}

func defaultString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
