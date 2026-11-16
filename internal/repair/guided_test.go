package repair

import (
	"context"
	"testing"
)

// spec: doctor-repair-planning/a-guided-layer-does-not-block-independent-steps
// spec: doctor-repair-planning/invalid-configuration-blocks-only-the-steps-that-need-it
func TestLoopRunsIndependentSafeStepAfterGuidedDecline(t *testing.T) {
	guidedRuns, independentRuns, selectionRuns := 0, 0, 0
	diagnoses := 0
	guided := Step{
		Key: "broken-repo-config", Stage: StageConfiguration, Class: Guided,
		Action: "edit repository config", Code: "v4.unknown-field",
		Apply: func(context.Context) (bool, error) {
			guidedRuns++
			return false, nil
		},
	}
	selection := Step{
		Key: "selection-repair", Stage: StageDrift, Class: Safe,
		Action: "sync selected resources", Code: "harness.drift",
		Inputs: []Input{InputSelection},
		Apply: func(context.Context) (bool, error) {
			selectionRuns++
			return true, nil
		},
	}
	independent := Step{
		Key: "valid-user-layer", Stage: StageLocks, Class: Safe,
		Action: "repair from user config", Code: "lock.stale",
		Inputs: []Input{Input("user-layer")},
		Apply: func(context.Context) (bool, error) {
			independentRuns++
			return true, nil
		},
	}
	loop := Loop{
		AllowGuided: true,
		Diagnose: func(context.Context) (Diagnosis, error) {
			diagnoses++
			if diagnoses > 1 && guidedRuns == 1 && independentRuns == 1 {
				return Diagnosis{}, nil
			}
			return Diagnosis{
				InvalidInputs: map[Input]Blocker{
					InputSelection: {Key: guided.Key, Reason: "repository configuration invalid"},
				},
				Steps: []Step{guided, selection, independent},
			}, nil
		},
		Confirm: func(steps []Step) bool {
			return len(steps) == 1 && steps[0].Key == independent.Key
		},
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("independent repair did not finish: %v", err)
	}
	if guidedRuns != 1 || independentRuns != 1 || selectionRuns != 0 {
		t.Fatalf("guided, independent, and selection-dependent runs = %d, %d, %d; want 1, 1, 0",
			guidedRuns, independentRuns, selectionRuns)
	}
}
