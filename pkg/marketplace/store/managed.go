package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrManagedRevisionConflict    = errors.New("marketplace/store: managed revision conflict")
	ErrManagedIdempotencyConflict = errors.New("marketplace/store: managed idempotency conflict")
	ErrManagedStateCorrupt        = errors.New("marketplace/store: managed state corrupt")
	ErrManagedChangeInvalid       = errors.New("marketplace/store: invalid managed change")
)

type ManagedActor struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

type ManagedChange struct {
	Marketplace    MarketplaceID   `json:"marketplace_id"`
	Revision       int64           `json:"revision"`
	ParentRevision int64           `json:"parent_revision"`
	Actor          ManagedActor    `json:"actor"`
	Operation      string          `json:"operation"`
	Reason         string          `json:"reason"`
	RequestID      string          `json:"request_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	RequestDigest  string          `json:"request_digest"`
	Snapshot       json.RawMessage `json:"snapshot"`
	JobIDs         []JobID         `json:"job_ids"`
	CreatedAt      time.Time       `json:"created_at"`
}

type ManagedChangeRequest struct {
	Change ManagedChange
	Jobs   []Job
}

type ManagedConfigurationStore interface {
	FindManagedChange(context.Context, MarketplaceID, ManagedActor, string, string) (ManagedChange, error)
	CurrentManagedConfiguration(context.Context, MarketplaceID) (ManagedChange, error)
	ManagedConfigurationHistory(context.Context, MarketplaceID, int64, int) ([]ManagedChange, error)
	ApplyManagedConfiguration(context.Context, ManagedChangeRequest) (ManagedChange, error)
}
