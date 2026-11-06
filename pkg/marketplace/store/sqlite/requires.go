package sqlite

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// ListCurrentRequirementCandidates runs the required_by candidate query: the
// current-head skill, command, and subagent revisions of one Marketplace that
// are in the requested registry or a listed registry.
func (s *Store) ListCurrentRequirementCandidates(ctx context.Context, marketplace store.MarketplaceID, registry store.RegistryID) ([]store.RequirementCandidate, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT o.registry_id, r.slug, o.kind, o.name, o.content_digest, rr.requires
FROM current_resource_occurrences o
JOIN resource_revisions rr
  ON rr.marketplace_id = o.marketplace_id AND rr.registry_id = o.registry_id
 AND rr.kind = o.kind AND rr.name = o.name AND rr.content_digest = o.content_digest
JOIN registries r ON r.id = o.registry_id
WHERE o.marketplace_id = ?
  AND (o.registry_id = ? OR r.listed = ?)
  AND o.kind IN ('skill', 'command', 'subagent')
ORDER BY r.slug, o.kind, o.name`, marketplace, registry, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.RequirementCandidate, 0)
	for rows.Next() {
		var candidate store.RequirementCandidate
		var requires sql.NullString
		if err := rows.Scan(&candidate.Registry, &candidate.RegistrySlug, &candidate.Kind, &candidate.Name, &candidate.ContentDigest, &requires); err != nil {
			return nil, err
		}
		candidate.Requires = requires.String
		out = append(out, candidate)
	}
	return out, rows.Err()
}

// GetCurrentOccurrenceRoots returns the current-head occurrence of a resource
// with the roots of its registry occurrence. Zero rows are ErrNotFound; more
// than one row is ErrAmbiguousOccurrence, which callers treat as unresolved.
func (s *Store) GetCurrentOccurrenceRoots(ctx context.Context, coord store.ResourceCoordinate) (store.CurrentOccurrenceRoots, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT o.content_digest, o.source_path, ro.roots
FROM current_resource_occurrences o
JOIN all_registry_occurrences ro
  ON ro.registry_id = o.registry_id AND ro.occurrence_key = o.occurrence_key
WHERE o.marketplace_id = ? AND o.registry_id = ? AND o.kind = ? AND o.name = ?`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name)
	if err != nil {
		return store.CurrentOccurrenceRoots{}, err
	}
	defer func() { _ = rows.Close() }()
	var out store.CurrentOccurrenceRoots
	found := false
	for rows.Next() {
		if found {
			slog.DebugContext(ctx, "ambiguous current occurrence", "marketplace", coord.Marketplace, "registry", coord.Registry, "kind", coord.Kind, "name", coord.Name)
			return store.CurrentOccurrenceRoots{}, store.ErrAmbiguousOccurrence
		}
		var sourcePath sql.NullString
		var rootsJSON string
		if err := rows.Scan(&out.ContentDigest, &sourcePath, &rootsJSON); err != nil {
			return store.CurrentOccurrenceRoots{}, err
		}
		out.SourcePath = sourcePath.String
		if err := unmarshalJSON([]byte(rootsJSON), &out.Roots); err != nil {
			return store.CurrentOccurrenceRoots{}, err
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return store.CurrentOccurrenceRoots{}, err
	}
	if !found {
		return store.CurrentOccurrenceRoots{}, store.ErrNotFound
	}
	return out, nil
}
