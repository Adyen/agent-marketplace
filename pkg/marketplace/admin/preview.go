package admin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// PreviewJobKind is the durable job kind that acquires and discovers a
// proposed registry without publishing.
const PreviewJobKind = "managed-preview"

// PreviewJobPayload is the bounded payload of a PreviewJobKind job. The
// proposal itself lives in the preview record, never in the queue.
type PreviewJobPayload struct {
	Marketplace store.MarketplaceID `json:"marketplace_id"`
	PreviewID   string              `json:"preview_id"`
}

// Preview statically validates an upsert_registry proposal exactly like
// Validate, then records a preview and enqueues its acquisition job in one
// transaction. The stored registry is the submitted proposal, so clients can
// detect a drifted draft by comparison; the worker resolves everything else
// from live deployment configuration at execution time.
func (s Service) Preview(ctx context.Context, id store.MarketplaceID, m Mutation) (store.ManagedPreview, error) {
	if m.Action != "upsert_registry" || m.Registry == nil || m.LicensePolicy != nil {
		return store.ManagedPreview{}, ErrInvalidConfiguration
	}
	if strings.TrimSpace(m.Actor.Issuer) == "" || strings.TrimSpace(m.Actor.Subject) == "" {
		return store.ManagedPreview{}, store.ErrManagedChangeInvalid
	}
	previews, ok := s.Metadata.(store.ManagedPreviewStore)
	if !ok {
		return store.ManagedPreview{}, store.ErrTopology
	}
	view, err := s.Read(ctx, id)
	if err != nil {
		return store.ManagedPreview{}, err
	}
	if _, _, err := s.prepare(view, m); err != nil {
		return store.ManagedPreview{}, err
	}
	registry, err := json.Marshal(m.Registry)
	if err != nil {
		return store.ManagedPreview{}, ErrInvalidConfiguration
	}
	previewID := strings.ToLower(rand.Text())
	payload, err := json.Marshal(PreviewJobPayload{Marketplace: id, PreviewID: previewID})
	if err != nil {
		return store.ManagedPreview{}, ErrInvalidConfiguration
	}
	jobID := store.JobID(PreviewJobKind + "-" + previewID)
	return previews.CreateManagedPreview(ctx, store.ManagedPreview{
		ID:           previewID,
		Marketplace:  id,
		Actor:        m.Actor,
		BaseRevision: view.Revision,
		Registry:     registry,
		Existing:     slices.ContainsFunc(view.Snapshot.Registries, func(r Registry) bool { return r.ID == m.Registry.ID }),
	}, store.Job{ID: jobID, Kind: PreviewJobKind, Payload: payload, IdempotencyKey: string(jobID), MaxAttempts: 1})
}

// GetPreview returns one preview scoped to the Marketplace.
func (s Service) GetPreview(ctx context.Context, id store.MarketplaceID, previewID string) (store.ManagedPreview, error) {
	previews, ok := s.Metadata.(store.ManagedPreviewStore)
	if !ok {
		return store.ManagedPreview{}, store.ErrTopology
	}
	return previews.GetManagedPreview(ctx, id, previewID)
}
