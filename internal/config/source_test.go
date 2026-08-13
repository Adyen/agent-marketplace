package config

import "testing"

func TestSourcePrecedenceOrdersLayersCorrectly(t *testing.T) {
	order := []Source{
		SourceUnset,
		Organization,
		UserPersonal,
		RepoCommitted,
		RepoPersonal,
	}
	for i := 0; i < len(order)-1; i++ {
		if order[i].Precedence() >= order[i+1].Precedence() {
			t.Fatalf("expected Precedence to be strictly increasing, but %v (%d) >= %v (%d)",
				order[i], order[i].Precedence(), order[i+1], order[i+1].Precedence())
		}
	}
}

func TestSourceIsMachineScope(t *testing.T) {
	tests := []struct {
		source Source
		want   bool
	}{
		{SourceUnset, false},
		{Organization, true},
		{UserPersonal, true},
		{RepoCommitted, false},
		{RepoPersonal, false},
	}

	for _, tc := range tests {
		if got := tc.source.IsMachineScope(); got != tc.want {
			t.Errorf("%v.IsMachineScope() = %v, want %v", tc.source, got, tc.want)
		}
	}
}

func TestSourceIsWritable(t *testing.T) {
	tests := []struct {
		source Source
		want   bool
	}{
		{SourceUnset, false},
		{Organization, false},
		{UserPersonal, true},
		{RepoCommitted, true},
		{RepoPersonal, true},
	}

	for _, tc := range tests {
		if got := tc.source.IsWritable(); got != tc.want {
			t.Errorf("%v.IsWritable() = %v, want %v", tc.source, got, tc.want)
		}
	}
}

func TestSourceString(t *testing.T) {
	tests := []struct {
		source Source
		want   string
	}{
		{SourceUnset, "-"},
		{Organization, "ORG"},
		{UserPersonal, "USER"},
		{RepoCommitted, "REPO"},
		{RepoPersonal, "REPO*"},
		{Source(99), "UNKNOWN"}, // out-of-range default branch
	}

	for _, tc := range tests {
		if got := tc.source.String(); got != tc.want {
			t.Errorf("Source(%d).String() = %q, want %q", tc.source, got, tc.want)
		}
	}
}
