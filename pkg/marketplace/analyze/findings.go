package analyze

import (
	"fmt"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// NewFinding builds a Finding with source range and analyzer metadata.
func NewFinding(code, message, analyzer string, sr *SourceRange) Finding {
	return Finding{
		Code:            code,
		Message:         message,
		Analyzer:        analyzer,
		AnalyzerVersion: AnalyzerVersionV1,
		SourceRange:     sr,
	}
}

// WithSourceRange returns a copy of f with the supplied source range.
func (f Finding) WithSourceRange(sr *SourceRange) Finding {
	f.SourceRange = sr
	return f
}

// SourceRangeAt returns a source range for a byte offset inside content.
func SourceRangeAt(file, content string, offset int) *SourceRange {
	cursor := sourceRangeCursor{content: content}
	return cursor.at(file, offset)
}

// lineColumn returns 1-based line and column (in runes) for a byte offset.
func lineColumn(content string, offset int) (line, column int) {
	cursor := sourceRangeCursor{content: content}
	location := cursor.at("", offset)
	return location.Line, location.Column
}

// ToCanonicalFinding converts an analyze.Finding into a
// harness-compilation CanonicalFinding.
func ToCanonicalFinding(f Finding) hccartifact.CanonicalFinding {
	cf := hccartifact.CanonicalFinding{
		Code:            f.Code,
		Severity:        f.Severity,
		Message:         f.Message,
		Analyzer:        f.Analyzer,
		AnalyzerVersion: f.AnalyzerVersion,
		PolicyOutcome:   f.PolicyOutcome,
	}
	if f.SourceRange != nil {
		cf.File = f.SourceRange.File
		cf.Line = f.SourceRange.Line
		cf.Column = f.SourceRange.Column
	}
	return cf
}

// truncate returns a short, safely printable preview of s.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// describeBytes returns a human-readable size description.
func describeBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}
