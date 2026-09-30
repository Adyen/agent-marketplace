package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestRunUpdateCmdClassifiesPipelineAndReloadFailures(t *testing.T) {
	failure := errors.New("induced failure")
	for _, test := range []struct {
		name     string
		result   pipeline.SyncResult
		snapshot snapshotLoadedMsg
		stage    string
		wantErr  bool
	}{
		{
			name: "refresh",
			result: pipeline.SyncResult{
				CatalogTrustErrors: []error{failure},
				ResolveResult:      pipeline.ResolveResult{Errors: []error{failure}},
			},
			stage:   updateStageRefresh,
			wantErr: true,
		},
		{
			name: "materialization",
			result: pipeline.SyncResult{
				MaterializeErrors: []error{failure},
				ResolveResult:     pipeline.ResolveResult{Errors: []error{failure}},
			},
			stage:   updateStageMaterialization,
			wantErr: true,
		},
		{
			name: "state reload",
			snapshot: snapshotLoadedMsg{diagnostics: []tuiDiagnostic{
				snapshotDiagnostic(diagnosticError, diagnosticConfig, failure),
			}},
			stage:   updateStageReload,
			wantErr: true,
		},
		{
			name:     "success",
			snapshot: snapshotLoadedMsg{driftAssessment: driftReady},
			stage:    updateStageMaterialization,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousSync := runPipelineSync
			previousSnapshot := readSnapshotForUpdate
			runPipelineSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
				return test.result
			}
			readSnapshotForUpdate = func(context.Context, pipeline.Workspace, string, catalogLoadMode) snapshotLoadedMsg {
				return test.snapshot
			}
			t.Cleanup(func() {
				runPipelineSync = previousSync
				readSnapshotForUpdate = previousSnapshot
			})

			msg := runUpdateCmdForWorkspace(context.Background(), defaultTUIWorkspace("/repo"), "/home", 7)()
			done, ok := msg.(updateDoneMsg)
			if !ok {
				t.Fatalf("message type = %T, want updateDoneMsg", msg)
			}
			if done.stage != test.stage {
				t.Fatalf("stage = %q, want %q", done.stage, test.stage)
			}
			if (done.err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %v", done.err, test.wantErr)
			}
			if done.snapshot.generation != 7 {
				t.Fatalf("snapshot generation = %d, want 7", done.snapshot.generation)
			}
		})
	}
}

func TestUpdateDoneAppliesAuthoritativeSnapshotBeforeReportingFailure(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.syncing = true
	generation := m.requests.active(requestSnapshot)
	failure := errors.New("materialization failed")

	updated, _ := m.Update(updateDoneMsg{
		generation: generation,
		stage:      updateStageMaterialization,
		err:        failure,
		snapshot: snapshotLoadedMsg{
			generation:      generation,
			driftAssessment: driftFailed,
			rows:            []row{mkRow("post-update", withProject)},
		},
	})
	m = updated.(Model)
	if len(m.rows) != 1 || m.rows[0].Artifact.Name != "post-update" {
		t.Fatalf("authoritative post-update rows were not applied: %+v", m.rows)
	}
	if len(m.errs) != 1 || !errors.Is(m.errs[0], failure) {
		t.Fatalf("materialization failure not retained: %v", m.errs)
	}
	if m.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed", m.driftAssessment)
	}
}
