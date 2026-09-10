package artifact

import "testing"

func TestToCanonicalFindingsEmpty(t *testing.T) {
	got := ToCanonicalFindings(nil)
	if got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
}
