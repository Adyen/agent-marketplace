package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RegistryConfigurationChange is one registry's durable state in a runtime
// operator-configuration candidate.
type RegistryConfigurationChange struct {
	Registry           Registry
	DesiredGeneration  string
	ClearDesiredCommit bool
	DueAt              time.Time
}

// ApplyRegistryConfiguration commits every registry and observation change for
// one runtime configuration candidate atomically.
func ApplyRegistryConfiguration(ctx context.Context, meta MetadataStore, changes []RegistryConfigurationChange) error {
	return meta.WithTx(ctx, func(txCtx context.Context, tx MetadataStore) error {
		for _, change := range changes {
			if err := tx.PutRegistry(txCtx, change.Registry); err != nil {
				return fmt.Errorf("write configured registry %q: %w", change.Registry.ID, err)
			}
			observation, err := tx.GetRegistryObservation(txCtx, change.Registry.ID)
			if errors.Is(err, ErrNotFound) {
				observation, err = tx.CreateRegistryObservation(txCtx, RegistryObservation{RegistryID: change.Registry.ID})
			}
			if err != nil {
				return fmt.Errorf("prepare observation for registry %q: %w", change.Registry.ID, err)
			}
			if change.DesiredGeneration == "" && !change.ClearDesiredCommit && change.DueAt.IsZero() {
				continue
			}
			next := observation
			if change.DesiredGeneration != "" {
				next.DesiredGeneration = change.DesiredGeneration
			}
			if change.ClearDesiredCommit {
				next.DesiredCommit = ""
			}
			if !change.DueAt.IsZero() {
				next.NextProbeAt = change.DueAt
				if next.NextReconcileAt.Before(change.DueAt) {
					next.NextReconcileAt = change.DueAt
				}
			}
			if _, err := tx.UpdateRegistryObservation(txCtx, observation.Version, next); err != nil {
				return fmt.Errorf("update observation for registry %q: %w", change.Registry.ID, err)
			}
		}
		return nil
	})
}
