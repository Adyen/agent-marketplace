package compile

import "testing"

func TestSidecarToolsWithinCanonical(t *testing.T) {
	tests := []struct {
		name      string
		canonical []string
		sidecar   []string
		target    Target
		overrides map[string]string
		want      bool
	}{
		{"no sidecar", []string{"Read"}, nil, TargetClaude, nil, true},
		{"unrestricted canonical", nil, []string{"Bash"}, TargetClaude, nil, true},
		{"restated", []string{"Read", "Grep"}, []string{"Grep", "Read"}, TargetClaude, nil, true},
		{"narrowed", []string{"Read", "Grep"}, []string{"Read"}, TargetClaude, nil, true},
		{"widened", []string{"Read"}, []string{"Read", "Bash"}, TargetClaude, nil, false},
		{"explicit empty admits only empty", []string{}, []string{"Read"}, TargetClaude, nil, false},
		{"explicit empty restated", []string{}, []string{}, TargetClaude, nil, true},
		{"target mapping", []string{"Write"}, []string{"Create"}, TargetFactory, nil, true},
		{"reverse mapping", []string{"Read"}, []string{"Read"}, TargetCursor, nil, true},
		{"verbatim native entry", []string{"native_reader"}, []string{"native_reader"}, TargetCursor, nil, true},
		{"caller override", []string{"CustomSource"}, []string{"Read"}, TargetCursor, map[string]string{"CustomSource": "Read"}, true},
		{"unmapped native entry", []string{"Read"}, []string{"native_writer"}, TargetCursor, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SidecarToolsWithinCanonical(tt.canonical, tt.sidecar, tt.target, tt.overrides); got != tt.want {
				t.Fatalf("SidecarToolsWithinCanonical(%v, %v, %s) = %t, want %t", tt.canonical, tt.sidecar, tt.target, got, tt.want)
			}
		})
	}
}
