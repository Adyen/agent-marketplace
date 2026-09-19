package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrManagedPreviewInvalid = errors.New("marketplace/store: invalid managed preview")

// ManagedPreviewStatus is the lifecycle state of a discovery preview.
const (
	ManagedPreviewPending   = "pending"
	ManagedPreviewRunning   = "running"
	ManagedPreviewSucceeded = "succeeded"
	ManagedPreviewFailed    = "failed"
)

// ManagedPreviewResultLimit bounds how many resources one root lists in a
// preview record. Counts remain exact; the list is marked truncated.
const ManagedPreviewResultLimit = 200

// ManagedPreview is an admin-requested acquisition and discovery preview of a
// proposed registry configuration. It never publishes, never advances registry
// heads, and never records credentials or raw Git output.
type ManagedPreview struct {
	ID           string        `json:"id"`
	Marketplace  MarketplaceID `json:"marketplace_id"`
	Actor        ManagedActor  `json:"actor"`
	BaseRevision int64         `json:"base_revision"`
	// Registry is the immutable proposed registry the preview was requested
	// for, encoded as the admin DTO. Clients compare it against their draft to
	// detect staleness.
	Registry json.RawMessage `json:"registry"`
	// Existing reports whether the registry identity already existed at
	// BaseRevision; only then is a diff against the current publication made.
	Existing    bool                   `json:"existing"`
	JobID       JobID                  `json:"job_id"`
	Status      string                 `json:"status"`
	Result      *ManagedPreviewResult  `json:"result,omitempty"`
	Failure     *ManagedPreviewFailure `json:"failure,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	CompletedAt time.Time              `json:"completed_at,omitzero"`
}

type ManagedPreviewResult struct {
	Commit         string                     `json:"commit"`
	Ref            string                     `json:"ref"`
	ObservedAt     time.Time                  `json:"observed_at"`
	PolicyRevision int64                      `json:"policy_revision"`
	DiffBase       PublicationID              `json:"diff_base,omitempty"`
	TotalResources int                        `json:"total_resources"`
	Roots          []ManagedPreviewRoot       `json:"roots"`
	Removed        []ManagedPreviewResource   `json:"removed,omitempty"`
	Diagnostics    []ManagedPreviewDiagnostic `json:"diagnostics,omitempty"`
}

type ManagedPreviewRoot struct {
	Path          string                   `json:"path"`
	SourceHarness string                   `json:"source_harness,omitempty"`
	ResourceCount int                      `json:"resource_count"`
	Truncated     bool                     `json:"truncated,omitempty"`
	Resources     []ManagedPreviewResource `json:"resources"`
}

type ManagedPreviewResource struct {
	Kind        string             `json:"kind"`
	Name        string             `json:"name"`
	SourcePath  string             `json:"source_path,omitempty"`
	Description string             `json:"description,omitempty"`
	Decision    AssessmentDecision `json:"decision,omitempty"`
	Findings    int                `json:"findings,omitempty"`
	// Change is "added", "changed", "unchanged" or "removed" relative to the
	// current publication; empty for new registries.
	Change string `json:"change,omitempty"`
}

type ManagedPreviewDiagnostic struct {
	Root    string `json:"root"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   bool   `json:"error,omitempty"`
}

type ManagedPreviewFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ManagedPreviewStore persists discovery previews. Creation enqueues the
// preview job in the same transaction; completion is fenced on the job lease.
type ManagedPreviewStore interface {
	CreateManagedPreview(context.Context, ManagedPreview, Job) (ManagedPreview, error)
	GetManagedPreview(context.Context, MarketplaceID, string) (ManagedPreview, error)
	// UpdateManagedPreview replaces the record while the preview is not yet
	// terminal. It returns ErrConflict once a terminal status was recorded.
	UpdateManagedPreview(context.Context, ManagedPreview) error
	PruneManagedPreviews(context.Context, MarketplaceID, time.Time) error
}

func (p ManagedPreview) Terminal() bool {
	return p.Status == ManagedPreviewSucceeded || p.Status == ManagedPreviewFailed
}
