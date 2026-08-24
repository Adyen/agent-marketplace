// Package worker implements the bounded Marketplace indexing scheduler.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Deps carries the runtime dependencies for worker operations.
type Deps struct {
	Config        config.Config
	Marketplace   store.Marketplace
	MetadataStore store.MetadataStore
	BlobStore     store.BlobStore
}

// Refresh performs a single refresh of the configured source.
// It upserts the source record, clones the repository, builds a source revision,
// and publishes it atomically. The source status is updated to reflect the
// outcome of the refresh.
func Refresh(ctx context.Context, deps Deps, source store.Source) error {
	cfgSource, ok := sourceByID(deps.Config, string(source.ID))
	if !ok {
		return fmt.Errorf("source %q is not present in the operator config", source.ID)
	}

	existing, err := deps.MetadataStore.GetSource(ctx, source.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load source %q: %w", source.ID, err)
	}

	merged := mergeStoreSource(existing, cfgSource, deps.Config.AcquisitionDefault)
	if err := deps.MetadataStore.PutSource(ctx, merged); err != nil {
		return fmt.Errorf("upsert source %q: %w", source.ID, err)
	}

	if len(merged.Origins) == 0 {
		return fmt.Errorf("source %q has no origin", source.ID)
	}
	origin := merged.Origins[len(merged.Origins)-1]

	creds, err := toAcquireCredentials(cfgSource.Credentials)
	if err != nil {
		return fmt.Errorf("resolve credentials for source %q: %w", source.ID, err)
	}

	limits := acquire.Limits{
		RepositoryBytes: deps.Config.Policy.Limits.RepositoryBytes,
		FileCount:       deps.Config.Policy.Limits.FileCount,
		FileBytes:       deps.Config.Policy.Limits.FileBytes,
	}

	res, err := acquire.Clone(ctx, origin, merged.Ref, creds, limits, deps.Config.Policy.Limits.CloneDuration.Duration())
	if err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}
	defer func() { _ = res.Release() }()

	rev, err := ingest.BuildSourceRevision(ctx, deps.Config, deps.Marketplace.ID, merged, res.Dir, res.Commit)
	if err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}

	pub := store.SourceRevisionPublication{
		Occurrence:      rev.Occurrence,
		Resources:       rev.Resources,
		Collections:     rev.Collections,
		ManifestPayload: rev.ManifestPayload,
		ExtraBlobs:      rev.ExtraBlobs,
	}
	if err := store.PublishSourceRevision(ctx, deps.MetadataStore, deps.BlobStore, pub); err != nil {
		return recordRefreshFailure(ctx, deps.MetadataStore, merged, err)
	}

	slog.Info("refreshed source", "source", source.ID, "commit", res.Commit, "resources", len(rev.Resources))
	return recordRefreshSuccess(ctx, deps.MetadataStore, merged)
}

// EnsureMarketplace loads or creates the Marketplace instance record.
func EnsureMarketplace(ctx context.Context, deps *Deps) error {
	mp, err := deps.MetadataStore.GetMarketplace(ctx, store.MarketplaceID(deps.Config.Instance.ID))
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

// EnsureRevocations writes operator-configured revocations to the store.
func EnsureRevocations(ctx context.Context, deps Deps) error {
	now := time.Now().UTC()
	return deps.MetadataStore.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		for _, id := range deps.Config.Revocations.SourceIDs {
			if err := tx.PutRevocation(txCtx, store.Revocation{
				Kind:      store.RevocationSource,
				TargetID:  id,
				Reason:    "operator configured",
				AppliedAt: now,
			}); err != nil {
				return fmt.Errorf("revoke source %q: %w", id, err)
			}
		}
		for _, coord := range deps.Config.Revocations.Coordinates {
			if err := tx.PutRevocation(txCtx, store.Revocation{
				Kind:      store.RevocationCoordinate,
				TargetID:  coord,
				Reason:    "operator configured",
				AppliedAt: now,
			}); err != nil {
				return fmt.Errorf("revoke coordinate %q: %w", coord, err)
			}
		}
		for _, raw := range deps.Config.Revocations.Revisions {
			target, err := store.ParseRevisionRevocationTarget(raw)
			if err != nil {
				return fmt.Errorf("parse revision revocation %q: %w", raw, err)
			}
			if configured := deps.Config.Instance.ID; configured != "" && string(target.Coordinate.Marketplace) != configured {
				return fmt.Errorf("revision revocation %q targets Marketplace %q, configured instance is %q", raw, target.Coordinate.Marketplace, configured)
			}
			revocation := store.Revocation{
				Kind:      store.RevocationRevision,
				TargetID:  target.String(),
				Reason:    "operator configured",
				AppliedAt: now,
			}
			if err := tx.PutRevocation(txCtx, revocation); err != nil {
				return fmt.Errorf("revoke revision %q: %w", revocation.TargetID, err)
			}
		}
		return nil
	})
}

// Run starts the background indexing worker. It enqueues an initial refresh for
// every configured source and then runs a scheduler loop that leases refresh
// jobs until ctx is cancelled.
func Run(ctx context.Context, deps Deps, cfg config.Config) error {
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		return err
	}
	if err := EnsureRevocations(ctx, deps); err != nil {
		slog.Error("failed to apply revocations", "err", err)
		return err
	}

	for _, src := range cfg.Sources {
		if err := enqueueRefresh(ctx, deps.MetadataStore, src, cfg.Refresh.Interval.Duration()); err != nil && !errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("enqueue initial refresh for %q: %w", src.ID, err)
		}
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

	ticker := time.NewTicker(cfg.Refresh.Interval.Duration())
	defer ticker.Stop()

	for {
		for _, src := range cfg.Sources {
			if err := enqueueRefresh(ctx, deps.MetadataStore, src, cfg.Refresh.Interval.Duration()); err != nil && !errors.Is(err, store.ErrConflict) {
				slog.Error("enqueue refresh failed", "source", src.ID, "err", err)
			}
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

func workerLoop(ctx context.Context, deps Deps, workerIndex int) {
	workerID := fmt.Sprintf("worker-%d-%s", workerIndex, uuid.NewString())
	leaseDuration := 5 * time.Minute
	if d := deps.Config.Policy.Limits.CloneDuration.Duration(); d > 0 && d > leaseDuration {
		leaseDuration = d + 5*time.Minute
	}

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

	source, err := loadSourceForJob(ctx, deps, payload.SourceID)
	if err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return err
	}

	if err := Refresh(ctx, deps, source); err != nil {
		_ = job.Fail(ctx, deps.MetadataStore, j.ID, workerID)
		return err
	}

	if err := job.Complete(ctx, deps.MetadataStore, j.ID, workerID); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

func loadSourceForJob(ctx context.Context, deps Deps, id string) (store.Source, error) {
	cfgSource, ok := sourceByID(deps.Config, id)
	if !ok {
		return store.Source{}, fmt.Errorf("source %q not in config", id)
	}

	existing, err := deps.MetadataStore.GetSource(ctx, store.SourceID(id))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Source{}, fmt.Errorf("get source %q: %w", id, err)
	}
	return mergeStoreSource(existing, cfgSource, deps.Config.AcquisitionDefault), nil
}

// Reindex performs a one-shot durable enqueue of the requested source and
// processes refresh jobs until the requested source's refresh is complete.
func Reindex(ctx context.Context, deps Deps, cfg config.Config, sourceSlugOrID string) error {
	if deps.Marketplace.ID == "" {
		if err := EnsureMarketplace(ctx, &deps); err != nil {
			return err
		}
	}

	cfgSource, ok := sourceBySlugOrID(cfg, sourceSlugOrID)
	if !ok {
		return fmt.Errorf("source %q not found in config", sourceSlugOrID)
	}

	existing, err := deps.MetadataStore.GetSource(ctx, store.SourceID(cfgSource.ID))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("get source %q: %w", cfgSource.ID, err)
	}

	source := mergeStoreSource(existing, cfgSource, cfg.AcquisitionDefault)
	if err := deps.MetadataStore.PutSource(ctx, source); err != nil {
		return fmt.Errorf("upsert source %q: %w", source.ID, err)
	}

	jobID := store.JobID(uuid.NewString())
	payloadBytes, err := json.Marshal(refreshPayload{SourceID: string(source.ID)})
	if err != nil {
		return fmt.Errorf("marshal job payload: %w", err)
	}

	j := job.Job{
		ID:             jobID,
		Kind:           "refresh",
		Payload:        payloadBytes,
		MaxAttempts:    1,
		IdempotencyKey: fmt.Sprintf("manual-%s-%d", source.ID, time.Now().Unix()),
	}
	if err := job.Enqueue(ctx, deps.MetadataStore, j); err != nil {
		return fmt.Errorf("enqueue reindex job: %w", err)
	}

	workerID := fmt.Sprintf("reindex-%s", uuid.NewString())
	leaseDuration := 5 * time.Minute
	if d := cfg.Policy.Limits.CloneDuration.Duration(); d > 0 && d > leaseDuration {
		leaseDuration = d + 5*time.Minute
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		leased, err := job.Lease(ctx, deps.MetadataStore, "refresh", workerID, leaseDuration)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("lease reindex job: %w", err)
		}

		if err := processRefreshJob(ctx, deps, leased, workerID); err != nil {
			return err
		}

		if string(leased.ID) == string(jobID) {
			return nil
		}
	}
}

type refreshPayload struct {
	SourceID string `json:"source_id"`
}

func enqueueRefresh(ctx context.Context, s store.MetadataStore, src config.Source, interval time.Duration) error {
	bucket := time.Now().Truncate(interval).Unix()
	payload, err := json.Marshal(refreshPayload{SourceID: src.ID})
	if err != nil {
		return err
	}

	j := job.Job{
		ID:             store.JobID(uuid.NewString()),
		Kind:           "refresh",
		Payload:        payload,
		MaxAttempts:    3,
		IdempotencyKey: fmt.Sprintf("scheduled-%s-%d", src.ID, bucket),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	return job.Enqueue(ctx, s, j)
}

func cfgRefreshConcurrency(cfg config.Config) int {
	if c := cfg.RefreshConcurrency; c > 0 {
		return c
	}
	return 1
}

func sourceByID(cfg config.Config, id string) (config.Source, bool) {
	for _, src := range cfg.Sources {
		if src.ID == id {
			return src, true
		}
	}
	return config.Source{}, false
}

func sourceBySlugOrID(cfg config.Config, s string) (config.Source, bool) {
	for _, src := range cfg.Sources {
		if src.ID == s || src.Slug == s {
			return src, true
		}
	}
	return config.Source{}, false
}

func mergeStoreSource(existing store.Source, cfg config.Source, defaultAcquisition string) store.Source {
	s := existing
	s.ID = store.SourceID(cfg.ID)
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

func toStoreRoots(roots []config.SourceRoot) []store.SourceRoot {
	out := make([]store.SourceRoot, 0, len(roots))
	for _, r := range roots {
		out = append(out, store.SourceRoot{Path: r.Path, SourceHarness: r.SourceHarness})
	}
	return out
}

func toAcquireCredentials(cred config.Credentials) (acquire.Credentials, error) {
	if cred.SSHKey != nil {
		return acquire.Credentials{SSHKeyFile: cred.SSHKey.File}, nil
	}
	if cred.Token != nil {
		token, err := resolveToken(*cred.Token)
		if err != nil {
			return acquire.Credentials{}, err
		}
		return acquire.Credentials{Token: token}, nil
	}
	return acquire.Credentials{}, nil
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

func recordRefreshSuccess(ctx context.Context, s store.MetadataStore, src store.Source) error {
	src.RefreshStatus = store.SourceRefreshStatusOK
	src.RefreshError = ""
	src.RefreshedAt = time.Now().UTC()
	if err := s.PutSource(ctx, src); err != nil {
		slog.Error("failed to record refresh success", "source", src.ID, "error", err)
		return err
	}
	return nil
}

func recordRefreshFailure(ctx context.Context, s store.MetadataStore, src store.Source, err error) error {
	src.RefreshStatus = store.SourceRefreshStatusFailed
	src.RefreshError = err.Error()
	src.RefreshedAt = time.Now().UTC()
	if putErr := s.PutSource(ctx, src); putErr != nil {
		slog.Error("failed to record refresh failure", "source", src.ID, "error", putErr)
	}
	return err
}

func defaultString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
