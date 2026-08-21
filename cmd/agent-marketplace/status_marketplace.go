package main

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

func marketplaceStatusHeaders() []string {
	return []string{
		"ALIAS", "URL", "INSTANCE_ID", "SOURCE_ID", "SOURCE_SLUG", "KIND", "NAME", "INSTALLED_NAME",
		"REVISION", "CONTENT_DIGEST", "ACQUISITION", "LAYER", "REQUIRED", "STATE",
		"CACHE", "ARCHIVE_DIGEST", "UPDATE_AVAILABLE", "DETAIL",
	}
}

func marketplaceStatusRowsToCells(rows []marketplace.PinStatus) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{
			quoteMarketplaceStatus(row.Alias),
			quoteMarketplaceStatus(row.URL),
			quoteMarketplaceStatus(row.InstanceID),
			quoteMarketplaceStatus(row.SourceID),
			quoteMarketplaceStatus(row.SourceSlug),
			quoteMarketplaceStatus(row.Kind),
			quoteMarketplaceStatus(row.Name),
			quoteMarketplaceStatus(row.InstalledName),
			quoteMarketplaceStatus(row.Revision),
			quoteMarketplaceStatus(row.ContentDigest),
			quoteMarketplaceStatus(string(row.Acquisition)),
			quoteMarketplaceStatus(row.Source.String()),
			fmt.Sprintf("%t", row.Required),
			quoteMarketplaceStatus(string(row.State)),
			quoteMarketplaceStatus(string(row.Cache)),
			quoteMarketplaceStatus(row.ArchiveDigest),
			fmt.Sprintf("%t", row.UpdateAvailable),
			quoteMarketplaceStatus(row.Detail),
		})
	}
	return cells
}

func quoteMarketplaceStatus(value string) string {
	if value == "" {
		return "-"
	}
	return marketplace.QuoteTerminal(value)
}
