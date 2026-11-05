package bundle

import "testing"

var toolListBenchmarkResult []string

// BenchmarkToToolList measures reading a subagent tool list, which ingestion
// does for every subagent on each sync: the comma-separated string, a string
// whose scoped entry holds commas inside parentheses, a string that is not
// well formed and keeps its single entry, and the YAML list.
func BenchmarkToToolList(b *testing.B) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"string", "Read, Grep, Glob, Bash, Edit"},
		{"scoped", "Agent(worker, researcher), Read, Grep"},
		{"malformed", "Read, Grep, Bash(git diff"},
		{"list", []any{"Read", "Grep", "Glob", "Bash", "Edit"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if toolListBenchmarkResult, err = toToolList(tc.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
