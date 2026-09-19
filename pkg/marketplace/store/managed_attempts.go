package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrManagedAttemptInvalid   = errors.New("marketplace/store: invalid managed refresh attempt")
	ErrManagedIdentityConflict = errors.New("marketplace/store: managed registry identity conflict")
)

type ManagedRefreshAttempt struct {
	Sequence              int64         `json:"sequence"`
	Marketplace           MarketplaceID `json:"marketplace_id"`
	Registry              RegistryID    `json:"registry_id"`
	JobID                 JobID         `json:"job_id"`
	ParentJobID           JobID         `json:"parent_job_id,omitempty"`
	Attempt               int           `json:"attempt"`
	WorkerID              string        `json:"worker_id"`
	RegistryGeneration    int64         `json:"registry_generation"`
	PolicyRevision        int64         `json:"policy_revision"`
	ConfigurationRevision int64         `json:"configuration_revision"`
	RequestID             string        `json:"request_id,omitempty"`
	Actor                 ManagedActor  `json:"actor"`
	Operation             string        `json:"operation"`
	ChangeOperation       string        `json:"change_operation,omitempty"`
	Outcome               string        `json:"outcome"`
	PublicationID         PublicationID `json:"publication_id,omitempty"`
	ExpectedPublicationID PublicationID `json:"expected_publication_id,omitempty"`
	RecordedAt            time.Time     `json:"recorded_at"`
}

type ManagedRefreshAttemptQuery struct {
	Marketplace MarketplaceID
	Registry    RegistryID
	JobID       JobID
	Before      int64
	Limit       int
}

type ManagedRegistryRefreshState struct {
	LastAttempt  *ManagedRefreshAttempt `json:"last_attempt,omitempty"`
	LastSuccess  *ManagedRefreshAttempt `json:"last_success,omitempty"`
	CurrentState string                 `json:"current_state"`
}

type ManagedRefreshJobView struct {
	JobID                 JobID         `json:"job_id"`
	Marketplace           MarketplaceID `json:"marketplace_id"`
	Registry              RegistryID    `json:"registry_id"`
	ParentJobID           JobID         `json:"parent_job_id,omitempty"`
	RegistryGeneration    int64         `json:"registry_generation"`
	PolicyRevision        int64         `json:"policy_revision"`
	ConfigurationRevision int64         `json:"configuration_revision"`
	Operation             string        `json:"operation"`
	RawStatus             JobStatus     `json:"raw_status"`
	State                 string        `json:"state"`
	Terminal              bool          `json:"terminal"`
	Attempt               int           `json:"attempt"`
	MaxAttempts           int           `json:"max_attempts"`
	LeaseUntil            time.Time     `json:"lease_until,omitempty"`
}

type ManagedRefreshAttemptStore interface {
	RecordManagedRefreshAttempt(context.Context, ManagedFence, string, PublicationID) error
	ListManagedRefreshAttempts(context.Context, ManagedRefreshAttemptQuery) ([]ManagedRefreshAttempt, error)
	GetManagedRegistryRefreshState(context.Context, MarketplaceID, RegistryID) (ManagedRegistryRefreshState, error)
	GetManagedRefreshJob(context.Context, MarketplaceID, JobID) (ManagedRefreshJobView, error)
}
