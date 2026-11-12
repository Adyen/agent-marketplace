package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ClaudeDeliveryStatePublished ClaudeDeliveryState = "published"
	ClaudeDeliveryStateRetired   ClaudeDeliveryState = "retired"
	ClaudeDeliveryStateSkipped   ClaudeDeliveryState = "skipped"
	ClaudeDeliveryStateConflict  ClaudeDeliveryState = "conflict"
	ClaudeDeliveryStateFailed    ClaudeDeliveryState = "failed"

	ClaudeDeliveryFailureMessageLimit = 200
)

type ClaudeDeliveryState string

type ClaudeDeliveryPlugin struct {
	Coordinate             ResourceCoordinate
	PluginName             string
	PluginID               string
	TargetMarketplaceID    string
	PublishedArchiveDigest ArchiveDigest
	PublishedContentDigest RevisionDigest
	PublishedVersionID     string
	LatestVersionID        string
	ServedVersionID        string
	ContentScanStatus      string
	Reach                  string
	InstallationPreference string
	State                  ClaudeDeliveryState
	LastErrorCode          string
	LastErrorMessage       string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ClaudeDeliveryDesired struct {
	Resource      AssessedResourceRevision
	PublicationID PublicationID
}

type ClaudeDeliveryStateStore interface {
	UpsertClaudeDeliveryPublished(context.Context, ClaudeDeliveryPlugin) error
	MarkClaudeDeliveryRetired(context.Context, ResourceCoordinate, time.Time) error
	MarkClaudeDeliverySkipped(context.Context, ResourceCoordinate, string, string, time.Time) error
	MarkClaudeDeliveryConflict(context.Context, ResourceCoordinate, string, string, time.Time) error
	RecordClaudeDeliveryFailure(context.Context, ResourceCoordinate, string, string, string, time.Time) error
	GetClaudeDeliveryPlugin(context.Context, ResourceCoordinate) (ClaudeDeliveryPlugin, error)
	ListClaudeDeliveryPlugins(context.Context) ([]ClaudeDeliveryPlugin, error)
	ListClaudeDeliveryDesired(context.Context, PublicationStore, []RegistryID) ([]ClaudeDeliveryDesired, error)
}

const ClaudeDeliverySchema = `
CREATE TABLE claude_delivery_plugins (
 source_marketplace_id TEXT NOT NULL,
 registry_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 resource_name TEXT NOT NULL,
 plugin_name TEXT NOT NULL,
 plugin_id TEXT NOT NULL DEFAULT '',
 target_marketplace_id TEXT NOT NULL DEFAULT '',
 published_archive_digest TEXT NOT NULL DEFAULT '',
 published_content_digest TEXT NOT NULL DEFAULT '',
 published_version_id TEXT NOT NULL DEFAULT '',
 latest_version_id TEXT NOT NULL DEFAULT '',
 served_version_id TEXT NOT NULL DEFAULT '',
 content_scan_status TEXT NOT NULL DEFAULT '',
 reach TEXT NOT NULL DEFAULT '',
 installation_preference TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 last_error_code TEXT NOT NULL DEFAULT '',
 last_error_message TEXT NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL,
 updated_at BIGINT NOT NULL,
 PRIMARY KEY (source_marketplace_id, registry_id, kind, resource_name)
);
CREATE INDEX claude_delivery_state ON claude_delivery_plugins(state, updated_at);
`

func UpsertClaudeDeliveryPublished(ctx context.Context, q ManagedSQLQueries, row ClaudeDeliveryPlugin) error {
	if err := row.validate(); err != nil || row.State != ClaudeDeliveryStatePublished || row.PluginID == "" {
		return errors.New("invalid published Claude delivery state")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = row.CreatedAt
	}
	_, err := q.ExecContext(ctx, `INSERT INTO claude_delivery_plugins (
		source_marketplace_id, registry_id, kind, resource_name, plugin_name, plugin_id,
		target_marketplace_id, published_archive_digest, published_content_digest,
		published_version_id, latest_version_id, served_version_id, content_scan_status,
		reach, installation_preference, state, last_error_code, last_error_message,
		created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'','',$17,$18)
		ON CONFLICT (source_marketplace_id, registry_id, kind, resource_name) DO UPDATE SET
		plugin_name=CASE WHEN claude_delivery_plugins.plugin_name='' THEN excluded.plugin_name ELSE claude_delivery_plugins.plugin_name END,
		plugin_id=excluded.plugin_id, target_marketplace_id=excluded.target_marketplace_id,
		published_archive_digest=excluded.published_archive_digest,
		published_content_digest=excluded.published_content_digest,
		published_version_id=excluded.published_version_id,
		latest_version_id=excluded.latest_version_id, served_version_id=excluded.served_version_id,
		content_scan_status=excluded.content_scan_status, reach=excluded.reach,
		installation_preference=excluded.installation_preference, state=excluded.state,
		last_error_code='', last_error_message='', updated_at=excluded.updated_at`,
		row.Coordinate.Marketplace, row.Coordinate.Registry, row.Coordinate.Kind, row.Coordinate.Name,
		row.PluginName, row.PluginID, row.TargetMarketplaceID, row.PublishedArchiveDigest,
		row.PublishedContentDigest, row.PublishedVersionID, row.LatestVersionID, row.ServedVersionID,
		row.ContentScanStatus, row.Reach, row.InstallationPreference, row.State,
		row.CreatedAt.UTC().UnixNano(), row.UpdatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("upsert Claude delivery state: %w", err)
	}
	return nil
}

func MarkClaudeDeliveryRetired(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := q.ExecContext(ctx, `UPDATE claude_delivery_plugins SET state=$5,
		installation_preference='not_available', last_error_code='', last_error_message='',
		updated_at=$6 WHERE source_marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND resource_name=$4`,
		coordinate.Marketplace, coordinate.Registry, coordinate.Kind, coordinate.Name,
		ClaudeDeliveryStateRetired, at.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("retire Claude delivery state: %w", err)
	}
	return nil
}

func MarkClaudeDeliverySkipped(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate, pluginName, code string, at time.Time) error {
	return upsertClaudeDeliveryDiagnostic(ctx, q, coordinate, pluginName, ClaudeDeliveryStateSkipped, code, at)
}

func MarkClaudeDeliveryConflict(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate, pluginName, code string, at time.Time) error {
	return upsertClaudeDeliveryDiagnostic(ctx, q, coordinate, pluginName, ClaudeDeliveryStateConflict, code, at)
}

func RecordClaudeDeliveryFailure(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate, pluginName, targetMarketplaceID, code string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	code = safeClaudeDeliveryErrorCode(code)
	message := boundedClaudeDeliveryFailureMessage(code)
	_, err := q.ExecContext(ctx, `INSERT INTO claude_delivery_plugins (
		source_marketplace_id, registry_id, kind, resource_name, plugin_name, target_marketplace_id,
		state, last_error_code, last_error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
		ON CONFLICT (source_marketplace_id, registry_id, kind, resource_name) DO UPDATE SET
		plugin_name=CASE WHEN claude_delivery_plugins.plugin_name='' THEN excluded.plugin_name ELSE claude_delivery_plugins.plugin_name END,
		target_marketplace_id=CASE WHEN excluded.target_marketplace_id='' THEN claude_delivery_plugins.target_marketplace_id ELSE excluded.target_marketplace_id END,
		state=excluded.state, last_error_code=excluded.last_error_code,
		last_error_message=excluded.last_error_message, updated_at=excluded.updated_at`,
		coordinate.Marketplace, coordinate.Registry, coordinate.Kind, coordinate.Name, pluginName,
		targetMarketplaceID, ClaudeDeliveryStateFailed, code, message, at.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("record Claude delivery failure: %w", err)
	}
	return nil
}

func upsertClaudeDeliveryDiagnostic(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate, pluginName string, state ClaudeDeliveryState, code string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	code = safeClaudeDeliveryErrorCode(code)
	_, err := q.ExecContext(ctx, `INSERT INTO claude_delivery_plugins (
		source_marketplace_id, registry_id, kind, resource_name, plugin_name, state,
		last_error_code, last_error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
		ON CONFLICT (source_marketplace_id, registry_id, kind, resource_name) DO UPDATE SET
		plugin_name=CASE WHEN claude_delivery_plugins.plugin_name='' THEN excluded.plugin_name ELSE claude_delivery_plugins.plugin_name END,
		state=excluded.state, last_error_code=excluded.last_error_code,
		last_error_message=excluded.last_error_message, updated_at=excluded.updated_at`,
		coordinate.Marketplace, coordinate.Registry, coordinate.Kind, coordinate.Name,
		pluginName, state, code, boundedClaudeDeliveryFailureMessage(code), at.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("record Claude delivery state: %w", err)
	}
	return nil
}

func safeClaudeDeliveryErrorCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) == 0 || len(code) > 120 {
		return "delivery.failed"
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return "delivery.failed"
		}
	}
	return code
}

func boundedClaudeDeliveryFailureMessage(code string) string {
	message := "delivery failed: " + safeClaudeDeliveryErrorCode(code)
	if len(message) > ClaudeDeliveryFailureMessageLimit {
		message = message[:ClaudeDeliveryFailureMessageLimit]
	}
	return message
}

func GetClaudeDeliveryPlugin(ctx context.Context, q ManagedSQLQueries, coordinate ResourceCoordinate) (ClaudeDeliveryPlugin, error) {
	row := q.QueryRowContext(ctx, `SELECT source_marketplace_id,registry_id,kind,resource_name,
		plugin_name,plugin_id,target_marketplace_id,published_archive_digest,published_content_digest,
		published_version_id,latest_version_id,served_version_id,content_scan_status,reach,
		installation_preference,state,last_error_code,last_error_message,created_at,updated_at
		FROM claude_delivery_plugins WHERE source_marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND resource_name=$4`,
		coordinate.Marketplace, coordinate.Registry, coordinate.Kind, coordinate.Name)
	result, err := scanClaudeDeliveryPlugin(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ClaudeDeliveryPlugin{}, ErrNotFound
	}
	return result, err
}

func ListClaudeDeliveryPlugins(ctx context.Context, q ManagedSQLQueries) ([]ClaudeDeliveryPlugin, error) {
	rows, err := q.QueryContext(ctx, `SELECT source_marketplace_id,registry_id,kind,resource_name,
		plugin_name,plugin_id,target_marketplace_id,published_archive_digest,published_content_digest,
		published_version_id,latest_version_id,served_version_id,content_scan_status,reach,
		installation_preference,state,last_error_code,last_error_message,created_at,updated_at
		FROM claude_delivery_plugins ORDER BY source_marketplace_id,registry_id,kind,resource_name`)
	if err != nil {
		return nil, fmt.Errorf("list Claude delivery state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]ClaudeDeliveryPlugin, 0)
	for rows.Next() {
		row, err := scanClaudeDeliveryPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Claude delivery state: %w", err)
	}
	return out, nil
}

type claudeDeliveryScanner interface {
	Scan(...any) error
}

func scanClaudeDeliveryPlugin(scanner claudeDeliveryScanner) (ClaudeDeliveryPlugin, error) {
	var row ClaudeDeliveryPlugin
	var created, updated int64
	err := scanner.Scan(
		&row.Coordinate.Marketplace, &row.Coordinate.Registry, &row.Coordinate.Kind, &row.Coordinate.Name,
		&row.PluginName, &row.PluginID, &row.TargetMarketplaceID, &row.PublishedArchiveDigest,
		&row.PublishedContentDigest, &row.PublishedVersionID, &row.LatestVersionID,
		&row.ServedVersionID, &row.ContentScanStatus, &row.Reach, &row.InstallationPreference,
		&row.State, &row.LastErrorCode, &row.LastErrorMessage, &created, &updated)
	if err != nil {
		return ClaudeDeliveryPlugin{}, err
	}
	row.CreatedAt = time.Unix(0, created).UTC()
	row.UpdatedAt = time.Unix(0, updated).UTC()
	return row, nil
}

func ReadClaudeDeliveryDesired(ctx context.Context, metadata MetadataStore, publications PublicationStore, registries []RegistryID) ([]ClaudeDeliveryDesired, error) {
	unique := make(map[RegistryID]struct{}, len(registries))
	ordered := make([]RegistryID, 0, len(registries))
	for _, registry := range registries {
		if registry == "" {
			continue
		}
		if _, exists := unique[registry]; exists {
			continue
		}
		unique[registry] = struct{}{}
		ordered = append(ordered, registry)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	var out []ClaudeDeliveryDesired
	for _, registry := range ordered {
		publication, err := publications.CurrentRegistryPublication(ctx, registry)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read current Claude delivery publication: %w", err)
		}
		assessments := make(map[ResourceCoordinate]ResourceAssessment, len(publication.Assessments))
		for _, assessment := range publication.Assessments {
			assessments[assessment.Coordinate] = assessment
		}
		for _, occurrence := range publication.Resources {
			coordinate := occurrence.Coordinate
			if coordinate.Kind == "collection" {
				continue
			}
			assessment, exists := assessments[coordinate]
			if !exists || assessment.ContentDigest != occurrence.ContentDigest || !assessmentAllowsDelivery(assessment.Decision) {
				continue
			}
			revision, err := metadata.GetResourceRevision(ctx, coordinate, occurrence.ContentDigest)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read current Claude delivery resource: %w", err)
			}
			if revision.Revocation != nil || !assessmentAllowsDelivery(revision.Assessment.Decision) {
				continue
			}
			revision.Occurrence = occurrence
			out = append(out, ClaudeDeliveryDesired{Resource: revision, PublicationID: publication.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Resource.Coordinate.String() < out[j].Resource.Coordinate.String()
	})
	return out, nil
}

func assessmentAllowsDelivery(decision AssessmentDecision) bool {
	return decision == AssessmentDecisionAllow || decision == AssessmentDecisionWarn
}

func (row ClaudeDeliveryPlugin) validate() error {
	if row.Coordinate.Marketplace == "" || row.Coordinate.Registry == "" ||
		row.Coordinate.Kind == "" || row.Coordinate.Name == "" || row.PluginName == "" {
		return errors.New("incomplete Claude delivery identity")
	}
	if row.State != ClaudeDeliveryStatePublished && row.State != ClaudeDeliveryStateRetired &&
		row.State != ClaudeDeliveryStateSkipped && row.State != ClaudeDeliveryStateConflict &&
		row.State != ClaudeDeliveryStateFailed {
		return errors.New("invalid Claude delivery state")
	}
	return nil
}
