package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var ErrRevisionRequired = errors.New("marketplace/admin: revision required")

type View struct {
	Revision int64    `json:"revision"`
	Snapshot Snapshot `json:"snapshot"`
}

type Mutation struct {
	Action              string             `json:"action"`
	RegistryID          store.RegistryID   `json:"registry_id,omitempty"`
	Registry            *Registry          `json:"registry,omitempty"`
	LicensePolicy       *LicensePolicy     `json:"license_policy,omitempty"`
	ExpectedRevision    int64              `json:"expected_revision"`
	Actor               store.ManagedActor `json:"-"`
	Reason              string             `json:"reason"`
	RequestID           string             `json:"-"`
	IdempotencyKey      string             `json:"-"`
	ConfirmOriginMove   bool               `json:"confirm_origin_move"`
	ConfirmUnrestricted bool               `json:"confirm_unrestricted"`
}

func (s Service) Read(ctx context.Context, id store.MarketplaceID) (View, error) {
	managed, ok := s.Metadata.(store.ManagedConfigurationStore)
	if !ok {
		return View{}, store.ErrTopology
	}
	current, err := managed.CurrentManagedConfiguration(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("read managed configuration: %w", err)
	}
	var snapshot Snapshot
	if json.Unmarshal(current.Snapshot, &snapshot) != nil || snapshot.Version != 1 || snapshot.PolicyRevision < 1 {
		return View{}, store.ErrManagedStateCorrupt
	}
	return View{Revision: current.Revision, Snapshot: snapshot}, nil
}

func (s Service) History(ctx context.Context, id store.MarketplaceID, before int64, limit int) ([]store.ManagedChange, error) {
	managed, ok := s.Metadata.(store.ManagedConfigurationStore)
	if !ok {
		return nil, store.ErrTopology
	}
	return managed.ManagedConfigurationHistory(ctx, id, before, limit)
}

func (r Registry) Active() bool {
	return !r.Paused && !r.Withdrawn && !r.Retired && r.Lifecycle == "active"
}

func (s Service) Validate(ctx context.Context, id store.MarketplaceID, m Mutation) error {
	if err := validateDraft(m); err != nil {
		return err
	}
	view, err := s.Read(ctx, id)
	if err != nil {
		return err
	}
	_, _, err = s.prepare(view, m)
	return err
}

func validateDraft(m Mutation) error {
	if m.ExpectedRevision <= 0 {
		return ErrRevisionRequired
	}
	if len(m.Reason) > 1024 || strings.ContainsAny(m.Reason, "\x00\r\n") {
		return store.ErrManagedChangeInvalid
	}
	return nil
}

func (s Service) prepare(view View, m Mutation) (Snapshot, bool, error) {
	if err := validateDraft(m); err != nil {
		return Snapshot{}, false, err
	}
	if view.Revision != m.ExpectedRevision {
		return Snapshot{}, false, store.ErrManagedRevisionConflict
	}
	snapshot := view.Snapshot
	snapshot.Registries = slices.Clone(snapshot.Registries)
	work, err := s.mutate(&snapshot, m)
	return snapshot, work, err
}

func (s Service) Apply(ctx context.Context, id store.MarketplaceID, m Mutation) (store.ManagedChange, error) {
	if err := validateDraft(m); err != nil {
		return store.ManagedChange{}, err
	}
	if s.Metadata == nil || strings.TrimSpace(string(id)) == "" || strings.TrimSpace(m.Actor.Issuer) == "" || strings.TrimSpace(m.Actor.Subject) == "" || strings.TrimSpace(m.Reason) == "" || strings.TrimSpace(m.RequestID) == "" || strings.TrimSpace(m.IdempotencyKey) == "" {
		return store.ManagedChange{}, store.ErrManagedChangeInvalid
	}
	body, err := json.Marshal(m)
	if err != nil {
		return store.ManagedChange{}, ErrInvalidConfiguration
	}
	requestDigest := digest(body)
	var accepted store.ManagedChange
	err = s.Metadata.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		managed, ok := tx.(store.ManagedConfigurationStore)
		if !ok {
			return store.ErrTopology
		}
		prior, err := managed.FindManagedChange(ctx, id, m.Actor, m.Action, m.IdempotencyKey)
		if err == nil {
			if prior.RequestDigest != requestDigest {
				return store.ErrManagedIdempotencyConflict
			}
			accepted = prior
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("find managed change: %w", err)
		}
		local := s
		local.Metadata = tx
		view, err := local.Read(ctx, id)
		if err != nil {
			return err
		}
		snapshot, work, err := s.prepare(view, m)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return ErrInvalidConfiguration
		}
		var jobs []store.Job
		if work {
			identity, _ := json.Marshal([]string{string(id), m.Actor.Issuer, m.Actor.Subject, m.Action, m.IdempotencyKey})
			key := "managed-change-" + digest(identity)
			payload, _ := json.Marshal(struct {
				Marketplace store.MarketplaceID `json:"marketplace_id"`
				Revision    int64               `json:"revision"`
				RegistryID  store.RegistryID    `json:"registry_id,omitempty"`
			}{id, view.Revision + 1, m.RegistryID})
			jobs = []store.Job{{ID: store.JobID(key), Kind: "managed-reconcile", Payload: payload, IdempotencyKey: key, MaxAttempts: 5}}
		}
		accepted, err = managed.ApplyManagedConfiguration(ctx, store.ManagedChangeRequest{Change: store.ManagedChange{Marketplace: id, ParentRevision: view.Revision, Actor: m.Actor, Operation: m.Action, Reason: m.Reason, RequestID: m.RequestID, IdempotencyKey: m.IdempotencyKey, RequestDigest: requestDigest, Snapshot: encoded}, Jobs: jobs})
		return err
	})
	if err != nil {
		slog.WarnContext(ctx, "managed operation rejected", "operation", m.Action)
		return store.ManagedChange{}, err
	}
	slog.InfoContext(ctx, "managed operation accepted", "operation", m.Action, "revision", accepted.Revision)
	return accepted, nil
}

func (s Service) mutate(snapshot *Snapshot, m Mutation) (bool, error) {
	if m.Action == "set_policy" {
		if m.RegistryID != "" || m.Registry != nil || m.LicensePolicy == nil || !validPolicy(*m.LicensePolicy, false) || len(m.LicensePolicy.Allowlist) == 0 && !m.ConfirmUnrestricted {
			return false, ErrInvalidConfiguration
		}
		if !reflect.DeepEqual(snapshot.LicensePolicy, *m.LicensePolicy) {
			snapshot.PolicyRevision++
			snapshot.LicensePolicy = *m.LicensePolicy
		}
		return slices.ContainsFunc(snapshot.Registries, func(r Registry) bool { return r.Active() }), nil
	}
	if m.RegistryID == "" || m.LicensePolicy != nil {
		return false, ErrInvalidConfiguration
	}
	index := slices.IndexFunc(snapshot.Registries, func(r Registry) bool { return r.ID == m.RegistryID })
	if m.Action == "upsert_registry" {
		if m.Registry == nil || m.Registry.ID != m.RegistryID {
			return false, ErrInvalidConfiguration
		}
		r := *m.Registry
		if index < 0 {
			if len(snapshot.Registries) >= 10000 || r.Generation != 0 || r.Paused || r.Withdrawn || r.Retired || (r.Lifecycle != "" && r.Lifecycle != "active") || len(r.Aliases) != 0 || len(r.HistoricalOrigins) != 0 || len(r.NonLicenseOverrides) != 0 {
				return false, ErrInvalidConfiguration
			}
			r.Generation = 1
			r.Lifecycle = "active"
		} else {
			old := snapshot.Registries[index]
			if r.Generation != old.Generation || r.Paused != old.Paused || r.Withdrawn != old.Withdrawn || r.Retired != old.Retired || r.Lifecycle != old.Lifecycle || !reflect.DeepEqual(r.NonLicenseOverrides, old.NonLicenseOverrides) || !reflect.DeepEqual(r.Aliases, old.Aliases) || !reflect.DeepEqual(r.HistoricalOrigins, old.HistoricalOrigins) {
				return false, ErrInvalidConfiguration
			}
			if r.Origin != old.Origin && !m.ConfirmOriginMove {
				return false, ErrInvalidConfiguration
			}
			if r.Slug != old.Slug {
				r.Aliases = uniqueNames(append(slices.Clone(old.Aliases), old.Slug), "")
			}
			if r.Origin != old.Origin {
				r.HistoricalOrigins = uniqueNames(append(slices.Clone(old.HistoricalOrigins), old.Origin), "")
			}
			if !reflect.DeepEqual(r, old) {
				r.Generation++
			}
		}
		if err := s.validateRegistry(r); err != nil {
			return false, err
		}
		for _, other := range snapshot.Registries {
			if other.ID == r.ID {
				continue
			}
			for _, name := range append(slices.Clone(r.Aliases), r.Slug) {
				if name == other.Slug || slices.Contains(other.Aliases, name) {
					return false, ErrInvalidConfiguration
				}
			}
		}
		if index < 0 {
			snapshot.Registries = append(snapshot.Registries, r)
			slices.SortFunc(snapshot.Registries, func(a, b Registry) int { return strings.Compare(string(a.ID), string(b.ID)) })
		} else {
			snapshot.Registries[index] = r
		}
		return r.Active(), nil
	}
	if m.Registry != nil {
		return false, ErrInvalidConfiguration
	}
	if index < 0 {
		return false, store.ErrNotFound
	}
	r := &snapshot.Registries[index]
	switch m.Action {
	case "pause":
		if !r.Active() {
			return false, ErrInvalidConfiguration
		}
		r.Paused = true
	case "resume":
		if !r.Paused || r.Withdrawn || r.Retired {
			return false, ErrInvalidConfiguration
		}
		r.Paused = false
	case "withdraw":
		if r.Withdrawn || r.Retired {
			return false, ErrInvalidConfiguration
		}
		r.Withdrawn = true
		r.Paused = true
	case "restore":
		if !r.Withdrawn || r.Retired {
			return false, ErrInvalidConfiguration
		}
		r.Withdrawn = false
		r.Paused = true
	case "retire":
		if !r.Withdrawn || r.Retired {
			return false, ErrInvalidConfiguration
		}
		r.Retired = true
	case "restore_retired":
		if !r.Retired {
			return false, ErrInvalidConfiguration
		}
		r.Retired = false
		r.Withdrawn = true
		r.Paused = true
	case "reindex":
		if !r.Active() {
			return false, ErrInvalidConfiguration
		}
		return true, s.validateRegistry(*r)
	default:
		return false, ErrInvalidConfiguration
	}
	r.Generation++
	r.Lifecycle = "active"
	if r.Paused {
		r.Lifecycle = "paused"
	}
	if r.Withdrawn {
		r.Lifecycle = "withdrawn"
	}
	if r.Retired {
		r.Lifecycle = "retired"
	}
	if r.Active() {
		if err := s.validateRegistry(*r); err != nil {
			return false, err
		}
	}
	return r.Active(), nil
}

func validPolicy(p LicensePolicy, override bool) bool {
	if (!override || p.Undeclared != "") && !validUndeclared(p.Undeclared) {
		return false
	}
	if override && p.Allowlist != nil && len(p.Allowlist) == 0 {
		return false
	}
	for _, license := range p.Allowlist {
		if strings.TrimSpace(license) == "" || len(license) > 256 || strings.ContainsAny(license, "\x00\r\n") {
			return false
		}
	}
	return !override || p.Allowlist != nil || p.Undeclared != ""
}
func (s Service) validateRegistry(r Registry) error {
	if !originAllowed(r.Origin, s.Deployment.AllowedOrigins) || strings.ContainsAny(string(r.ID), "/\\@\r\n\x00 ") || len(r.ID) > 256 || r.Acquisition == "" {
		return ErrInvalidConfiguration
	}
	if r.ProfileID != "" {
		p, ok := s.Deployment.Profiles[r.ProfileID]
		if !ok || !originAllowed(r.Origin, p.AllowedOrigins) || strings.ContainsAny(r.ProfileID, "/\\\r\n\x00 ") {
			return ErrInvalidConfiguration
		}
	}
	if r.LicenseOverride != nil && !validPolicy(*r.LicenseOverride, true) {
		return ErrInvalidConfiguration
	}
	source := config.Registry{ID: string(r.ID), Slug: r.Slug, Origin: r.Origin, Ref: r.Ref, Acquisition: string(r.Acquisition), Publisher: config.Publisher{Name: r.Publisher.Name, URL: r.Publisher.URL}, License: r.License}
	for _, root := range r.Roots {
		if strings.ContainsAny(root.Path, "\\\x00\r\n") {
			return ErrInvalidConfiguration
		}
		source.Roots = append(source.Roots, config.RegistryRoot{Path: root.Path, SourceHarness: root.SourceHarness})
	}
	if err := source.Validate(config.Policy{}); err != nil {
		return ErrInvalidConfiguration
	}
	return nil
}

func (s Service) RestoreDraft(ctx context.Context, id store.MarketplaceID, revision int64, registryID store.RegistryID) (Mutation, error) {
	current, err := s.Read(ctx, id)
	if err != nil {
		return Mutation{}, err
	}
	if revision < 1 || revision > current.Revision {
		return Mutation{}, store.ErrNotFound
	}
	history, err := s.History(ctx, id, revision+1, 1)
	if err != nil {
		return Mutation{}, err
	}
	if len(history) != 1 || history[0].Revision != revision {
		return Mutation{}, store.ErrNotFound
	}
	var old Snapshot
	if json.Unmarshal(history[0].Snapshot, &old) != nil {
		return Mutation{}, store.ErrManagedStateCorrupt
	}
	m := Mutation{ExpectedRevision: current.Revision, RegistryID: registryID}
	if registryID == "" {
		m.Action = "set_policy"
		m.LicensePolicy = &old.LicensePolicy
		return m, nil
	}
	i := slices.IndexFunc(old.Registries, func(r Registry) bool { return r.ID == registryID })
	j := slices.IndexFunc(current.Snapshot.Registries, func(r Registry) bool { return r.ID == registryID })
	if i < 0 || j < 0 {
		return Mutation{}, store.ErrNotFound
	}
	r := old.Registries[i]
	now := current.Snapshot.Registries[j]
	r.Aliases = now.Aliases
	r.HistoricalOrigins = now.HistoricalOrigins
	r.Generation = now.Generation
	r.Lifecycle = now.Lifecycle
	r.Paused = now.Paused
	r.Withdrawn = now.Withdrawn
	r.Retired = now.Retired
	r.NonLicenseOverrides = now.NonLicenseOverrides
	if err := s.validateRegistry(r); err != nil {
		return Mutation{}, err
	}
	m.Action = "upsert_registry"
	m.Registry = &r
	return m, nil
}
