package api

import (
	"context"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/prometheus/client_golang/prometheus"
)

type artifactCoverageCollector struct {
	source      store.ArtifactCoverageStore
	marketplace store.MarketplaceID
	revisions   *prometheus.Desc
	available   *prometheus.Desc
}

func newArtifactCoverageCollector(source store.ArtifactCoverageStore, marketplace store.MarketplaceID) *artifactCoverageCollector {
	return &artifactCoverageCollector{source: source, marketplace: marketplace, revisions: prometheus.NewDesc("agent_marketplace_artifact_revisions", "Mirrored resource revisions with each artifact schema.", []string{"schema"}, nil), available: prometheus.NewDesc("agent_marketplace_artifact_coverage_available", "Whether artifact coverage was read successfully.", nil, nil)}
}
func (c *artifactCoverageCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.revisions
	ch <- c.available
}
func (c *artifactCoverageCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	coverage, err := c.source.ArtifactCoverage(ctx, c.marketplace)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.revisions, prometheus.GaugeValue, float64(coverage.LegacyRevisions), "v1")
	ch <- prometheus.MustNewConstMetric(c.revisions, prometheus.GaugeValue, float64(coverage.V2Revisions), "v2")
}
