package main

import (
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func validateLegacyMarketplaceSeeds(entries []config.MarketplaceEntry, doc lockfile.Document) error {
	for _, entry := range entries {
		locked, ok := findLockedMarketplace(doc, entry)
		if !ok {
			return fmt.Errorf("legacy Marketplace endpoint %s has no lock record", entry.URL)
		}
		for _, pin := range entry.Resources {
			legacy := pin.Digest != "" || pin.SourceSlug != "" || pin.Acquisition != "" || pin.SourceCommit != "" || pin.SourceManifestDigest != "" || pin.Direct || pin.SelectedBy != ""
			if !legacy {
				continue
			}
			if pin.Kind == "collection" {
				found := false
				for _, collection := range locked.Collections {
					if collection.SourceID == pin.SourceID && collection.Name == pin.Name && collection.Revision.String() == pin.Digest {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("legacy collection %s/%s@%s conflicts with verified collection graph", pin.SourceID, pin.Name, pin.Digest)
				}
				continue
			}
			var record *lockfile.MarketplaceResource
			for i := range locked.Resources {
				candidate := &locked.Resources[i]
				if candidate.SourceID == pin.SourceID && candidate.Kind == pin.Kind && candidate.Name == pin.Name {
					if record != nil {
						return fmt.Errorf("legacy resource %s/%s/%s is ambiguous", pin.SourceID, pin.Kind, pin.Name)
					}
					record = candidate
				}
			}
			if record == nil || record.Revision.String() != pin.Digest {
				return fmt.Errorf("legacy resource %s/%s/%s@%s conflicts with verified lock", pin.SourceID, pin.Kind, pin.Name, pin.Digest)
			}
			if pin.Acquisition != "" && string(record.Acquisition) != pin.Acquisition {
				return fmt.Errorf("legacy resource %s/%s/%s acquisition conflicts with verified lock", pin.SourceID, pin.Kind, pin.Name)
			}
			if pin.SourceCommit != "" && record.SourceCommit.Hex() != strings.TrimPrefix(pin.SourceCommit, "sha1:") {
				return fmt.Errorf("legacy resource %s/%s/%s source commit conflicts with verified lock", pin.SourceID, pin.Kind, pin.Name)
			}
			if pin.SourceManifestDigest != "" && record.SourceManifestDigest.String() != pin.SourceManifestDigest {
				return fmt.Errorf("legacy resource %s/%s/%s manifest digest conflicts with verified lock", pin.SourceID, pin.Kind, pin.Name)
			}
			for _, selected := range pin.SelectedBy.List() {
				matched := false
				for _, edge := range record.SelectedBy {
					if edge.SourceID == selected.SourceID && edge.Name == selected.Name && edge.Revision.String() == selected.Revision {
						matched = true
						break
					}
				}
				if !matched {
					return fmt.Errorf("legacy resource %s/%s/%s collection selection conflicts with verified graph", pin.SourceID, pin.Kind, pin.Name)
				}
			}
		}
	}
	return nil
}
