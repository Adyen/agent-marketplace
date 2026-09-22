package api

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

type artifactCoverageFixture struct{ fail bool }

func (f artifactCoverageFixture) ArtifactCoverage(ctx context.Context, _ store.MarketplaceID) (store.ArtifactCoverage, error) {
	if _, ok := ctx.Deadline(); !ok {
		return store.ArtifactCoverage{}, errors.New("missing deadline")
	}
	if f.fail {
		return store.ArtifactCoverage{}, errors.New("unavailable")
	}
	return store.ArtifactCoverage{LegacyRevisions: 7, V2Revisions: 4}, nil
}
func TestArtifactCoverageMetrics(t *testing.T) {
	collect := func(fail bool) string {
		t.Helper()
		registry := prometheus.NewRegistry()
		registry.MustRegister(newArtifactCoverageCollector(artifactCoverageFixture{fail: fail}, "mp"))
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		for _, family := range families {
			if _, err := expfmt.MetricFamilyToText(&output, family); err != nil {
				t.Fatal(err)
			}
		}
		return output.String()
	}
	expected := `# HELP agent_marketplace_artifact_coverage_available Whether artifact coverage was read successfully.
# TYPE agent_marketplace_artifact_coverage_available gauge
agent_marketplace_artifact_coverage_available 1
# HELP agent_marketplace_artifact_revisions Mirrored resource revisions with each artifact schema.
# TYPE agent_marketplace_artifact_revisions gauge
agent_marketplace_artifact_revisions{schema="v1"} 7
agent_marketplace_artifact_revisions{schema="v2"} 4
`
	if got := collect(false); got != expected {
		t.Fatalf("coverage metrics: %s", got)
	}
	unavailable := `# HELP agent_marketplace_artifact_coverage_available Whether artifact coverage was read successfully.
# TYPE agent_marketplace_artifact_coverage_available gauge
agent_marketplace_artifact_coverage_available 0
`
	if got := collect(true); got != unavailable {
		t.Fatalf("unavailable metrics: %s", got)
	}
}
