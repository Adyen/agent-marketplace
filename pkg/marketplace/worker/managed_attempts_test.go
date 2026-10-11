package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestManagedPreflightRecordsSupersededWithoutStarted(t *testing.T) {
	ctx := context.Background()
	deps, service := managedFixture(t)
	attempts := deps.MetadataStore.(store.ManagedRefreshAttemptStore)
	if err := scheduleManaged(ctx, deps, "prior"); err != nil {
		t.Fatal(err)
	}
	prior, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "prior", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedRefresh(ctx, deps, prior, "prior", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return nil, ErrManagedRefresh
	}); !errors.Is(err, ErrManagedRefresh) {
		t.Fatal(err)
	}
	before, err := attempts.GetManagedRegistryRefreshState(ctx, "test-mp", "src-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduleManaged(ctx, deps, "stale"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(ctx, "test-mp", admin.Mutation{Action: "pause", RegistryID: "src-test", ExpectedRevision: 1, Actor: store.ManagedActor{Issuer: "test", Subject: "operator"}, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"})
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		t.Fatal("stale job acquired")
		return nil, nil
	})
	if !errors.Is(err, ErrManagedSuperseded) {
		t.Fatalf("preflight: %v", err)
	}
	events, err := attempts.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
	if err != nil || len(events) != 1 || events[0].Outcome != "superseded" {
		t.Fatalf("events: %+v %v", events, err)
	}
	after, err := attempts.GetManagedRegistryRefreshState(ctx, "test-mp", "src-test")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("obsolete attempt changed aggregate: %+v %+v %v", before, after, err)
	}
	terminal, err := attempts.GetManagedRefreshJob(ctx, "test-mp", j.ID)
	if err != nil || !terminal.Terminal {
		t.Fatalf("job not terminal: %+v %v", terminal, err)
	}
}

func TestManagedManualReindexAuditAttribution(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	deps.ReindexActor = store.ManagedActor{Issuer: "test-cli", Subject: "operator-123"}
	view, err := managedView(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	id, err := enqueueManagedReindex(ctx, deps, view, view.Snapshot.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJobByID(ctx, id, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return nil, errors.New("private acquisition diagnostic")
	})
	if !errors.Is(err, ErrManagedRefresh) {
		t.Fatalf("refresh result: %v", err)
	}
	events, err := deps.MetadataStore.(store.ManagedRefreshAttemptStore).ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: id, Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("attempts: %+v %v", events, err)
	}
	event := events[0]
	if event.Actor != deps.ReindexActor || event.ChangeOperation != "reindex" || event.Operation != "reconcile" || event.ParentJobID == "" || event.RequestID == "" || event.ConfigurationRevision != 2 || event.Outcome != "failed" {
		t.Fatalf("missing audited attribution: %+v", event)
	}
	parent, err := deps.MetadataStore.GetJob(ctx, event.ParentJobID)
	if err != nil || parent.Status != store.JobStatusCompleted {
		t.Fatalf("manual parent left pending: %+v %v", parent, err)
	}
	if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "unexpected", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("parent duplicated child: %v", err)
	}
}

// spec: marketplace-publication/acquisition-failure-records-a-sanitized-cause
func TestManagedAcquisitionFailureRecordsSanitizedCause(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	capture := &capturedWorkerLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if err := scheduleManaged(ctx, deps, "scheduled"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return nil, errors.New("raw git output: credential=hunter2 stderr trace")
	})
	if !errors.Is(err, ErrManagedRefresh) {
		t.Fatalf("refresh result: %v", err)
	}
	events, err := deps.MetadataStore.(store.ManagedRefreshAttemptStore).ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var failed *store.ManagedRefreshAttempt
	for i := range events {
		if events[i].Outcome == "failed" {
			failed = &events[i]
		}
	}
	if failed == nil {
		t.Fatalf("no failed attempt recorded: %+v", events)
	}
	if failed.FailureCode != "acquisition/scheduled" {
		t.Fatalf("failure code = %q", failed.FailureCode)
	}
	if failed.FailureCause == "" || strings.Contains(failed.FailureCause, "credential=hunter2") || strings.Contains(failed.FailureCause, "stderr") {
		t.Fatalf("failure cause leaked raw output: %q", failed.FailureCause)
	}
	found := false
	for _, record := range capture.records {
		if !strings.Contains(record.Message, "managed refresh attempt failed") {
			continue
		}
		var code, cause string
		record.Attrs(func(a slog.Attr) bool {
			if a.Key == "failure_code" {
				code = a.Value.String()
			}
			if a.Key == "cause" {
				cause = a.Value.String()
			}
			return true
		})
		if code == failed.FailureCode && cause == failed.FailureCause {
			found = true
		}
	}
	if !found {
		t.Fatal("worker log line did not carry the recorded failure code and cause")
	}
}

type capturedWorkerLogs struct {
	records []slog.Record
}

func (h *capturedWorkerLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturedWorkerLogs) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *capturedWorkerLogs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturedWorkerLogs) WithGroup(string) slog.Handler      { return h }

// spec: marketplace-publication/failed-publication-records-the-conflict-cause
func TestManagedPublicationConflictRecordsCause(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	capture := &capturedWorkerLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	repo, cleanup := createTestRepo(t)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	view, err := managedView(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	cfg, registry := managedInputs(deps, view, view.Snapshot.Registries[0])
	revision, err := ingest.BuildRegistryRevision(ctx, cfg, store.MarketplaceID(deps.Config.Instance.ID), registry, repo, commit)
	if err != nil || len(revision.Resources) == 0 {
		t.Fatalf("build seeded revision: %v", err)
	}
	if err := deps.MetadataStore.PutRegistry(ctx, store.Registry{ID: registry.ID, Slug: registry.Slug, Ref: registry.Ref, Acquisition: registry.Acquisition, Listed: true}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range revision.Resources {
		conflicting := resource
		conflicting.License = "Apache-2.0"
		if err := deps.MetadataStore.PutResourceRevision(ctx, conflicting); err != nil {
			t.Fatalf("seed conflicting revision: %v", err)
		}
	}
	if err := scheduleManaged(ctx, deps, "conflict"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return &acquire.Result{Dir: repo, Commit: commit}, nil
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("refresh result: %v, want ErrConflict", err)
	}

	attempts := deps.MetadataStore.(store.ManagedRefreshAttemptStore)
	events, err := attempts.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var failed *store.ManagedRefreshAttempt
	for i := range events {
		if events[i].Outcome == "failed" {
			failed = &events[i]
		}
	}
	if failed == nil {
		t.Fatalf("no failed attempt recorded: %+v", events)
	}
	if failed.FailureCode != "publication_conflict/scheduled" || failed.FailureCause == "" {
		t.Fatalf("failed attempt missing conflict code/cause: %+v", failed)
	}

	found := false
	for _, record := range capture.records {
		if !strings.Contains(record.Message, "managed refresh attempt failed") {
			continue
		}
		var code, cause string
		record.Attrs(func(a slog.Attr) bool {
			if a.Key == "failure_code" {
				code = a.Value.String()
			}
			if a.Key == "cause" {
				cause = a.Value.String()
			}
			return true
		})
		if code == failed.FailureCode && cause == failed.FailureCause {
			found = true
		}
	}
	if !found {
		t.Fatal("worker log line did not carry the recorded failure code and cause")
	}

	deps.Config.Admin = config.Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32"}, Origin: "https://market.example", Issuer: "test", SubjectHeader: "X-Test-Subject", GroupsHeader: "X-Test-Groups", AdminGroups: []string{"operators"}}
	handler := api.NewServer(deps.Config, deps.MetadataStore, deps.BlobStore).PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/attempts?job_id="+string(j.ID), nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Origin", "https://market.example")
	request.Header.Set("X-Marketplace-Admin", "1")
	request.Header.Set("X-Test-Subject", "operator")
	request.Header.Set("X-Test-Groups", `["operators"]`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin attempts status/body = %d/%s", response.Code, response.Body.String())
	}
	var body struct {
		Attempts []store.ManagedRefreshAttempt `json:"attempts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	adminFound := false
	for _, attempt := range body.Attempts {
		if attempt.Outcome == "failed" && attempt.FailureCode == failed.FailureCode && attempt.FailureCause == failed.FailureCause {
			adminFound = true
		}
	}
	if !adminFound {
		t.Fatalf("admin surface did not expose the persisted failure: %s", response.Body.String())
	}
}

// spec: marketplace-publication/failed-publication-records-the-conflict-cause
func TestManagedFailureClassifiesPublicationConflict(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		op    string
		code  string
	}{
		{"conflict", fmt.Errorf("stage: %w", store.ErrConflict), "scheduled", "publication_conflict/scheduled"},
		{"conflict-manual", store.ErrConflict, "manual-reindex", "publication_conflict/manual-reindex"},
		{"fence", store.ErrPublicationFence, "scheduled", "publication_fence"},
		{"acquisition", fmt.Errorf("clone: %w", ErrManagedAcquire), "scheduled", "acquisition/scheduled"},
		{"superseded", ErrManagedSuperseded, "scheduled", "superseded"},
		{"internal", errors.New("boom"), "scheduled", "internal/scheduled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := managedFailure(tc.cause, tc.op)
			if failure.Code != tc.code || failure.Cause == "" {
				t.Fatalf("managedFailure = %+v, want code %q", failure, tc.code)
			}
			for _, bad := range []string{tc.cause.Error(), "\x00", "\n"} {
				if strings.Contains(failure.Cause, bad) && bad != "" {
					t.Fatalf("cause leaked raw error text: %q", failure.Cause)
				}
			}
		})
	}
}
