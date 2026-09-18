package store

import "context"

type PublicationID string

type ManagedFence struct {
	Marketplace           MarketplaceID
	RegistryGeneration    int64
	PolicyRevision        int64
	JobID                 JobID
	WorkerID              string
	Attempt               int
	ExpectedPublicationID PublicationID
}

type RegistryPublication struct {
	ID                 PublicationID        `json:"id"`
	Occurrence         RegistryOccurrence   `json:"occurrence"`
	Resources          []ResourceOccurrence `json:"resources"`
	Assessments        []ResourceAssessment `json:"assessments"`
	RegistryGeneration int64                `json:"registry_generation,omitempty"`
	PolicyRevision     int64                `json:"policy_revision,omitempty"`
	JobID              JobID                `json:"job_id,omitempty"`
	WorkerID           string               `json:"worker_id,omitempty"`
	Attempt            int                  `json:"attempt,omitempty"`
}

type PublicationStore interface {
	PublishRegistryPublication(context.Context, RegistryRevisionPublication) error
	GetRegistryPublication(context.Context, RegistryID, PublicationID) (RegistryPublication, error)
	CurrentRegistryPublication(context.Context, RegistryID) (RegistryPublication, error)
}
