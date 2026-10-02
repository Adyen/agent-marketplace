package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/google/uuid"
)

var (
	ErrManagedSuperseded = errors.New("marketplace/worker: managed work superseded")
	ErrManagedProfile    = errors.New("marketplace/worker: managed credential profile unavailable")
	ErrManagedRefresh    = errors.New("marketplace/worker: managed refresh failed")
	ErrManagedAcquire    = fmt.Errorf("marketplace/worker: managed acquisition failed: %w", ErrManagedRefresh)
)

type managedAttemptRecorder interface {
	RecordManagedRefreshAttempt(context.Context, store.ManagedFence, string, store.PublicationID, ...store.ManagedRefreshFailure) error
}

type managedReconcilePayload struct {
	Marketplace store.MarketplaceID `json:"marketplace_id"`
	Revision    int64               `json:"revision"`
	RegistryID  store.RegistryID    `json:"registry_id,omitempty"`
}

type managedRefreshPayload struct {
	Marketplace        store.MarketplaceID `json:"marketplace_id"`
	RegistryID         store.RegistryID    `json:"registry_id"`
	RegistryGeneration int64               `json:"registry_generation"`
	PolicyRevision     int64               `json:"policy_revision"`
	Revision           int64               `json:"revision"`
	ParentJobID        store.JobID         `json:"parent_job_id,omitempty"`
	Operation          string              `json:"operation"`
}

func managedView(ctx context.Context, deps Deps) (admin.View, error) {
	return (admin.Service{Metadata: deps.MetadataStore}).Read(ctx, store.MarketplaceID(deps.Config.Instance.ID))
}

func managedJob(p managedRefreshPayload, scope string) (store.Job, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return store.Job{}, err
	}
	identity, _ := json.Marshal([]any{p.Marketplace, p.RegistryID, p.RegistryGeneration, p.PolicyRevision, scope})
	sum := sha256.Sum256(identity)
	key := "managed-refresh-" + hex.EncodeToString(sum[:])
	return store.Job{ID: store.JobID(key), Kind: "managed-refresh", Payload: payload, IdempotencyKey: key, MaxAttempts: 1}, nil
}

func enqueueManaged(ctx context.Context, deps Deps, view admin.View, registry admin.Registry, parent store.JobID, revision int64, scope, operation string) (store.JobID, error) {
	p := managedRefreshPayload{Marketplace: store.MarketplaceID(deps.Config.Instance.ID), RegistryID: registry.ID, RegistryGeneration: registry.Generation, PolicyRevision: view.Snapshot.PolicyRevision, Revision: revision, ParentJobID: parent, Operation: operation}
	j, err := managedJob(p, scope)
	if err != nil {
		return "", err
	}
	err = deps.MetadataStore.EnqueueJob(ctx, j)
	if errors.Is(err, store.ErrConflict) {
		err = nil
	}
	return j.ID, err
}

func scheduleManaged(ctx context.Context, deps Deps, scope string) error {
	view, err := managedView(ctx, deps)
	if err != nil {
		return err
	}
	for _, r := range view.Snapshot.Registries {
		if !r.Active() {
			continue
		}
		if _, err := enqueueManaged(ctx, deps, view, r, "", view.Revision, scope, "scheduled"); err != nil {
			return err
		}
	}
	return nil
}

func managedLease(ctx context.Context, s store.MetadataStore, j store.Job, worker string) error {
	current, err := s.GetJob(ctx, j.ID)
	if err != nil {
		return err
	}
	if current.Status != store.JobStatusLeased || current.WorkerID != worker || current.Attempt != j.Attempt || !current.LeaseUntil.After(time.Now()) {
		return store.ErrLeaseExpired
	}
	return nil
}

func processManagedReconcile(ctx context.Context, deps Deps, j store.Job, worker string) error {
	var p managedReconcilePayload
	if json.Unmarshal(j.Payload, &p) != nil || p.Marketplace != store.MarketplaceID(deps.Config.Instance.ID) || p.Revision < 1 {
		return finishManaged(ctx, deps, j, worker, store.ErrManagedStateCorrupt)
	}
	err := deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		local := deps
		local.MetadataStore = tx
		if err := managedLease(ctx, tx, j, worker); err != nil {
			return err
		}
		view, err := managedView(ctx, local)
		if err != nil {
			return err
		}
		if p.Revision > view.Revision {
			return store.ErrManagedStateCorrupt
		}
		intended, err := managedIntentSnapshot(ctx, tx, j, p)
		if err != nil {
			return err
		}
		original := make(map[store.RegistryID]admin.Registry, len(intended.Registries))
		for _, r := range intended.Registries {
			original[r.ID] = r
		}
		for _, r := range view.Snapshot.Registries {
			prior, exists := original[r.ID]
			if !exists || !prior.Active() || !r.Active() || prior.Generation != r.Generation || intended.PolicyRevision != view.Snapshot.PolicyRevision || (p.RegistryID != "" && p.RegistryID != r.ID) {
				continue
			}
			if _, err := enqueueManaged(ctx, local, view, r, j.ID, p.Revision, "reconcile-"+string(j.ID), "reconcile"); err != nil {
				return err
			}
		}
		return tx.CompleteJob(ctx, j.ID, worker)
	})
	if err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	return nil
}

func managedIntentSnapshot(ctx context.Context, metadata store.MetadataStore, j store.Job, p managedReconcilePayload) (admin.Snapshot, error) {
	managed, ok := metadata.(store.ManagedConfigurationStore)
	if !ok {
		return admin.Snapshot{}, store.ErrTopology
	}
	before := p.Revision + 1
	if before < 0 {
		before = 0
	}
	history, err := managed.ManagedConfigurationHistory(ctx, p.Marketplace, before, 1)
	if err != nil {
		return admin.Snapshot{}, err
	}
	if len(history) != 1 || history[0].Revision != p.Revision || !slices.Contains(history[0].JobIDs, j.ID) {
		return admin.Snapshot{}, store.ErrManagedStateCorrupt
	}
	var snapshot admin.Snapshot
	if json.Unmarshal(history[0].Snapshot, &snapshot) != nil || snapshot.Version != 1 || snapshot.PolicyRevision < 1 {
		return admin.Snapshot{}, store.ErrManagedStateCorrupt
	}
	return snapshot, nil
}

func managedPreflight(ctx context.Context, deps Deps, j store.Job, worker string, p managedRefreshPayload) (admin.View, admin.Registry, error) {
	if p.Marketplace != store.MarketplaceID(deps.Config.Instance.ID) || p.RegistryGeneration < 1 || p.PolicyRevision < 1 || p.Revision < 1 {
		return admin.View{}, admin.Registry{}, store.ErrManagedStateCorrupt
	}
	if err := managedLease(ctx, deps.MetadataStore, j, worker); err != nil {
		return admin.View{}, admin.Registry{}, err
	}
	view, err := managedView(ctx, deps)
	if err != nil {
		return view, admin.Registry{}, err
	}
	if p.PolicyRevision != view.Snapshot.PolicyRevision {
		return view, admin.Registry{}, ErrManagedSuperseded
	}
	for _, r := range view.Snapshot.Registries {
		if r.ID == p.RegistryID {
			if !r.Active() || r.Generation != p.RegistryGeneration {
				return view, r, ErrManagedSuperseded
			}
			return view, r, nil
		}
	}
	return view, admin.Registry{}, ErrManagedSuperseded
}

func finishManaged(ctx context.Context, deps Deps, j store.Job, worker string, cause error) error {
	err := deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := managedLease(ctx, tx, j, worker); err != nil {
			return err
		}
		if cause != nil {
			var backoff time.Duration
			if j.Kind == "managed-reconcile" {
				backoff = max(job.BaseBackoff, job.NextRetryDelay(j.Attempt))
			}
			return tx.FailJob(ctx, j.ID, worker, backoff)
		}
		return tx.CompleteJob(ctx, j.ID, worker)
	})
	if err != nil {
		return err
	}
	return cause
}

func managedOriginAllowed(origin string, allowed []string) bool {
	if strings.HasPrefix(origin, "git@") && !strings.Contains(origin, "://") {
		host, path, ok := strings.Cut(strings.TrimPrefix(origin, "git@"), ":")
		if !ok || strings.ContainsAny(host, "/?#@") || path == "" {
			return false
		}
		origin = "ssh://git@" + host + "/" + path
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || strings.ContainsAny(u.Path, "\\\x00\r\n") || (u.Scheme != "https" && u.Scheme != "ssh") {
		return false
	}
	if u.User != nil {
		if _, password := u.User.Password(); password || u.Scheme != "ssh" || u.User.Username() != "git" {
			return false
		}
	}
	return slices.Contains(allowed, u.Scheme+"://"+strings.ToLower(u.Host))
}

func managedCredentials(m config.Management, r admin.Registry) (acquire.Credentials, error) {
	if err := m.Validate(); err != nil || !managedOriginAllowed(r.Origin, m.AllowedOrigins) {
		return acquire.Credentials{}, ErrManagedProfile
	}
	if r.ProfileID == "" {
		return acquire.Credentials{}, nil
	}
	profile, ok := m.Profiles[r.ProfileID]
	if !ok || !managedOriginAllowed(r.Origin, profile.AllowedOrigins) {
		return acquire.Credentials{}, ErrManagedProfile
	}
	creds, err := toAcquireCredentials(profile.Credentials, profile.CAFile)
	if err != nil || (profile.Credentials.Token != nil && strings.TrimSpace(creds.Token) == "") {
		return acquire.Credentials{}, ErrManagedProfile
	}
	return creds, nil
}

func managedInputs(deps Deps, view admin.View, r admin.Registry) (config.Config, store.Registry) {
	cfg := deps.Config
	cfg.Policy.LicenseAllowlist = slices.Clone(view.Snapshot.LicensePolicy.Allowlist)
	cfg.Policy.LicenseUndeclared = view.Snapshot.LicensePolicy.Undeclared
	cfg.Policy.RejectedResources = config.ResolveRejectedResourcesOutcome(view.Snapshot.DistributionPolicy.RejectedResources)
	overrides := maps.Clone(r.NonLicenseOverrides)
	if overrides == nil {
		overrides = map[string]any{}
	}
	if r.RejectedResources != "" {
		overrides["rejected_resources"] = r.RejectedResources
	}
	if r.LicenseOverride != nil {
		values := make([]any, len(r.LicenseOverride.Allowlist))
		for i, v := range r.LicenseOverride.Allowlist {
			values[i] = v
		}
		overrides["license_allowlist"] = values
		overrides["license_undeclared"] = r.LicenseOverride.Undeclared
	}
	return cfg, store.Registry{ID: r.ID, Slug: r.Slug, Aliases: slices.Clone(r.Aliases), Origins: append(slices.Clone(r.HistoricalOrigins), r.Origin), Ref: r.Ref, Roots: slices.Clone(r.Roots), Acquisition: r.Acquisition, Publisher: r.Publisher, License: r.License, PolicyOverrides: overrides, Listed: true}
}

type managedCloneFunc func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error)

func processManagedRefresh(ctx context.Context, deps Deps, j store.Job, worker string, clone managedCloneFunc) error {
	var p managedRefreshPayload
	if json.Unmarshal(j.Payload, &p) != nil {
		return finishManaged(ctx, deps, j, worker, store.ErrManagedStateCorrupt)
	}
	fence := store.ManagedFence{Marketplace: p.Marketplace, RegistryGeneration: p.RegistryGeneration, PolicyRevision: p.PolicyRevision, JobID: j.ID, WorkerID: worker, Attempt: j.Attempt}
	view, r, err := managedPreflight(ctx, deps, j, worker, p)
	if err != nil {
		return recordManagedFailure(ctx, deps, j, worker, fence, p, err)
	}
	recorder, ok := deps.MetadataStore.(managedAttemptRecorder)
	if !ok {
		return finishManaged(ctx, deps, j, worker, store.ErrTopology)
	}
	publications, ok := deps.MetadataStore.(store.PublicationStore)
	if !ok {
		return finishManaged(ctx, deps, j, worker, store.ErrTopology)
	}
	current, err := publications.CurrentRegistryPublication(ctx, r.ID)
	if err == nil {
		fence.ExpectedPublicationID = current.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		return recordManagedFailure(ctx, deps, j, worker, fence, p, err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		return recordManagedFailure(ctx, deps, j, worker, fence, p, err)
	}
	err = refreshManaged(ctx, deps, j, worker, p, view, r, fence, clone)
	if err == nil {
		return nil
	}
	return recordManagedFailure(ctx, deps, j, worker, fence, p, err)
}

func managedFailure(cause error, operation string) store.ManagedRefreshFailure {
	op := "refresh"
	if operation != "" && len(operation) <= 64 && !strings.ContainsAny(operation, "/\\ \t\r\n\x00") {
		op = operation
	}
	switch {
	case errors.Is(cause, ErrManagedSuperseded):
		return store.ManagedRefreshFailure{Code: "superseded", Cause: "managed work superseded by a newer configuration"}
	case errors.Is(cause, store.ErrPublicationFence):
		return store.ManagedRefreshFailure{Code: "publication_fence", Cause: "publication fence rejected the staged revision"}
	case errors.Is(cause, store.ErrLeaseExpired):
		return store.ManagedRefreshFailure{Code: "lease_lost", Cause: "the job lease was lost before completion"}
	case errors.Is(cause, ErrManagedProfile):
		return store.ManagedRefreshFailure{Code: "egress_policy/" + op, Cause: "registry origin or credentials are not permitted by the management profile"}
	case errors.Is(cause, store.ErrConflict):
		return store.ManagedRefreshFailure{Code: "publication_conflict/" + op, Cause: "publication returned a conflict"}
	case errors.Is(cause, ErrManagedAcquire):
		return store.ManagedRefreshFailure{Code: "acquisition/" + op, Cause: "registry acquisition failed"}
	case errors.Is(cause, ErrManagedRefresh):
		return store.ManagedRefreshFailure{Code: "artifact_verification/" + op, Cause: "registry revision build or artifact verification failed"}
	default:
		return store.ManagedRefreshFailure{Code: "internal/" + op, Cause: "managed refresh failed"}
	}
}

func recordManagedFailure(ctx context.Context, deps Deps, j store.Job, worker string, fence store.ManagedFence, p managedRefreshPayload, cause error) error {
	failure := managedFailure(cause, p.Operation)
	err := deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := managedLease(ctx, tx, j, worker); err != nil {
			return err
		}
		recorder, ok := tx.(managedAttemptRecorder)
		if !ok {
			return store.ErrTopology
		}
		outcome := "failed"
		if errors.Is(cause, ErrManagedSuperseded) || errors.Is(cause, store.ErrPublicationFence) {
			outcome = "superseded"
		}
		if err := recorder.RecordManagedRefreshAttempt(ctx, fence, outcome, "", failure); err != nil {
			return err
		}
		return tx.FailJob(ctx, j.ID, worker, 0)
	})
	if err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	slog.WarnContext(ctx, "managed refresh attempt failed", "job", j.ID, "registry", p.RegistryID, "outcome", outcomeFor(cause), "failure_code", failure.Code, "cause", failure.Cause)
	return cause
}

func outcomeFor(cause error) string {
	if errors.Is(cause, ErrManagedSuperseded) || errors.Is(cause, store.ErrPublicationFence) {
		return "superseded"
	}
	return "failed"
}

func refreshManaged(ctx context.Context, deps Deps, j store.Job, worker string, p managedRefreshPayload, view admin.View, r admin.Registry, fence store.ManagedFence, clone managedCloneFunc) error {
	creds, err := managedCredentials(deps.Config.Management, r)
	if err != nil {
		return err
	}
	cfg, registry := managedInputs(deps, view, r)
	deadlineCtx, stop := context.WithDeadline(ctx, j.LeaseUntil)
	defer stop()
	workCtx, cancel := context.WithCancelCause(deadlineCtx)
	done := make(chan struct{})
	defer func() { cancel(nil); <-done }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				if _, _, err := managedPreflight(workCtx, deps, j, worker, p); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	limits := acquire.Limits{RepositoryBytes: cfg.Policy.Limits.RepositoryBytes, FileCount: cfg.Policy.Limits.FileCount, FileBytes: cfg.Policy.Limits.FileBytes}
	roots := make([]string, len(r.Roots))
	for i := range r.Roots {
		roots[i] = r.Roots[i].Path
	}
	result, err := clone(workCtx, r.Origin, r.Ref, creds, limits, cfg.Policy.Limits.CloneDuration.Duration(), roots...)
	if err != nil {
		return managedWorkFailure(workCtx, ErrManagedAcquire)
	}
	defer func() { _ = result.Release() }()
	revision, err := ingest.BuildRegistryRevision(workCtx, cfg, p.Marketplace, registry, result.Dir, result.Commit)
	if err != nil {
		return managedWorkFailure(workCtx, ErrManagedRefresh)
	}
	if _, _, err := managedPreflight(workCtx, deps, j, worker, p); err != nil {
		if workCtx.Err() != nil {
			return managedWorkFailure(workCtx, ErrManagedRefresh)
		}
		return err
	}
	publication := store.RegistryRevisionPublication{PublicationID: store.PublicationID(uuid.NewString()), ManagedFence: &fence, Occurrence: revision.Occurrence, Resources: revision.Resources, ResourceOccurrences: revision.ResourceOccurrences, Assessments: revision.Assessments, Collections: revision.Collections, ManifestPayload: revision.ManifestPayload, ExtraBlobs: revision.ExtraBlobs, InspectionBlobs: revision.InspectionBlobs}
	metadata := managedPublicationCompletion{MetadataStore: deps.MetadataStore, fence: fence, publication: publication.PublicationID}
	if err := store.PublishRegistryRevision(workCtx, metadata, deps.BlobStore, publication); err != nil {
		if workCtx.Err() != nil {
			return managedWorkFailure(workCtx, ErrManagedRefresh)
		}
		return err
	}
	slog.InfoContext(ctx, "managed registry published", "registry", r.ID, "generation", r.Generation, "job", j.ID, "publication", publication.PublicationID)
	return nil
}

func managedWorkFailure(ctx context.Context, fallback error) error {
	cause := context.Cause(ctx)
	if errors.Is(cause, ErrManagedSuperseded) {
		return ErrManagedSuperseded
	}
	if errors.Is(cause, store.ErrLeaseExpired) {
		return store.ErrLeaseExpired
	}
	return fallback
}

type managedPublicationCompletion struct {
	store.MetadataStore
	fence       store.ManagedFence
	publication store.PublicationID
}

func (s managedPublicationCompletion) WithTx(ctx context.Context, fn func(context.Context, store.MetadataStore) error) error {
	return s.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		recorder, ok := tx.(managedAttemptRecorder)
		if !ok {
			return store.ErrTopology
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if err := recorder.RecordManagedRefreshAttempt(ctx, s.fence, "succeeded", s.publication); err != nil {
			return err
		}
		return tx.CompleteJob(ctx, s.fence.JobID, s.fence.WorkerID)
	})
}

func managedInterval(cfg config.Config) time.Duration {
	if d := cfg.Refresh.Interval.Duration(); d > 0 {
		return d
	}
	return 30 * time.Minute
}

func managedLeaseDuration(cfg config.Config) time.Duration {
	d := cfg.Policy.Limits.CloneDuration.Duration() + cfg.Policy.Limits.IngestionDuration.Duration() + time.Minute
	if d < 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

func runManaged(ctx context.Context, deps Deps) error {
	if _, err := managedView(ctx, deps); err != nil {
		return err
	}
	if _, ok := deps.MetadataStore.(managedAttemptRecorder); !ok {
		return store.ErrTopology
	}
	if _, ok := deps.MetadataStore.(store.PublicationStore); !ok {
		return store.ErrTopology
	}
	interval := managedInterval(deps.Config)
	if err := scheduleManaged(ctx, deps, fmt.Sprintf("scheduled-%d", time.Now().Truncate(interval).Unix())); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for i := 0; i < cfgRefreshConcurrency(deps.Config); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); managedWorkerLoop(ctx, deps) }()
	}
	defer wg.Wait()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := scheduleManaged(ctx, deps, fmt.Sprintf("scheduled-%d", time.Now().Truncate(interval).Unix())); err != nil {
				slog.ErrorContext(ctx, "managed scheduling failed")
			}
		}
	}
}

func managedWorkerLoop(ctx context.Context, deps Deps) {
	worker := "managed-" + uuid.NewString()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, kind := range []string{"managed-reconcile", "managed-refresh", admin.PreviewJobKind} {
			if ctx.Err() != nil {
				return
			}
			j, err := deps.MetadataStore.LeaseJob(ctx, kind, worker, managedLeaseDuration(deps.Config))
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				slog.ErrorContext(ctx, "managed lease failed")
				break
			}
			switch kind {
			case "managed-reconcile":
				err = processManagedReconcile(ctx, deps, j, worker)
			case admin.PreviewJobKind:
				err = processManagedPreview(ctx, deps, j, worker, acquire.Clone)
			default:
				err = processManagedRefresh(ctx, deps, j, worker, acquire.Clone)
			}
			if err != nil {
				slog.WarnContext(ctx, "managed job did not complete", "job", j.ID, "kind", kind)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func enqueueManagedReindex(ctx context.Context, deps Deps, view admin.View, target admin.Registry) (store.JobID, error) {
	actor := deps.ReindexActor
	if actor == (store.ManagedActor{}) {
		actor = store.ManagedActor{Issuer: "agent-marketplace:cli", Subject: fmt.Sprintf("uid:%d", os.Getuid())}
	}
	request := uuid.NewString()
	var id store.JobID
	err := deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		service := admin.Service{Metadata: tx, Deployment: deps.Config.Management}
		change, err := service.Apply(ctx, store.MarketplaceID(deps.Config.Instance.ID), admin.Mutation{Action: "reindex", RegistryID: target.ID, ExpectedRevision: view.Revision, Actor: actor, Reason: "CLI reindex", RequestID: request, IdempotencyKey: request})
		if err != nil {
			return err
		}
		if len(change.JobIDs) != 1 {
			return store.ErrManagedStateCorrupt
		}
		local := deps
		local.MetadataStore = tx
		current, err := managedView(ctx, local)
		if err != nil {
			return err
		}
		id, err = enqueueManaged(ctx, local, current, target, change.JobIDs[0], change.Revision, "reconcile-"+string(change.JobIDs[0]), "reconcile")
		if err != nil {
			return err
		}
		worker := "managed-reindex-reconcile-" + request
		parent, err := tx.LeaseJobByID(ctx, change.JobIDs[0], worker, managedLeaseDuration(deps.Config))
		if err != nil {
			return err
		}
		return processManagedReconcile(ctx, local, parent, worker)
	})
	return id, err
}

func reindexManaged(ctx context.Context, deps Deps, name string) error {
	view, err := managedView(ctx, deps)
	if err != nil {
		return err
	}
	var target admin.Registry
	for _, r := range view.Snapshot.Registries {
		if string(r.ID) == name || r.Slug == name || slices.Contains(r.Aliases, name) {
			target = r
			break
		}
	}
	if target.ID == "" {
		return store.ErrNotFound
	}
	if !target.Active() {
		return ErrManagedSuperseded
	}
	id, err := enqueueManagedReindex(ctx, deps, view, target)
	if err != nil {
		return err
	}
	worker := "managed-reindex-" + uuid.NewString()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		j, err := deps.MetadataStore.GetJob(ctx, id)
		if err != nil {
			return err
		}
		switch j.Status {
		case store.JobStatusCompleted:
			return nil
		case store.JobStatusFailed:
			return ErrManagedRefresh
		}
		if j.Attempt >= j.MaxAttempts && !j.LeaseUntil.After(time.Now()) {
			return store.ErrLeaseExpired
		}
		j, err = deps.MetadataStore.LeaseJobByID(ctx, id, worker, managedLeaseDuration(deps.Config))
		if err == nil {
			return processManagedRefresh(ctx, deps, j, worker, acquire.Clone)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
