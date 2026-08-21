package main

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func TestMarketplaceStatusRowsQuoteControlledValues(t *testing.T) {
	rows := marketplaceStatusRowsToCells([]marketplace.PinStatus{{
		Alias: "alias\t\x1b[31m", URL: "https://example.test/\r\n", InstanceID: "instance",
		SourceID: "source", SourceSlug: "slug", Kind: "skill", Name: "name",
		Revision: "refs/heads/main", ContentDigest: "sha256:abc", Acquisition: marketplace.AcquisitionMirror,
		Source: config.RepoCommitted, State: marketplace.PinStateUnavailable, Cache: marketplace.CacheStateMissing,
		Detail: "controlled\u202evalue",
	}})
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want one", len(rows))
	}
	output := strings.Join(rows[0], "\t")
	if strings.ContainsAny(output, "\x00\x1b\r\n") {
		t.Fatalf("Marketplace status cells contain raw terminal controls: %q", output)
	}
	if !strings.Contains(output, `"alias\t\x1b[31m"`) {
		t.Fatalf("alias was not terminal quoted: %q", output)
	}
	if !strings.Contains(output, `"https://example.test/\r\n"`) {
		t.Fatalf("URL was not terminal quoted: %q", output)
	}
	if !strings.Contains(output, "false") {
		t.Fatalf("status must explicitly report update availability false: %q", output)
	}
}
