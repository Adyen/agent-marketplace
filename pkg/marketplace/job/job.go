package job

import "github.com/Adyen/agent-marketplace/pkg/marketplace/store"

// Job is an alias to the durable job type in [store].
type Job = store.Job

// Status is an alias to [store.JobStatus].
type Status = store.JobStatus

// Status constants.
const (
	Pending        = store.JobStatusPending
	Leased         = store.JobStatusLeased
	Completed      = store.JobStatusCompleted
	Failed         = store.JobStatusFailed
	TerminalFailed = store.JobStatusTerminalFailed
)
