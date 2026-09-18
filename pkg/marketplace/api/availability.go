package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/licensepolicy"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type availabilityContextKey struct{}

type availabilityView struct {
	revision       int64
	policyRevision int64
	policy         admin.LicensePolicy
	registries     map[store.RegistryID]admin.Registry
}

type availabilityDecision struct {
	Availability         Availability             `json:"availability"`
	ManagedRevision      int64                    `json:"managed_revision"`
	PolicyRevision       int64                    `json:"policy_revision"`
	RegistryGeneration   int64                    `json:"registry_generation"`
	LicenseOutcome       string                   `json:"license_outcome"`
	LicenseCode          string                   `json:"license_code,omitempty"`
	AssessmentDecision   store.AssessmentDecision `json:"assessment_decision"`
	AdminWithdrawal      bool                     `json:"admin_withdrawal"`
	DeploymentRevocation bool                     `json:"deployment_revocation"`
}

var (
	errAvailabilityUnavailable = errors.New("managed availability unavailable")
	errContentRevoked          = errors.New("content revoked")
	errContentBlocked          = errors.New("content blocked")
)

func availabilityFromContext(ctx context.Context) *availabilityView {
	view, _ := ctx.Value(availabilityContextKey{}).(*availabilityView)
	return view
}

func (s *Server) loadAvailability(ctx context.Context) (*availabilityView, error) {
	if s.revocationErr != nil || s.revocations == nil {
		return nil, errAvailabilityUnavailable
	}
	managed, ok := s.meta.(store.ManagedConfigurationStore)
	if !ok {
		return nil, store.ErrTopology
	}
	change, err := managed.CurrentManagedConfiguration(ctx, s.mpID)
	if err != nil {
		return nil, err
	}
	var snapshot admin.Snapshot
	if change.Marketplace != s.mpID || change.Revision < 1 || json.Unmarshal(change.Snapshot, &snapshot) != nil || snapshot.Version != 1 || snapshot.PolicyRevision < 1 || !validAvailabilityPolicy(snapshot.LicensePolicy, false) {
		return nil, store.ErrManagedStateCorrupt
	}
	view := &availabilityView{revision: change.Revision, policyRevision: snapshot.PolicyRevision, policy: snapshot.LicensePolicy, registries: make(map[store.RegistryID]admin.Registry, len(snapshot.Registries))}
	for _, registry := range snapshot.Registries {
		if registry.ID == "" || registry.Generation < 1 || registry.Acquisition.Validate() != nil {
			return nil, store.ErrManagedStateCorrupt
		}
		if _, exists := view.registries[registry.ID]; exists {
			return nil, store.ErrManagedStateCorrupt
		}
		switch registry.Lifecycle {
		case "active", "paused", "withdrawn", "retired":
		default:
			return nil, store.ErrManagedStateCorrupt
		}
		if registry.LicenseOverride != nil && !validAvailabilityPolicy(*registry.LicenseOverride, true) {
			return nil, store.ErrManagedStateCorrupt
		}
		view.registries[registry.ID] = registry
	}
	return view, nil
}

func validAvailabilityPolicy(policy admin.LicensePolicy, override bool) bool {
	return (override && policy.Undeclared == "") || (policy.Undeclared != "" && config.ResolveUndeclaredLicenseOutcome(policy.Undeclared) == policy.Undeclared)
}

func (s *Server) evaluateAvailability(view *availabilityView, rev store.AssessedResourceRevision) availabilityDecision {
	result := availabilityDecision{Availability: AvailabilityAvailable, AssessmentDecision: rev.Assessment.Decision}
	effective := s.applyEffectiveRevocation(rev)
	result.DeploymentRevocation = effective.Revocation != nil
	if result.DeploymentRevocation {
		result.Availability = AvailabilityRevoked
	}
	if view == nil {
		if s.cfg.Management.Mode == "database" {
			result.Availability = AvailabilityBlocked
		}
		return result
	}
	result.ManagedRevision = view.revision
	result.PolicyRevision = view.policyRevision
	registry, ok := view.registries[rev.Coordinate.Registry]
	if !ok || rev.Coordinate.Marketplace != s.mpID {
		result.Availability = AvailabilityBlocked
		return result
	}
	result.RegistryGeneration = registry.Generation
	result.AdminWithdrawal = managedRegistryWithdrawn(registry)
	policy := view.policy
	if registry.LicenseOverride != nil {
		if len(registry.LicenseOverride.Allowlist) > 0 {
			policy.Allowlist = registry.LicenseOverride.Allowlist
		}
		if registry.LicenseOverride.Undeclared != "" {
			policy.Undeclared = registry.LicenseOverride.Undeclared
		}
	}
	license := licensepolicy.Evaluate(rev.License, policy.Allowlist, policy.Undeclared)
	result.LicenseOutcome = license.Outcome
	result.LicenseCode = license.Code
	switch {
	case result.AdminWithdrawal || result.DeploymentRevocation:
		result.Availability = AvailabilityRevoked
	case license.Outcome == "reject" || rev.Assessment.Decision == store.AssessmentDecisionReject:
		result.Availability = AvailabilityPolicyBlocked
	case rev.Assessment.Decision != store.AssessmentDecisionAllow && rev.Assessment.Decision != store.AssessmentDecisionWarn:
		result.Availability = AvailabilityBlocked
	case registry.Acquisition == store.AcquisitionMirror && rev.Coordinate.Kind != KindCollection && rev.ArchiveDigest == "":
		result.Availability = AvailabilityNoArtifact
	}
	return result
}

func (s *Server) availabilityHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Management.Mode != "database" || r.URL.Path == "/healthz" || (r.URL.Path == "/api/v1/admin" || strings.HasPrefix(r.URL.Path, "/api/v1/admin/")) {
			next.ServeHTTP(w, r)
			return
		}
		view, err := s.loadAvailability(r.Context())
		if err != nil {
			slog.WarnContext(r.Context(), "managed availability read failed", "marketplace", s.mpID)
			w.Header().Set("Cache-Control", "private, no-store")
			writeError(w, http.StatusServiceUnavailable, "availability unavailable")
			return
		}
		wrapped := &availabilityResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r.WithContext(context.WithValue(r.Context(), availabilityContextKey{}, view)))
	})
}

type availabilityResponseWriter struct{ http.ResponseWriter }

func (w *availabilityResponseWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "private, no-cache, max-age=0, must-revalidate")
	w.ResponseWriter.WriteHeader(status)
}
func (w *availabilityResponseWriter) Write(data []byte) (int, error) {
	w.Header().Set("Cache-Control", "private, no-cache, max-age=0, must-revalidate")
	return w.ResponseWriter.Write(data)
}

func (s *Server) availabilityResourceDTO(ctx context.Context, rev store.AssessedResourceRevision) any {
	effective := s.applyEffectiveRevocation(rev)
	if s.cfg.Management.Mode != "database" {
		return effective
	}
	return struct {
		store.AssessedResourceRevision
		CurrentAvailability availabilityDecision `json:"current_availability"`
	}{effective, s.evaluateAvailability(availabilityFromContext(ctx), rev)}
}

func (s *Server) managedResourceAllowed(w http.ResponseWriter, ctx context.Context, rev store.AssessedResourceRevision) bool {
	if s.cfg.Management.Mode != "database" {
		return true
	}
	if availabilityFromContext(ctx) == nil {
		writeError(w, http.StatusServiceUnavailable, "availability unavailable")
		return false
	}
	decision := s.evaluateAvailability(availabilityFromContext(ctx), rev)
	if decision.Availability == AvailabilityAvailable {
		return true
	}
	status := http.StatusForbidden
	if decision.Availability == AvailabilityRevoked {
		status = http.StatusGone
	}
	writeJSON(w, status, struct {
		Error               string               `json:"error"`
		CurrentAvailability availabilityDecision `json:"current_availability"`
	}{"resource unavailable", decision})
	return false
}

func (s *Server) managedOwnerRestriction(ctx context.Context, revisions []store.AssessedResourceRevision) (Availability, error) {
	if s.cfg.Management.Mode != "database" {
		return s.resourceOwnerRestriction(revisions), nil
	}
	view := availabilityFromContext(ctx)
	if view == nil {
		return AvailabilityBlocked, errAvailabilityUnavailable
	}
	if len(revisions) == 0 {
		return AvailabilityBlocked, store.ErrNotFound
	}
	result := AvailabilityAvailable
	for _, revision := range revisions {
		decision := s.evaluateAvailability(view, revision)
		if decision.Availability == AvailabilityRevoked {
			return AvailabilityRevoked, nil
		}
		if decision.Availability != AvailabilityAvailable {
			result = AvailabilityBlocked
		}
	}
	return result, nil
}

func writeAvailabilityRestriction(w http.ResponseWriter, restriction Availability, err error) bool {
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "content not found")
		} else {
			writeError(w, http.StatusServiceUnavailable, "availability unavailable")
		}
		return false
	}
	if restriction == AvailabilityAvailable {
		return true
	}
	status := http.StatusForbidden
	if restriction == AvailabilityRevoked {
		status = http.StatusGone
	}
	writeError(w, status, "content unavailable")
	return false
}

type blobOwnerStore interface {
	ListBlobOwners(context.Context, store.MarketplaceID, string, store.ArchiveDigest) ([]store.AssessedResourceRevision, []store.RegistryID, error)
}

func managedRegistryWithdrawn(registry admin.Registry) bool {
	return registry.Withdrawn || registry.Retired || registry.Lifecycle == "withdrawn" || registry.Lifecycle == "retired"
}

func (s *Server) managedRegistryRestriction(ctx context.Context, id store.RegistryID) (Availability, error) {
	view := availabilityFromContext(ctx)
	if view == nil {
		return AvailabilityBlocked, errAvailabilityUnavailable
	}
	registry, ok := view.registries[id]
	if !ok {
		return AvailabilityBlocked, store.ErrNotFound
	}
	probe := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: s.mpID, Registry: id}}}
	if managedRegistryWithdrawn(registry) || s.applyEffectiveRevocation(probe).Revocation != nil {
		return AvailabilityRevoked, nil
	}
	return AvailabilityAvailable, nil
}

func (s *Server) managedDigestAllowed(w http.ResponseWriter, ctx context.Context, kind string, digest store.ArchiveDigest) bool {
	if s.cfg.Management.Mode != "database" {
		return true
	}
	owners, ok := s.meta.(blobOwnerStore)
	if !ok {
		return writeAvailabilityRestriction(w, AvailabilityBlocked, store.ErrTopology)
	}
	revisions, registries, err := owners.ListBlobOwners(ctx, s.mpID, kind, digest)
	if err != nil {
		return writeAvailabilityRestriction(w, AvailabilityBlocked, err)
	}
	if len(revisions) == 0 && len(registries) == 0 {
		return writeAvailabilityRestriction(w, AvailabilityBlocked, store.ErrNotFound)
	}
	for _, registry := range registries {
		restriction, err := s.managedRegistryRestriction(ctx, registry)
		if !writeAvailabilityRestriction(w, restriction, err) {
			return false
		}
	}
	if len(revisions) > 0 {
		restriction, err := s.managedOwnerRestriction(ctx, revisions)
		return writeAvailabilityRestriction(w, restriction, err)
	}
	if kind != "manifest" {
		return writeAvailabilityRestriction(w, AvailabilityBlocked, store.ErrNotFound)
	}
	return true
}

func (s *Server) availabilityETag(ctx context.Context, version store.CatalogVersion) CatalogETag {
	base := s.catalogETag(version)
	if view := availabilityFromContext(ctx); view != nil {
		return CatalogETag(fmt.Sprintf("%s-managed-%d\"", strings.TrimSuffix(string(base), "\""), view.revision))
	}
	return base
}
