package repair

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// spec: doctor-repair-planning/invalid-configuration-blocks-only-the-steps-that-need-it
// spec: doctor-repair-planning/failed-resolution-blocks-selection-dependent-steps
// spec: doctor-repair-planning/shared-aggregate-repair-runs-after-drift-repair
func TestPlanOrdersStepsAndBlocksOnlyInvalidInputs(t *testing.T) {
	independentRuns := 0
	diagnosis := Diagnosis{
		InvalidInputs: map[Input]Blocker{
			InputSelection: {Code: "resolve.failed", Reason: "selection unresolved"},
		},
		Steps: []Step{
			{Stage: StageOwnership, Class: Safe, Action: "reconcile ownership", Code: "ownership.repair", Inputs: []Input{InputSelection}},
			{Stage: StageDrift, Class: Safe, Action: "sync drift", Code: "harness.drift", Inputs: []Input{InputSelection}},
			{Stage: StageJournal, Class: Safe, Action: "recover journal", Code: "dependency.recovery-required", Apply: func(context.Context) (bool, error) {
				independentRuns++
				return true, nil
			}},
		},
	}

	plan := Plan(diagnosis)
	if len(plan) != 3 {
		t.Fatalf("Plan() returned %d steps, want 3: %#v", len(plan), plan)
	}
	if plan[0].Stage != StageJournal || plan[1].Stage != StageDrift || plan[2].Stage != StageOwnership {
		t.Fatalf("plan stage order = %d, %d, %d", plan[0].Stage, plan[1].Stage, plan[2].Stage)
	}
	if plan[0].BlockedBy != nil {
		t.Fatalf("independent step blocked: %#v", plan[0])
	}
	for _, step := range plan[1:] {
		if step.BlockedBy == nil || step.BlockedBy.Code != "resolve.failed" || step.BlockedBy.Reason != "selection unresolved" {
			t.Fatalf("%s blocker = %#v", step.Action, step.BlockedBy)
		}
	}
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			if independentRuns > 0 {
				return Diagnosis{}, nil
			}
			return diagnosis, nil
		},
		Confirm: func(steps []Step) bool { return len(steps) == 1 && steps[0].Code == "dependency.recovery-required" },
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("independent step did not finish: %v", err)
	}
	if independentRuns != 1 {
		t.Fatalf("valid-input step ran %d times, want once", independentRuns)
	}
}

// spec: doctor-repair-planning/steps-added-by-re-diagnosis-are-confirmed-before-they-run
// spec: doctor-repair-planning/shared-aggregate-repair-runs-after-drift-repair
func TestRunRediagnosesAndConfirmsNewSteps(t *testing.T) {
	first := Step{Stage: StageDrift, Class: Safe, Action: "sync drift", Code: "harness.drift"}
	second := Step{Stage: StageOwnership, Class: Safe, Action: "adopt matches", Code: "ownership.adopt"}
	var ran []string
	var confirmations [][]string
	var displayed int
	diagnoses := 0
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			diagnoses++
			switch diagnoses {
			case 1:
				first.Apply = func(context.Context) (bool, error) {
					ran = append(ran, first.Action)
					return true, nil
				}
				return Diagnosis{Steps: []Step{first}}, nil
			case 2:
				second.Apply = func(context.Context) (bool, error) {
					ran = append(ran, second.Action)
					return true, nil
				}
				return Diagnosis{Steps: []Step{second}}, nil
			default:
				return Diagnosis{}, nil
			}
		},
		Confirm: func(steps []Step) bool {
			actions := make([]string, len(steps))
			for i, step := range steps {
				actions[i] = step.Action
			}
			confirmations = append(confirmations, actions)
			return true
		},
		Display: func([]Step) error {
			displayed++
			return nil
		},
	}

	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("Loop.Run() error = %v", err)
	}
	if strings.Join(ran, ",") != "sync drift,adopt matches" {
		t.Fatalf("ran steps = %v", ran)
	}
	if len(confirmations) != 2 || strings.Join(confirmations[0], ",") != "sync drift" || strings.Join(confirmations[1], ",") != "adopt matches" {
		t.Fatalf("confirmations = %v", confirmations)
	}
	if displayed != 3 {
		t.Fatalf("plans displayed = %d, want each diagnosis", displayed)
	}
}

// spec: doctor-repair-planning/a-recurring-step-stops-repair-at-the-iteration-cap
func TestRunStopsWhenStepRecursFiveTimes(t *testing.T) {
	runs := 0
	step := Step{Stage: StageDrift, Class: Safe, Action: "sync drift", Code: "harness.drift"}
	step.Apply = func(context.Context) (bool, error) {
		runs++
		return true, nil
	}
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			return Diagnosis{Steps: []Step{step}}, nil
		},
		Confirm: func([]Step) bool { return true },
	}

	err := loop.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sync drift") {
		t.Fatalf("Loop.Run() error = %v, want recurring step name", err)
	}
	if runs != 5 {
		t.Fatalf("step ran %d times, want 5", runs)
	}
}

// spec: doctor-repair-planning/repair-exits-zero-when-the-setup-ends-clean
func TestRunExitsCleanWhenDiagnosisHasNoSteps(t *testing.T) {
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) { return Diagnosis{}, nil },
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("clean Loop.Run() error = %v", err)
	}
}

// spec: doctor-repair-planning/repair-exits-one-when-a-step-fails
func TestRunReturnsStepFailure(t *testing.T) {
	want := errors.New("write failed")
	step := Step{Stage: StageDrift, Class: Safe, Action: "sync drift", Code: "harness.drift", Apply: func(context.Context) (bool, error) {
		return false, want
	}}
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			return Diagnosis{Steps: []Step{step}}, nil
		},
		Confirm: func([]Step) bool { return true },
	}
	if err := loop.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Loop.Run() error = %v, want %v", err, want)
	}
}

// spec: doctor-repair-planning/declining-the-repair-plan-changes-nothing
func TestRunDeclineDoesNotApplySteps(t *testing.T) {
	runs := 0
	step := Step{Stage: StageDrift, Class: Safe, Action: "sync drift", Code: "harness.drift", Apply: func(context.Context) (bool, error) {
		runs++
		return true, nil
	}}
	loop := Loop{
		Diagnose: func(context.Context) (Diagnosis, error) {
			return Diagnosis{Steps: []Step{step}}, nil
		},
		Confirm: func([]Step) bool { return false },
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("declined Loop.Run() error = %v", err)
	}
	if runs != 0 {
		t.Fatalf("declined plan ran %d steps", runs)
	}
}
