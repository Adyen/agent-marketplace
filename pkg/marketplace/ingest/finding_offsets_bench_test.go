package ingest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

// BenchmarkStoreFindingOffsets isolates assessment-location conversion; it
// excludes scanning, Git, filesystem I/O and publication.
func BenchmarkStoreFindingOffsets(b *testing.B) {
	for _, layout := range []string{"lines", "columns"} {
		for _, count := range []int{128, 512, 2048} {
			b.Run(fmt.Sprintf("%s/%d", layout, count), func(b *testing.B) {
				sample := "\u00e9<!-- inert note -->"
				if layout == "lines" {
					sample += "\n"
				}
				body := []byte(strings.Repeat(sample, count))
				findings := make([]analyze.Finding, count)
				for i := range findings {
					line, column := i+1, 2
					if layout == "columns" {
						line, column = 1, 2+i*len([]rune(sample))
					}
					findings[i] = analyze.Finding{
						Code:        analyze.FindingHTMLComment,
						SourceRange: &analyze.SourceRange{File: "fixture/body", Line: line, Column: column},
					}
				}
				b.SetBytes(int64(len(body)))
				b.ReportAllocs()
				for b.Loop() {
					stored := toStoreFindings(findings, "SKILL.md", body, nil)
					if len(stored) != count || body[stored[count-1].Location.StartByte] != '<' {
						b.Fatal("lost finding or byte offset")
					}
				}
			})
		}
	}
}
