package store

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrAuthorityModeConflict     = errors.New("marketplace/store: configuration authority mode conflict")
	ErrManagedAdoptionRequired   = errors.New("marketplace/store: managed configuration adoption required")
	ErrManagedRuntimeUnavailable = errors.New("marketplace/store: managed runtime safety capabilities unavailable")
)

// CheckConfigurationAuthority fails closed before legacy readers or writers can
// use deployment configuration against an adopted database. Absence of the
// capability is not proof of absence of the durable adoption marker.
//
// Database mode requires publication identity, transactional worker fencing,
// refresh history and blob ownership capabilities on the configured store.
// Accepting configuration revisions alone is not safe enablement.
func CheckConfigurationAuthority(ctx context.Context, meta MetadataStore, id MarketplaceID, mode string) error {
	if mode != "" && mode != "file" && mode != "database" {
		return ErrAuthorityModeConflict
	}
	managed, ok := meta.(interface {
		CurrentManagedConfiguration(context.Context, MarketplaceID) (ManagedChange, error)
	})
	if !ok {
		return ErrTopology
	}
	_, err := managed.CurrentManagedConfiguration(ctx, id)
	if errors.Is(err, ErrNotFound) {
		if mode == "database" {
			return ErrManagedAdoptionRequired
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("check configuration authority: %w", err)
	}
	if mode != "database" {
		return ErrAuthorityModeConflict
	}
	if _, ok := meta.(interface {
		ManagedConfigurationStore
		PublicationStore
		ManagedRefreshAttemptStore
		BlobOwnerStore
		BlobOwnerMaintenanceStore
	}); !ok {
		return ErrManagedRuntimeUnavailable
	}
	return nil
}
