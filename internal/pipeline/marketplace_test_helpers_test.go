package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func testAssessedResourceRevision(t testing.TB, revision store.ResourceRevision, decision store.AssessmentDecision, findings []store.Finding, analyzerVersions ...string) store.AssessedResourceRevision {
	t.Helper()
	analyzerVersion := "pipeline-test"
	if len(analyzerVersions) > 0 {
		analyzerVersion = analyzerVersions[0]
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               revision.Coordinate,
		ContentDigest:            revision.ContentDigest,
		AnalyzerVersion:          analyzerVersion,
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 decision,
		Findings:                 findings,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize test resource assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate:     revision.Coordinate,
			ContentDigest:  revision.ContentDigest,
			Commit:         strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
}
