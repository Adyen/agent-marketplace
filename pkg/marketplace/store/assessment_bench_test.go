package store_test

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkResourceAssessmentValidate(b *testing.B) {
	assessment := validResourceAssessment()
	assessment.Findings = make([]store.Finding, 4)
	for i := range assessment.Findings {
		assessment.Findings[i] = store.Finding{Code: "safe", Severity: "info", Policy: "allow", Message: "No unsafe behavior found.", Location: &store.FindingLocation{Path: "resource/skill.json", Line: i + 1}}
	}
	assessment, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := assessment.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
