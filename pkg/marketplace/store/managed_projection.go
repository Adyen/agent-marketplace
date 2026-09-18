package store

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"
)

type managedLicensePolicy struct {
	Allowlist  []string `json:"allowlist"`
	Undeclared string   `json:"undeclared"`
}

type managedRegistryProjection struct {
	ID                  RegistryID            `json:"id"`
	Slug                string                `json:"slug"`
	Aliases             []string              `json:"aliases"`
	Origin              string                `json:"origin"`
	HistoricalOrigins   []string              `json:"historical_origins"`
	Ref                 string                `json:"ref"`
	Roots               []RegistryRoot        `json:"roots"`
	Acquisition         AcquisitionMode       `json:"acquisition"`
	Publisher           Publisher             `json:"publisher"`
	License             string                `json:"license"`
	ProfileID           string                `json:"profile_id"`
	NonLicenseOverrides map[string]any        `json:"non_license_overrides"`
	LicenseOverride     *managedLicensePolicy `json:"license_override"`
	Generation          int64                 `json:"generation"`
	Lifecycle           string                `json:"lifecycle"`
	Paused              bool                  `json:"paused"`
	Withdrawn           bool                  `json:"withdrawn"`
	Retired             bool                  `json:"retired"`
}

type managedProjectionSnapshot struct {
	Version        int                         `json:"version"`
	PolicyRevision int64                       `json:"policy_revision"`
	LicensePolicy  managedLicensePolicy        `json:"license_policy"`
	Registries     []managedRegistryProjection `json:"registries"`
}

func managedToken(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "/\\\x00\r\n\t ")
}

func decodeManagedProjection(raw json.RawMessage) (managedProjectionSnapshot, error) {
	var snapshot managedProjectionSnapshot
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.Version != 1 || snapshot.PolicyRevision < 1 || snapshot.Registries == nil || len(snapshot.Registries) > 10000 {
		return snapshot, ErrManagedChangeInvalid
	}
	ids := make(map[RegistryID]bool, len(snapshot.Registries))
	for _, registry := range snapshot.Registries {
		if !managedToken(string(registry.ID)) || !managedToken(registry.Slug) || registry.Generation < 1 || ids[registry.ID] || registry.Acquisition.Validate() != nil || registry.Origin == "" || registry.Ref == "" {
			return snapshot, ErrManagedChangeInvalid
		}
		if len(registry.Aliases) > 1000 || len(registry.HistoricalOrigins) > 1000 || len(registry.Roots) > 1000 {
			return snapshot, ErrManagedChangeInvalid
		}
		for _, alias := range registry.Aliases {
			if !managedToken(alias) {
				return snapshot, ErrManagedChangeInvalid
			}
		}
		for _, value := range append([]string{registry.Origin, registry.Ref, registry.ProfileID}, registry.HistoricalOrigins...) {
			if len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
				return snapshot, ErrManagedChangeInvalid
			}
		}
		for _, root := range registry.Roots {
			if root.Path != "" && root.Path != "." {
				if _, err := NormalizeSourcePath(root.Path); err != nil {
					return snapshot, ErrManagedChangeInvalid
				}
			}
		}
		switch registry.Lifecycle {
		case "active", "paused", "withdrawn", "retired":
		default:
			return snapshot, ErrManagedChangeInvalid
		}
		ids[registry.ID] = true
	}
	return snapshot, nil
}

type managedExistingIdentity struct {
	id      RegistryID
	slug    string
	aliases []string
	origins []string
}

func ProjectManagedRegistries(ctx context.Context, q ManagedSQLQueries, change ManagedChange, previous *ManagedChange) error {
	snapshot, err := decodeManagedProjection(change.Snapshot)
	if err != nil {
		return err
	}
	prior := make(map[RegistryID]managedRegistryProjection)
	if previous != nil {
		old, err := decodeManagedProjection(previous.Snapshot)
		if err != nil {
			return ErrManagedStateCorrupt
		}
		if snapshot.PolicyRevision < old.PolicyRevision || (!reflect.DeepEqual(snapshot.LicensePolicy, old.LicensePolicy) && snapshot.PolicyRevision == old.PolicyRevision) {
			return ErrManagedChangeInvalid
		}
		for _, registry := range old.Registries {
			prior[registry.ID] = registry
		}
	}
	next := make(map[RegistryID]bool, len(snapshot.Registries))
	for _, registry := range snapshot.Registries {
		next[registry.ID] = true
		if old, ok := prior[registry.ID]; ok {
			if registry.Generation < old.Generation {
				return ErrManagedChangeInvalid
			}
			candidate := registry
			candidate.Generation = old.Generation
			if !reflect.DeepEqual(candidate, old) && registry.Generation == old.Generation {
				return ErrManagedChangeInvalid
			}
		}
	}
	for id := range prior {
		if !next[id] {
			return ErrManagedIdentityConflict
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT id,slug,aliases,origins FROM registries ORDER BY id LIMIT 10001`)
	if err != nil {
		return err
	}
	existing := make(map[RegistryID]managedExistingIdentity)
	names := make(map[string]RegistryID)
	for rows.Next() {
		if len(existing) >= 10000 {
			_ = rows.Close()
			return ErrManagedStateCorrupt
		}
		var item managedExistingIdentity
		var aliases, origins string
		if err := rows.Scan(&item.id, &item.slug, &aliases, &origins); err != nil {
			_ = rows.Close()
			return err
		}
		if json.Unmarshal([]byte(aliases), &item.aliases) != nil || json.Unmarshal([]byte(origins), &item.origins) != nil {
			_ = rows.Close()
			return ErrManagedStateCorrupt
		}
		for _, name := range append([]string{string(item.id), item.slug}, item.aliases...) {
			if !managedToken(name) || len(names) > 100000 {
				_ = rows.Close()
				return ErrManagedIdentityConflict
			}
			if owner, ok := names[name]; ok && owner != item.id {
				_ = rows.Close()
				return ErrManagedIdentityConflict
			}
			names[name] = item.id
		}
		existing[item.id] = item
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, registry := range snapshot.Registries {
		for _, name := range append([]string{string(registry.ID), registry.Slug}, registry.Aliases...) {
			if owner, ok := names[name]; ok && owner != registry.ID {
				return ErrManagedIdentityConflict
			}
			names[name] = registry.ID
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		if _, err := q.ExecContext(ctx, `INSERT INTO managed_registry_names (name,registry_id) VALUES ($1,$2) ON CONFLICT(name) DO NOTHING`, name, names[name]); err != nil {
			return err
		}
		var owner RegistryID
		if err := q.QueryRowContext(ctx, `SELECT registry_id FROM managed_registry_names WHERE name=$1`, name).Scan(&owner); err != nil {
			return err
		}
		if owner != names[name] {
			return ErrManagedIdentityConflict
		}
	}
	sort.Slice(snapshot.Registries, func(i, j int) bool { return snapshot.Registries[i].ID < snapshot.Registries[j].ID })
	for _, registry := range snapshot.Registries {
		if _, known := existing[registry.ID]; known && previous != nil {
			if _, wasManaged := prior[registry.ID]; !wasManaged {
				return ErrManagedIdentityConflict
			}
		}
		claim, err := q.ExecContext(ctx, `INSERT INTO managed_registry_identities (registry_id,marketplace_id) VALUES ($1,$2) ON CONFLICT(registry_id) DO NOTHING`, registry.ID, change.Marketplace)
		if err != nil {
			return err
		}
		claimed, err := claim.RowsAffected()
		if err != nil {
			return err
		}
		var marketplace MarketplaceID
		if err := q.QueryRowContext(ctx, `SELECT marketplace_id FROM managed_registry_identities WHERE registry_id=$1`, registry.ID).Scan(&marketplace); err != nil {
			return err
		}
		if marketplace != change.Marketplace {
			return ErrManagedIdentityConflict
		}
		old, exists := existing[registry.ID]
		if before, known := prior[registry.ID]; known && exists && claimed == 0 && sameManagedRegistrySettings(before, registry) {
			if _, err := q.ExecContext(ctx, `UPDATE registries SET listed=$1 WHERE id=$2 AND listed<>$1`, true, registry.ID); err != nil {
				return err
			}
			continue
		}
		aliases := retainedManagedStrings(registry.Slug, registry.Aliases, old.aliases, []string{old.slug})
		origins := append(retainedManagedStrings(registry.Origin, registry.HistoricalOrigins, old.origins), registry.Origin)
		fields := []any{aliases, origins, registry.Roots, registry.Publisher, registry.NonLicenseOverrides}
		encoded := make([]string, len(fields))
		for i, field := range fields {
			data, err := json.Marshal(field)
			if err != nil {
				return err
			}
			encoded[i] = string(data)
		}
		now := time.Now().UTC().UnixMicro()
		_, err = q.ExecContext(ctx, `INSERT INTO registries
   (id,slug,aliases,origins,ref,roots,acquisition,publisher,license,credentials_ref,policy_overrides,listed,refresh_status,refresh_error,refreshed_at,created_at,updated_at)
   VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'unknown','',0,$13,$13)
   ON CONFLICT(id) DO UPDATE SET slug=excluded.slug,aliases=excluded.aliases,origins=excluded.origins,ref=excluded.ref,roots=excluded.roots,acquisition=excluded.acquisition,publisher=excluded.publisher,license=excluded.license,credentials_ref=excluded.credentials_ref,policy_overrides=excluded.policy_overrides,listed=excluded.listed,updated_at=excluded.updated_at
   WHERE registries.slug<>excluded.slug OR registries.aliases<>excluded.aliases OR registries.origins<>excluded.origins OR registries.ref<>excluded.ref OR registries.roots<>excluded.roots OR registries.acquisition<>excluded.acquisition OR registries.publisher<>excluded.publisher OR registries.license<>excluded.license OR registries.credentials_ref<>excluded.credentials_ref OR registries.policy_overrides<>excluded.policy_overrides OR registries.listed<>excluded.listed`, registry.ID, registry.Slug, encoded[0], encoded[1], registry.Ref, encoded[2], registry.Acquisition, encoded[3], registry.License, registry.ProfileID, encoded[4], true, now)
		if err != nil {
			return err
		}
	}
	return nil
}

func sameManagedRegistrySettings(left, right managedRegistryProjection) bool {
	left.Generation, right.Generation = 0, 0
	left.Lifecycle, right.Lifecycle = "", ""
	left.Paused, right.Paused = false, false
	left.Withdrawn, right.Withdrawn = false, false
	left.Retired, right.Retired = false, false
	return reflect.DeepEqual(left, right)
}

func retainedManagedStrings(exclude string, groups ...[]string) []string {
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, value := range group {
			if value != "" && value != exclude {
				seen[value] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
