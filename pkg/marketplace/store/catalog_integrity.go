package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func catalogReferenceContainsNUL(reference ResourceCoordinate) bool {
	return strings.IndexByte(string(reference.Marketplace), 0) >= 0 ||
		strings.IndexByte(string(reference.Registry), 0) >= 0 ||
		strings.IndexByte(reference.Name, 0) >= 0
}

func ReadCatalogProjectionIntegrity(rows *sql.Rows, textParametersRejectNUL bool) error {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var findings string
		var assessedAt int64
		var revisionAt, coordinateAt, registryAt sql.NullInt64
		var collectionID, collectionRegistry, collectionManifest, members, includes sql.NullString
		var pageRegistry, pageContentDigest string
		if err := rows.Scan(&findings, &assessedAt, &revisionAt, &coordinateAt, &registryAt, &collectionID, &collectionRegistry, &collectionManifest, &members, &includes, &pageRegistry, &pageContentDigest); err != nil {
			return fmt.Errorf("scan catalog integrity row: %w", err)
		}
		if findings != "" {
			var decoded []Finding
			if err := json.Unmarshal([]byte(findings), &decoded); err != nil {
				return fmt.Errorf("unmarshal assessment findings: %w", err)
			}
		}
		if collectionID.Valid {
			if !collectionRegistry.Valid || !collectionManifest.Valid || !members.Valid || !includes.Valid {
				return fmt.Errorf("catalog integrity: collection revision %q is missing members or includes", collectionID.String)
			}
			var memberRefs, includeRefs []ResourceCoordinate
			if err := json.Unmarshal([]byte(members.String), &memberRefs); err != nil {
				return fmt.Errorf("unmarshal collection members: %w", err)
			}
			if err := json.Unmarshal([]byte(includes.String), &includeRefs); err != nil {
				return fmt.Errorf("unmarshal collection includes: %w", err)
			}
			if textParametersRejectNUL && collectionRegistry.String == pageRegistry && collectionManifest.String == pageContentDigest {
				for _, reference := range memberRefs {
					if IsMarketplaceResourceKind(reference.Kind) && catalogReferenceContainsNUL(reference) {
						return fmt.Errorf("catalog integrity: recognized collection reference contains a NUL")
					}
				}
				for _, reference := range includeRefs {
					if IsMarketplaceResourceKind(reference.Kind) && catalogReferenceContainsNUL(reference) {
						return fmt.Errorf("catalog integrity: recognized collection reference contains a NUL")
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read catalog integrity: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close catalog integrity: %w", err)
	}
	return nil
}
