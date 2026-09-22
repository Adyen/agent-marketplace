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
	Action                   string              `json:"action"`
	RegistryID               store.RegistryID    `json:"registry_id,omitempty"`
	Registry                 *Registry           `json:"registry,omitempty"`
	LicensePolicy            *LicensePolicy      `json:"license_policy,omitempty"`
	DistributionPolicy       *DistributionPolicy `json:"distribution_policy,omitempty"`
	ExpectedRevision         int64               `json:"expected_revision"`
	Actor                    store.ManagedActor  `json:"-"`
	Reason                   string              `json:"reason"`
	RequestID                string              `json:"-"`
	IdempotencyKey           string              `json:"-"`
	ConfirmOriginMove        bool                `json:"confirm_origin_move"`
	ConfirmUnrestricted      bool                `json:"confirm_unrestricted"`
	ConfirmRejectedResources bool                `json:"confirm_rejected_resources"`
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
	normalizeDistributionPolicy(&snapshot)
	return View{Revision: current.Revision, Snapshot: snapshot}, nil
}

func (s Service) History(ctx context.Context, id store.MarketplaceID, before int64, limit int) ([]store.ManagedChange, error) {
	managed, ok := s.Metadata.(store.ManagedConfigurationStore)
	if !ok {
		return nil, store.ErrTopology
	}
	changes, err := managed.ManagedConfigurationHistory(ctx, id, before, limit)
	if err != nil {
		return nil, err
	}
	for i := range changes {
		var snapshot Snapshot
		if json.Unmarshal(changes[i].Snapshot, &snapshot) != nil {
			return nil, store.ErrManagedStateCorrupt
		}
		normalizeDistributionPolicy(&snapshot)
		changes[i].Snapshot, err = json.Marshal(snapshot)
		if err != nil {
			return nil, store.ErrManagedStateCorrupt
		}
	}
	return changes, nil
}

func normalizeDistributionPolicy(snapshot *Snapshot) {
	if snapshot.DistributionPolicy.RejectedResources == "" {
		snapshot.DistributionPolicy.RejectedResources = config.RejectedResourcesBlock
	}
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
		switch {
		case m.RegistryID != "" || m.Registry != nil || (m.LicensePolicy == nil && m.DistributionPolicy == nil):
			return false, invalid("", "A policy change carries policy fields, not a registry.")
		case m.LicensePolicy != nil && !validPolicy(*m.LicensePolicy, false):
			return false, invalid("license_policy", "The license policy needs a valid undeclared outcome and nonempty SPDX identifiers.")
		case m.LicensePolicy != nil && len(m.LicensePolicy.Allowlist) == 0 && !m.ConfirmUnrestricted:
			return false, invalid("license_policy", "An empty allowlist disables license enforcement; confirm the unrestricted policy first.")
		case m.DistributionPolicy != nil && !validRejectedResources(m.DistributionPolicy.RejectedResources, false):
			return false, invalid("distribution_policy.rejected_resources", "Choose block or expose for rejected resources.")
		case m.DistributionPolicy != nil && m.DistributionPolicy.RejectedResources == config.RejectedResourcesExpose && snapshot.DistributionPolicy.RejectedResources != config.RejectedResourcesExpose && !m.ConfirmRejectedResources:
			return false, invalid("distribution_policy.rejected_resources", "Confirm that rejected resources from every inheriting registry may become installable.")
		}
		changed := false
		reindex := false
		if m.LicensePolicy != nil && !reflect.DeepEqual(snapshot.LicensePolicy, *m.LicensePolicy) {
			snapshot.LicensePolicy = *m.LicensePolicy
			changed = true
			reindex = true
		}
		if m.DistributionPolicy != nil && snapshot.DistributionPolicy != *m.DistributionPolicy {
			snapshot.DistributionPolicy = *m.DistributionPolicy
			changed = true
		}
		if changed {
			snapshot.PolicyRevision++
		}
		return reindex && slices.ContainsFunc(snapshot.Registries, func(r Registry) bool { return r.Active() }), nil
	}
	if m.RegistryID == "" {
		return false, invalid("id", "A registry ID is required.")
	}
	if m.LicensePolicy != nil || m.DistributionPolicy != nil {
		return false, invalid("", "A registry operation cannot change global policy.")
	}
	index := slices.IndexFunc(snapshot.Registries, func(r Registry) bool { return r.ID == m.RegistryID })
	if m.Action == "upsert_registry" {
		if m.Registry == nil || m.Registry.ID != m.RegistryID {
			return false, invalid("id", "The registry draft must carry the same ID as the operation.")
		}
		r := *m.Registry
		if index < 0 {
			if len(snapshot.Registries) >= 10000 {
				return false, invalid("", "This Marketplace already holds the maximum number of registries.")
			}
			if r.Generation != 0 || r.Paused || r.Withdrawn || r.Retired || (r.Lifecycle != "" && r.Lifecycle != "active") || len(r.Aliases) != 0 || len(r.HistoricalOrigins) != 0 || len(r.NonLicenseOverrides) != 0 {
				return false, invalid("", "A new registry starts active at generation 0 without aliases, historical origins or non-license overrides.")
			}
			r.Generation = 1
			r.Lifecycle = "active"
		} else {
			old := snapshot.Registries[index]
			if r.Generation == 0 {
				return false, invalidf("id", "A registry with ID %q already exists (slug %s); edit that registry instead or choose another ID.", r.ID, old.Slug)
			}
			if r.Generation != old.Generation || r.Paused != old.Paused || r.Withdrawn != old.Withdrawn || r.Retired != old.Retired || r.Lifecycle != old.Lifecycle || !reflect.DeepEqual(r.NonLicenseOverrides, old.NonLicenseOverrides) || !reflect.DeepEqual(r.Aliases, old.Aliases) || !reflect.DeepEqual(r.HistoricalOrigins, old.HistoricalOrigins) {
				return false, invalid("", "Generation, lifecycle, aliases, historical origins and non-license overrides are read-only here; reload the registry and reapply your edits.")
			}
			if r.Origin != old.Origin && !m.ConfirmOriginMove {
				return false, invalidf("origin", "Moving the origin from %s requires confirming the origin move.", old.Origin)
			}
			if r.Slug != old.Slug {
				r.Aliases = uniqueNames(append(slices.Clone(old.Aliases), old.Slug), "")
			}
			if r.Origin != old.Origin {
				r.HistoricalOrigins = uniqueNames(append(slices.Clone(old.HistoricalOrigins), old.Origin), "")
			}
			policyChanged := r.RejectedResources != old.RejectedResources
			withoutPolicy := r
			withoutPolicy.RejectedResources = old.RejectedResources
			if !reflect.DeepEqual(withoutPolicy, old) {
				r.Generation++
			}
			if policyChanged {
				snapshot.PolicyRevision++
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
					return false, invalidf("slug", "%q is already used as a slug or alias by registry %s.", name, other.ID)
				}
			}
		}
		if index < 0 {
			snapshot.Registries = append(snapshot.Registries, r)
			slices.SortFunc(snapshot.Registries, func(a, b Registry) int { return strings.Compare(string(a.ID), string(b.ID)) })
		} else {
			priorGeneration := snapshot.Registries[index].Generation
			snapshot.Registries[index] = r
			return r.Active() && r.Generation != priorGeneration, nil
		}
		return r.Active(), nil
	}
	if m.Registry != nil {
		return false, invalidf("", "The %s action does not accept a registry draft.", m.Action)
	}
	if index < 0 {
		return false, store.ErrNotFound
	}
	r := &snapshot.Registries[index]
	transition := func(allowed bool) error {
		if allowed {
			return nil
		}
		return invalidf("", "Cannot %s registry %s while it is %s.", m.Action, r.ID, r.Lifecycle)
	}
	switch m.Action {
	case "pause":
		if err := transition(r.Active()); err != nil {
			return false, err
		}
		r.Paused = true
	case "resume":
		if err := transition(r.Paused && !r.Withdrawn && !r.Retired); err != nil {
			return false, err
		}
		r.Paused = false
	case "withdraw":
		if err := transition(!r.Withdrawn && !r.Retired); err != nil {
			return false, err
		}
		r.Withdrawn = true
		r.Paused = true
	case "restore":
		if err := transition(r.Withdrawn && !r.Retired); err != nil {
			return false, err
		}
		r.Withdrawn = false
		r.Paused = true
	case "retire":
		if err := transition(r.Withdrawn && !r.Retired); err != nil {
			return false, err
		}
		r.Retired = true
	case "restore_retired":
		if err := transition(r.Retired); err != nil {
			return false, err
		}
		r.Retired = false
		r.Withdrawn = true
		r.Paused = true
	case "reindex":
		if err := transition(r.Active()); err != nil {
			return false, err
		}
		return true, s.validateRegistry(*r)
	default:
		return false, invalidf("", "Unknown administration action %q.", m.Action)
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

func validRejectedResources(value string, inherit bool) bool {
	return (inherit && value == "") || value == config.RejectedResourcesBlock || value == config.RejectedResourcesExpose
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
	if strings.ContainsAny(string(r.ID), "/\\@\r\n\x00 ") || len(r.ID) > 256 {
		return invalid("id", "The registry ID must not contain spaces, slashes, '@' or control characters, and must be at most 256 characters.")
	}
	if !originAllowed(r.Origin, s.Deployment.AllowedOrigins) {
		return invalidf("origin", "The origin must be an HTTPS or SSH URL on one of the allowed authorities: %s.", strings.Join(s.Deployment.AllowedOrigins, ", "))
	}
	if r.Acquisition == "" {
		return invalid("acquisition", "Choose mirror or index-only acquisition.")
	}
	if r.ProfileID != "" {
		p, ok := s.Deployment.Profiles[r.ProfileID]
		if !ok || strings.ContainsAny(r.ProfileID, "/\\\r\n\x00 ") {
			return invalidf("profile_id", "Credential profile %q is missing from the deployment configuration.", r.ProfileID)
		}
		if !originAllowed(r.Origin, p.AllowedOrigins) {
			return invalidf("profile_id", "Profile %q only allows origins on %s.", r.ProfileID, strings.Join(p.AllowedOrigins, ", "))
		}
	}
	if r.LicenseOverride != nil && !validPolicy(*r.LicenseOverride, true) {
		return invalid("license_override", "A license override needs a nonempty allowlist of SPDX identifiers or a valid undeclared outcome.")
	}
	if !validRejectedResources(r.RejectedResources, true) {
		return invalid("rejected_resources", "Choose inherit, block, or expose for rejected resources.")
	}
	source := config.Registry{ID: string(r.ID), Slug: r.Slug, Origin: r.Origin, Ref: r.Ref, Acquisition: string(r.Acquisition), Publisher: config.Publisher{Name: r.Publisher.Name, URL: r.Publisher.URL}, License: r.License}
	for i, root := range r.Roots {
		if strings.ContainsAny(root.Path, "\\\x00\r\n") {
			return invalid(fmt.Sprintf("roots[%d].path", i), "Use forward slashes; backslashes and control characters are not allowed.")
		}
		source.Roots = append(source.Roots, config.RegistryRoot{Path: root.Path, SourceHarness: root.SourceHarness})
	}
	if err := source.Validate(config.Policy{}); err != nil {
		return registryValidationError(err)
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
	normalizeDistributionPolicy(&old)
	m := Mutation{ExpectedRevision: current.Revision, RegistryID: registryID}
	if registryID == "" {
		m.Action = "set_policy"
		m.LicensePolicy = &old.LicensePolicy
		m.DistributionPolicy = &old.DistributionPolicy
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
