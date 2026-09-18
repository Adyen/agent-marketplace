package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type authorityStore struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	readErr error
}

func (s authorityStore) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{Marketplace: "market", Revision: 1}, s.readErr
}

func TestConfigurationAuthority(t *testing.T) {
	outage := errors.New("database unavailable")
	for _, tt := range []struct {
		name, mode    string
		readErr, want error
	}{
		{"default unadopted", "", store.ErrNotFound, nil},
		{"file unadopted", "file", store.ErrNotFound, nil},
		{"file adopted", "file", nil, store.ErrAuthorityModeConflict},
		{"default adopted", "", nil, store.ErrAuthorityModeConflict},
		{"database unadopted", "database", store.ErrNotFound, store.ErrManagedAdoptionRequired},
		{"database adopted unsupported runtime", "database", nil, store.ErrManagedRuntimeUnavailable},
		{"file corrupt", "file", store.ErrManagedStateCorrupt, store.ErrManagedStateCorrupt},
		{"database corrupt", "database", store.ErrManagedStateCorrupt, store.ErrManagedStateCorrupt},
		{"file outage", "file", outage, outage},
		{"database outage", "database", outage, outage},
		{"unknown mode", "typo", store.ErrNotFound, store.ErrAuthorityModeConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := store.CheckConfigurationAuthority(context.Background(), authorityStore{readErr: tt.readErr}, "market", tt.mode)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
	if err := store.CheckConfigurationAuthority(context.Background(), nil, "market", "file"); !errors.Is(err, store.ErrTopology) {
		t.Fatalf("missing capability: %v", err)
	}
}
