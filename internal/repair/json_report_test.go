package repair

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// spec: doctor-repair-planning/json-repair-with-yes-runs-only-safe-steps
func TestJSONReportDefersGuidedStepsAndResolvesBlockerIDs(t *testing.T) {
	report := NewJSONReport()
	steps := []Step{
		{Key: "cause", Stage: StageConfiguration, Class: HumanOnly, Code: "config.invalid", Action: "review config"},
		{
			Key: "blocked", Stage: StageDrift, Class: Safe, Code: "ownership.drift", Action: "sync drift",
			BlockedBy: &Blocker{Key: "cause", Reason: "unserialized blocker reason"},
		},
		{Key: "guided", Stage: StageLocks, Class: Guided, Code: "lock.conflicted", Action: "resolve conflict"},
	}
	if err := report.Display(steps); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "unserialized blocker reason") {
		t.Fatalf("report serialized blocker reason: %s", data)
	}
	if len(report.Steps) != 3 || report.Steps[0].ID != 1 ||
		report.Steps[0].Status != "blocked" || len(report.Steps[0].Files) != 0 ||
		report.Steps[1].BlockedBy == nil || *report.Steps[1].BlockedBy != report.Steps[0].ID ||
		report.Steps[1].Status != "blocked" || report.Steps[1].Files == nil ||
		report.Steps[2].Status != "deferred" {
		t.Fatalf("projected steps = %+v", report.Steps)
	}
	if len(report.Results) != 0 {
		t.Fatalf("unexecuted steps produced results: %+v", report.Results)
	}
}

// spec: doctor-repair-planning/json-repair-preserves-text-repair-exit-status
func TestLoopJSONReportRetainsCompletedStepWhenLaterStepFails(t *testing.T) {
	report := NewJSONReport()
	failure := errors.New("later step failed")
	firstRuns, laterRuns, diagnoses := 0, 0, 0
	first := Step{
		Key: "first", Stage: StageConfiguration, Class: Safe,
		Code: "config.repair", Action: "repair config", Files: []string{"config.yaml"},
		Apply: func(context.Context) (bool, error) {
			firstRuns++
			return true, nil
		},
	}
	later := Step{
		Key: "later", Stage: StageLocks, Class: Safe,
		Code: "lock.refresh", Action: "refresh lock", Files: []string{"lock.yaml"},
		Apply: func(context.Context) (bool, error) {
			laterRuns++
			return false, failure
		},
	}
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			diagnoses++
			if diagnoses == 1 {
				return Diagnosis{Steps: []Step{first}}, nil
			}
			diagnosedFirst := first
			diagnosedFirst.Class = HumanOnly
			diagnosedFirst.Apply = nil
			return Diagnosis{Steps: []Step{diagnosedFirst, later}}, nil
		},
		Confirm: func([]Step) bool { return true },
		Display: report.Display,
		Result:  report.RecordResult,
	}
	if err := loop.Run(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("loop error = %v, want later-step failure", err)
	}
	if firstRuns != 1 || laterRuns != 1 || diagnoses != 2 {
		t.Fatalf("first runs=%d later runs=%d diagnoses=%d", firstRuns, laterRuns, diagnoses)
	}
	if len(report.Steps) != 2 ||
		report.Steps[0].ID != 1 || report.Steps[0].Status != "completed" ||
		report.Steps[1].ID != 2 || report.Steps[1].Status != "failed" {
		t.Fatalf("retained plan steps = %+v", report.Steps)
	}
	if len(report.Results) != 2 ||
		report.Results[0] != (JSONStepResult{ID: 1, Status: "completed"}) ||
		report.Results[1] != (JSONStepResult{ID: 2, Status: "failed"}) {
		t.Fatalf("step results = %+v", report.Results)
	}
}

// spec: doctor-repair-planning/json-repair-preserves-text-repair-exit-status
func TestLoopJSONReportRecordsDeferredAndValidationFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		validation   error
		applied      bool
		wantStatus   string
		wantApplyRun int
	}{
		{name: "apply declined", wantStatus: "deferred", wantApplyRun: 1},
		{name: "validation failed", validation: errors.New("policy changed"), applied: true, wantStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := NewJSONReport()
			applyRuns := 0
			step := Step{
				Key: tc.name, Stage: StageLocks, Class: Safe,
				Code: "lock.refresh", Action: "refresh lock",
				Apply: func(context.Context) (bool, error) {
					applyRuns++
					return tc.applied, nil
				},
			}
			loop := Loop{
				Diagnose: func(context.Context) (Diagnosis, error) {
					return Diagnosis{Steps: []Step{step}}, nil
				},
				Confirm:  func([]Step) bool { return true },
				Validate: func() error { return tc.validation },
				Display:  report.Display,
				Result:   report.RecordResult,
			}
			if err := loop.Run(context.Background()); !errors.Is(err, tc.validation) {
				t.Fatalf("loop error = %v, want %v", err, tc.validation)
			}
			if applyRuns != tc.wantApplyRun || len(report.Results) != 1 ||
				report.Results[0].Status != tc.wantStatus || report.Steps[0].Status != tc.wantStatus {
				t.Fatalf("validation failure report = %+v / %+v; apply runs %d",
					report.Steps, report.Results, applyRuns)
			}
		})
	}
}
