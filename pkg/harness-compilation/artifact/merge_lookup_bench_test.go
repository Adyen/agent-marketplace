package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkMergeJSONCObjectLookupsCPU includes parsing, merge decisions,
// concrete-syntax patching and rendering, but no filesystem or network I/O.
func BenchmarkMergeJSONCObjectLookupsCPU(b *testing.B) {
	for _, count := range []int{1, 32, 256, 1024, 4096} {
		for _, changed := range []bool{false, true} {
			b.Run(fmt.Sprintf("members-%d/changed-%t", count, changed), func(b *testing.B) {
				var existing strings.Builder
				existing.WriteString("{\n  // retained header\n  \"mcpServers\": {\n")
				desired := make(map[string]any, count)
				for i := range count {
					name := fmt.Sprintf("server-%04d", i)
					if i > 0 {
						existing.WriteString(",\n")
					}
					fmt.Fprintf(&existing, "    // retained %s\n    %q: {\"command\": \"old\", \"_managed_by\": \"agent-marketplace\"}", name, name)
					command := "old"
					if changed && i%2 == 0 {
						command = "new"
					}
					desired[name] = map[string]any{"command": command}
				}
				existing.WriteString("\n  },\n  \"handwritten\": 1.00\n}\n")
				before := []byte(existing.String())
				want, err := json.Marshal(map[string]any{"mcpServers": desired})
				if err != nil {
					b.Fatal(err)
				}
				out, report, err := MergeWithExistingJSON(before, want, markerField, markerValue)
				added := 0
				if changed {
					added = (count + 1) / 2
				}
				if err != nil || len(report.Added) != added || len(report.Unchanged) != count-added || len(report.Removed) != 0 {
					b.Fatalf("fixture: report=%+v, err=%v", report, err)
				}
				if !changed && !bytes.Equal(out, before) {
					b.Fatal("unchanged fixture lost concrete syntax")
				}
				again, _, err := MergeWithExistingJSON(out, want, markerField, markerValue)
				if err != nil || !bytes.Equal(out, again) {
					b.Fatalf("fixture is not idempotent: %v", err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, _, err := MergeWithExistingJSON(before, want, markerField, markerValue); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
