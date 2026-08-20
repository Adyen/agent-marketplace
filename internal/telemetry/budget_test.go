package telemetry

import "testing"

func TestValidateCardinalityBudget(t *testing.T) {
	tests := []struct {
		name   string
		ids    []string
		budget int
		wantOK bool
	}{
		{
			name:   "empty list passes",
			ids:    nil,
			budget: 10,
			wantOK: true,
		},
		{
			name:   "single id within default budget passes",
			ids:    []string{"abc123"},
			budget: 0,
			wantOK: true,
		},
		{
			name:   "unique count exceeds budget fails",
			ids:    []string{"a", "b", "c"},
			budget: 2,
			wantOK: false,
		},
		{
			name:   "duplicates are deduplicated",
			ids:    []string{"a", "a", "b"},
			budget: 2,
			wantOK: true,
		},
		{
			name:   "empty ids are ignored",
			ids:    []string{"", "x", ""},
			budget: 1,
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCardinalityBudget(tc.ids, tc.budget)
			if tc.wantOK && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("expected cardinality error, got nil")
			}
		})
	}
}

func TestTelemetryStateValidateCardinalityBudgetNoOp(t *testing.T) {
	state := TelemetryState{}
	if err := state.ValidateCardinalityBudget(); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
}
