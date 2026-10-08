package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type managedRefreshJobPayload struct {
	Marketplace        MarketplaceID `json:"marketplace_id"`
	RegistryID         RegistryID    `json:"registry_id"`
	RegistryGeneration int64         `json:"registry_generation"`
	PolicyRevision     int64         `json:"policy_revision"`
	Revision           int64         `json:"revision"`
	ParentJobID        JobID         `json:"parent_job_id,omitempty"`
	Operation          string        `json:"operation"`
}

func decodeManagedRefreshJob(job Job) (managedRefreshJobPayload, error) {
	var payload managedRefreshJobPayload
	if job.Kind != "managed-refresh" || !managedToken(string(job.ID)) || len(job.Payload) > 65536 || json.Unmarshal(job.Payload, &payload) != nil || !managedToken(string(payload.Marketplace)) || !managedToken(string(payload.RegistryID)) || payload.RegistryGeneration < 1 || payload.PolicyRevision < 1 || payload.Revision < 1 {
		return payload, ErrManagedAttemptInvalid
	}
	switch payload.Operation {
	case "reconcile":
		if !managedToken(string(payload.ParentJobID)) {
			return payload, ErrManagedAttemptInvalid
		}
	case "scheduled", "manual":
		if payload.ParentJobID != "" {
			return payload, ErrManagedAttemptInvalid
		}
	default:
		return payload, ErrManagedAttemptInvalid
	}
	return payload, nil
}

func validManagedOutcome(outcome string) bool {
	switch outcome {
	case "started", "failed", "superseded", "succeeded":
		return true
	default:
		return false
	}
}

func decodeManagedRefreshEvent(record string, marketplace MarketplaceID) (ManagedRefreshAttempt, error) {
	var event ManagedRefreshAttempt
	if len(record) > 65536 || json.Unmarshal([]byte(record), &event) != nil || event.Marketplace != marketplace || event.Sequence < 1 || event.Attempt < 1 || event.RegistryGeneration < 1 || event.PolicyRevision < 1 || event.ConfigurationRevision < 1 || !managedToken(string(event.Registry)) || event.JobID == "" || event.WorkerID == "" || !validManagedOutcome(event.Outcome) || (event.PublicationID != "") != (event.Outcome == "succeeded") ||
		(event.Outcome == "succeeded" || event.Outcome == "started") && (event.FailureCode != "" || event.FailureCause != "") || len(event.FailureCode) > 128 || len(event.FailureCause) > 2048 {
		return event, ErrManagedStateCorrupt
	}
	return event, nil
}

func ReadManagedRefreshAttempts(ctx context.Context, q ManagedSQLQueries, request ManagedRefreshAttemptQuery) ([]ManagedRefreshAttempt, error) {
	if request.Marketplace == "" || request.Limit < 1 || request.Limit > 100 || request.Before < 0 {
		return nil, ErrManagedAttemptInvalid
	}
	query := `SELECT record FROM managed_refresh_events WHERE marketplace_id=$1`
	args := []any{request.Marketplace}
	if request.Registry != "" {
		args = append(args, request.Registry)
		query += fmt.Sprintf(" AND registry_id=$%d", len(args))
	}
	if request.JobID != "" {
		args = append(args, request.JobID)
		query += fmt.Sprintf(" AND job_id=$%d", len(args))
	}
	if request.Before > 0 {
		args = append(args, request.Before)
		query += fmt.Sprintf(" AND sequence<$%d", len(args))
	}
	args = append(args, request.Limit)
	query += fmt.Sprintf(" ORDER BY sequence DESC LIMIT $%d", len(args))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]ManagedRefreshAttempt, 0, request.Limit)
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			return nil, err
		}
		event, err := decodeManagedRefreshEvent(record, request.Marketplace)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func ReadManagedRefreshJob(ctx context.Context, meta MetadataStore, marketplace MarketplaceID, id JobID) (ManagedRefreshJobView, error) {
	job, err := meta.GetJob(ctx, id)
	if err != nil {
		return ManagedRefreshJobView{}, err
	}
	payload, err := decodeManagedRefreshJob(job)
	if err != nil {
		return ManagedRefreshJobView{}, err
	}
	if payload.Marketplace != marketplace {
		return ManagedRefreshJobView{}, ErrNotFound
	}
	result := ManagedRefreshJobView{JobID: job.ID, Marketplace: marketplace, Registry: payload.RegistryID, ParentJobID: payload.ParentJobID, RegistryGeneration: payload.RegistryGeneration, PolicyRevision: payload.PolicyRevision, ConfigurationRevision: payload.Revision, Operation: payload.Operation, RawStatus: job.Status, Attempt: job.Attempt, MaxAttempts: job.MaxAttempts, LeaseUntil: job.LeaseUntil}
	if job.MaxAttempts < 1 || job.Attempt < 0 {
		return ManagedRefreshJobView{}, ErrManagedStateCorrupt
	}
	switch job.Status {
	case JobStatusCompleted:
		result.State = "completed"
		result.Terminal = true
	case JobStatusFailed:
		result.State = "failed"
		result.Terminal = job.Attempt >= job.MaxAttempts
	case JobStatusPending, JobStatusLeased:
		result.State = string(job.Status)
		if job.Status == JobStatusPending || !job.LeaseUntil.After(time.Now().UTC()) {
			if job.Attempt >= job.MaxAttempts {
				result.State = "exhausted"
				result.Terminal = true
			} else if job.Status == JobStatusLeased {
				result.State = "lease_expired"
			}
		}
	default:
		return ManagedRefreshJobView{}, ErrManagedStateCorrupt
	}
	return result, nil
}

func ReadManagedRegistryRefreshState(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, marketplace MarketplaceID, registry RegistryID) (ManagedRegistryRefreshState, error) {
	var attemptRecord string
	var successRecord sql.NullString
	err := q.QueryRowContext(ctx, `SELECT a.record,s.record FROM managed_registry_refresh_status h
 JOIN managed_refresh_events a ON a.marketplace_id=h.marketplace_id AND a.sequence=h.last_attempt_sequence
 LEFT JOIN managed_refresh_events s ON s.marketplace_id=h.marketplace_id AND s.sequence=h.last_success_sequence
 WHERE h.marketplace_id=$1 AND h.registry_id=$2`, marketplace, registry).Scan(&attemptRecord, &successRecord)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedRegistryRefreshState{}, ErrNotFound
	}
	if err != nil {
		return ManagedRegistryRefreshState{}, err
	}
	attempt, err := decodeManagedRefreshEvent(attemptRecord, marketplace)
	if err != nil {
		return ManagedRegistryRefreshState{}, err
	}
	if attempt.Registry != registry {
		return ManagedRegistryRefreshState{}, ErrManagedStateCorrupt
	}
	result := ManagedRegistryRefreshState{LastAttempt: &attempt, CurrentState: attempt.Outcome}
	if successRecord.Valid {
		success, err := decodeManagedRefreshEvent(successRecord.String, marketplace)
		if err != nil {
			return result, err
		}
		if success.Registry != registry || success.Outcome != "succeeded" {
			return ManagedRegistryRefreshState{}, ErrManagedStateCorrupt
		}
		result.LastSuccess = &success
	}
	if attempt.Outcome == "started" {
		job, err := ReadManagedRefreshJob(ctx, meta, marketplace, attempt.JobID)
		if err != nil {
			return ManagedRegistryRefreshState{}, err
		}
		result.CurrentState = job.State
		if job.Attempt > attempt.Attempt {
			result.CurrentState = "superseded"
		}
	}
	return result, nil
}

func CommitManagedRefreshAttempt(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, fence ManagedFence, outcome string, publication PublicationID, failures ...ManagedRefreshFailure) error {
	if len(publication) > 256 || len(fence.ExpectedPublicationID) > 256 || strings.ContainsAny(string(publication)+string(fence.ExpectedPublicationID), "\x00\r\n") {
		return ErrManagedAttemptInvalid
	}
	if !validManagedOutcome(outcome) || (publication != "") != (outcome == "succeeded") || !managedToken(string(fence.Marketplace)) || !managedToken(string(fence.JobID)) || fence.Attempt < 1 || fence.RegistryGeneration < 1 || fence.PolicyRevision < 1 || fence.WorkerID == "" || len(fence.WorkerID) > 256 || strings.ContainsAny(fence.WorkerID, "\x00\r\n") {
		return ErrManagedAttemptInvalid
	}
	var failure ManagedRefreshFailure
	if len(failures) > 0 {
		failure = failures[0]
	}
	if len(failures) > 1 || outcome == "started" && (failure.Code != "" || failure.Cause != "") || outcome == "succeeded" && (failure.Code != "" || failure.Cause != "") ||
		len(failure.Code) > 128 || len(failure.Cause) > 2048 || strings.ContainsAny(failure.Code+failure.Cause, "\x00\r\n") {
		return ErrManagedAttemptInvalid
	}
	result, err := q.ExecContext(ctx, `UPDATE managed_configuration_heads SET revision=revision WHERE marketplace_id=$1`, fence.Marketplace)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrPublicationFence
	}
	if _, err := q.ExecContext(ctx, `UPDATE jobs SET attempt=attempt WHERE id=$1`, fence.JobID); err != nil {
		return err
	}
	job, err := meta.GetJob(ctx, fence.JobID)
	if err != nil {
		return err
	}
	if job.Status != JobStatusLeased || job.WorkerID != fence.WorkerID || job.Attempt != fence.Attempt || !job.LeaseUntil.After(time.Now().UTC()) {
		return ErrLeaseExpired
	}
	payload, err := decodeManagedRefreshJob(job)
	if err != nil {
		return err
	}
	if payload.Marketplace != fence.Marketplace || payload.RegistryGeneration != fence.RegistryGeneration || payload.PolicyRevision != fence.PolicyRevision {
		return ErrPublicationFence
	}
	var changeRecord string
	if err := q.QueryRowContext(ctx, `SELECT record FROM managed_configuration_changes WHERE marketplace_id=$1 AND revision=$2`, payload.Marketplace, payload.Revision).Scan(&changeRecord); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrManagedAttemptInvalid
		}
		return fmt.Errorf("read managed refresh request: %w", err)
	}
	change, err := decodeManagedChange(changeRecord, payload.Marketplace, payload.Revision)
	if err != nil {
		return err
	}
	event := ManagedRefreshAttempt{Marketplace: payload.Marketplace, Registry: payload.RegistryID, JobID: job.ID, ParentJobID: payload.ParentJobID, Attempt: job.Attempt, WorkerID: fence.WorkerID, RegistryGeneration: payload.RegistryGeneration, PolicyRevision: payload.PolicyRevision, ConfigurationRevision: payload.Revision, Operation: payload.Operation, Outcome: outcome, PublicationID: publication, ExpectedPublicationID: fence.ExpectedPublicationID, FailureCode: failure.Code, FailureCause: failure.Cause}
	if payload.ParentJobID != "" {
		parent, err := meta.GetJob(ctx, payload.ParentJobID)
		if errors.Is(err, ErrNotFound) {
			return ErrManagedAttemptInvalid
		}
		if err != nil {
			return err
		}
		var parentPayload struct {
			Marketplace MarketplaceID `json:"marketplace_id"`
			Revision    int64         `json:"revision"`
			Registry    RegistryID    `json:"registry_id"`
		}
		if parent.Kind != "managed-reconcile" || len(parent.Payload) > 65536 || json.Unmarshal(parent.Payload, &parentPayload) != nil || parentPayload.Marketplace != payload.Marketplace || parentPayload.Revision != payload.Revision || (parentPayload.Registry != "" && parentPayload.Registry != payload.RegistryID) {
			return ErrManagedAttemptInvalid
		}
		found := false
		for _, id := range change.JobIDs {
			if id == payload.ParentJobID {
				found = true
			}
		}
		if !found {
			return ErrManagedAttemptInvalid
		}
		event.RequestID = change.RequestID
		event.Actor = change.Actor
		event.ChangeOperation = change.Operation
	}
	rows, err := q.QueryContext(ctx, `SELECT record FROM managed_refresh_events WHERE job_id=$1 AND attempt=$2 ORDER BY sequence LIMIT 3`, job.ID, job.Attempt)
	if err != nil {
		return err
	}
	var started, terminal *ManagedRefreshAttempt
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			_ = rows.Close()
			return err
		}
		prior, err := decodeManagedRefreshEvent(record, fence.Marketplace)
		if err != nil {
			_ = rows.Close()
			return err
		}
		if prior.Registry != event.Registry || prior.WorkerID != event.WorkerID || prior.RegistryGeneration != event.RegistryGeneration || prior.PolicyRevision != event.PolicyRevision || prior.ExpectedPublicationID != event.ExpectedPublicationID || prior.ConfigurationRevision != event.ConfigurationRevision {
			_ = rows.Close()
			return ErrConflict
		}
		if prior.Outcome == "started" {
			if started != nil {
				_ = rows.Close()
				return ErrManagedStateCorrupt
			}
			started = &prior
		} else {
			if terminal != nil {
				_ = rows.Close()
				return ErrManagedStateCorrupt
			}
			terminal = &prior
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if terminal != nil {
		if terminal.Outcome == outcome && terminal.PublicationID == publication {
			return nil
		}
		return ErrConflict
	}
	if outcome == "succeeded" && started == nil {
		return ErrManagedAttemptInvalid
	}
	if outcome == "started" || outcome == "succeeded" {
		req := RegistryRevisionPublication{Occurrence: RegistryOccurrence{RegistryID: payload.RegistryID}, ManagedFence: &fence}
		if err := lockPublicationFence(ctx, q, req); err != nil {
			return err
		}
		if outcome == "started" && started != nil {
			return nil
		}
		current, err := ReadRegistryPublication(ctx, q, payload.RegistryID, "")
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if outcome == "started" {
			if current.ID != fence.ExpectedPublicationID {
				return ErrPublicationFence
			}
		} else {
			if current.ID != publication || current.RegistryGeneration != fence.RegistryGeneration || current.PolicyRevision != fence.PolicyRevision || current.JobID != fence.JobID || current.WorkerID != fence.WorkerID || current.Attempt != fence.Attempt {
				return ErrPublicationFence
			}
		}
	}
	var sequence int64
	if err := q.QueryRowContext(ctx, `SELECT attempt_sequence FROM managed_configuration_heads WHERE marketplace_id=$1`, fence.Marketplace).Scan(&sequence); err != nil {
		return err
	}
	if sequence < 0 || sequence == math.MaxInt64 {
		return ErrManagedStateCorrupt
	}
	event.Sequence = sequence + 1
	event.RecordedAt = time.Now().UTC()
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO managed_refresh_events (marketplace_id,sequence,registry_id,job_id,attempt,outcome,record) VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.Marketplace, event.Sequence, event.Registry, event.JobID, event.Attempt, event.Outcome, string(encoded)); err != nil {
		return fmt.Errorf("append managed refresh event: %w", err)
	}
	if _, err := q.ExecContext(ctx, `UPDATE managed_configuration_heads SET attempt_sequence=$1 WHERE marketplace_id=$2`, event.Sequence, event.Marketplace); err != nil {
		return err
	}
	if outcome == "started" {
		_, err = q.ExecContext(ctx, `INSERT INTO managed_registry_refresh_status (marketplace_id,registry_id,last_attempt_sequence) VALUES ($1,$2,$3) ON CONFLICT(marketplace_id,registry_id) DO UPDATE SET last_attempt_sequence=excluded.last_attempt_sequence`, event.Marketplace, event.Registry, event.Sequence)
	} else if started == nil {
		_, err = q.ExecContext(ctx, `INSERT INTO managed_registry_refresh_status (marketplace_id,registry_id,last_attempt_sequence) VALUES ($1,$2,$3) ON CONFLICT(marketplace_id,registry_id) DO NOTHING`, event.Marketplace, event.Registry, event.Sequence)
	} else {
		_, err = q.ExecContext(ctx, `UPDATE managed_registry_refresh_status SET last_attempt_sequence=$1 WHERE marketplace_id=$2 AND registry_id=$3 AND last_attempt_sequence=$4`, event.Sequence, event.Marketplace, event.Registry, started.Sequence)
	}
	if err != nil {
		return err
	}
	var statusRows int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_registry_refresh_status WHERE marketplace_id=$1 AND registry_id=$2`, event.Marketplace, event.Registry).Scan(&statusRows); err != nil {
		return err
	}
	if statusRows != 1 {
		return ErrManagedStateCorrupt
	}
	if outcome == "succeeded" {
		if _, err := q.ExecContext(ctx, `UPDATE managed_registry_refresh_status SET last_success_sequence=$1 WHERE marketplace_id=$2 AND registry_id=$3`, event.Sequence, event.Marketplace, event.Registry); err != nil {
			return err
		}
	}
	var valid int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id=$1 AND status='leased' AND worker_id=$2 AND attempt=$3 AND lease_until>$4`, job.ID, fence.WorkerID, fence.Attempt, time.Now().UTC().UnixMicro()).Scan(&valid); err != nil {
		return err
	}
	if valid != 1 {
		return ErrLeaseExpired
	}
	return nil
}
