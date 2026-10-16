package compile

import "testing"

func TestSidecarToolsWithinCanonical(t *testing.T) {
	tests := []struct {
		name      string
		source    Target
		canonical []string
		sidecar   []string
		target    Target
		overrides map[string]string
		want      bool
	}{
		{"no sidecar", "", []string{"Read"}, nil, TargetClaude, nil, true},
		{"unrestricted canonical", "", nil, []string{"Bash"}, TargetClaude, nil, true},
		{"restated", TargetClaude, []string{"Read", "Grep"}, []string{"Grep", "Read"}, TargetClaude, nil, true},
		{"narrowed", TargetClaude, []string{"Read", "Grep"}, []string{"Read"}, TargetClaude, nil, true},
		{"widened", TargetClaude, []string{"Read"}, []string{"Read", "Bash"}, TargetClaude, nil, false},
		{"explicit empty admits only empty", TargetClaude, []string{}, []string{"Read"}, TargetClaude, nil, false},
		{"explicit empty restated", TargetClaude, []string{}, []string{}, TargetClaude, nil, true},
		{"translated target tool", "", []string{"Write"}, []string{"Create"}, TargetFactory, nil, true},
		{"translation never widens", "", []string{"Write"}, []string{"Edit"}, TargetFactory, nil, false},
		{"verbatim entry on a target without a tool surface", "", []string{"Read"}, []string{"Read"}, TargetCursor, nil, true},
		{"verbatim native entry", TargetCursor, []string{"native_reader"}, []string{"native_reader"}, TargetCursor, nil, true},
		{"caller override", "", []string{"CustomSource"}, []string{"Read"}, TargetFactory, map[string]string{"CustomSource": "Read"}, true},
		{"unmapped native entry", "", []string{"Read"}, []string{"native_writer"}, TargetCursor, nil, false},
		{"tool under a wildcard", TargetClaude, []string{"mcp__github__*"}, []string{"mcp__github__list_issues"}, TargetClaude, nil, true},
		{"narrower wildcard", TargetClaude, []string{"mcp__github__*"}, []string{"mcp__github__list_*"}, TargetClaude, nil, true},
		{"other server outside a wildcard", TargetClaude, []string{"mcp__github__*"}, []string{"mcp__gitlab__list_issues"}, TargetClaude, nil, false},
		{"wider wildcard", TargetClaude, []string{"mcp__github__list_*"}, []string{"mcp__github__*"}, TargetClaude, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SidecarToolsWithinCanonical(tt.source, tt.canonical, tt.sidecar, tt.target, tt.overrides); got != tt.want {
				t.Fatalf("SidecarToolsWithinCanonical(%q, %v, %v, %s) = %t, want %t", tt.source, tt.canonical, tt.sidecar, tt.target, got, tt.want)
			}
		})
	}
}
