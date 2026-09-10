package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func testAssessedMarketplaceRevision(t testing.TB, revision store.ResourceRevision) store.AssessedResourceRevision {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               revision.Coordinate,
		ContentDigest:            revision.ContentDigest,
		AnalyzerVersion:          "agent-marketplace-test/v1",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize Marketplace test assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate:     revision.Coordinate,
			ContentDigest:  revision.ContentDigest,
			Commit:         strings.Repeat("e", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
		},
	}
}
