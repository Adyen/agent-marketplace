// Package revocation computes the effective control-plane revocation for an
// immutable Marketplace resource revision.
package revocation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Evaluator is an invocation-independent view of the operator's live
// revocation declarations. It overlays declarations on immutable metadata at
// read time; it never mutates a stored revision.
type Evaluator struct {
	marketplace  store.MarketplaceID
	source       map[string]store.Revocation
	coordinate   map[string]store.Revocation
	exact        map[string]store.Revocation
	targets      []store.RevisionRevocationTarget
	declarations []store.Revocation
}

// New builds an evaluator from the live operator configuration. Declarations
// may target a whole source, a resource coordinate, or one exact revision.
// Specificity is ordered exact > coordinate > source.
func New(cfg config.Config) (*Evaluator, error) {
	e := &Evaluator{
		marketplace: store.MarketplaceID(strings.TrimSpace(cfg.Instance.ID)),
		source:      make(map[string]store.Revocation, len(cfg.Revocations.SourceIDs)),
		coordinate:  make(map[string]store.Revocation, len(cfg.Revocations.Coordinates)),
		exact:       make(map[string]store.Revocation, len(cfg.Revocations.Revisions)),
	}
	seenDeclarations := make(map[string]struct{})
	add := func(kind store.RevocationKind, target string) {
		r := store.Revocation{Kind: kind, TargetID: target, Reason: "operator configured"}
		if _, seen := seenDeclarations[declarationKey(r)]; seen {
			return
		}
		seenDeclarations[declarationKey(r)] = struct{}{}
		e.declarations = append(e.declarations, r)
	}
	for _, raw := range cfg.Revocations.SourceIDs {
		target := strings.TrimSpace(raw)
		if err := validateToken("source", target); err != nil {
			return nil, fmt.Errorf("source revocation %q: %w", raw, err)
		}
		r := store.Revocation{Kind: store.RevocationSource, TargetID: target, Reason: "operator configured"}
		e.source[target] = r
		add(r.Kind, r.TargetID)
	}
	for _, raw := range cfg.Revocations.Coordinates {
		target, err := parseCoordinateTarget(raw, strings.TrimSpace(cfg.Instance.ID))
		if err != nil {
			return nil, fmt.Errorf("coordinate revocation %q: %w", raw, err)
		}
		r := store.Revocation{Kind: store.RevocationCoordinate, TargetID: target.String(), Reason: "operator configured"}
		e.coordinate[target.String()] = r
		add(r.Kind, r.TargetID)
	}
	seenExactTargets := make(map[string]struct{}, len(cfg.Revocations.Revisions))
	for _, raw := range cfg.Revocations.Revisions {
		target, err := store.ParseRevisionRevocationTarget(raw)
		if err != nil {
			return nil, err
		}
		if configured := strings.TrimSpace(cfg.Instance.ID); configured != "" && string(target.Coordinate.Marketplace) != configured {
			return nil, fmt.Errorf("revision revocation %q targets Marketplace %q, configured instance is %q", raw, target.Coordinate.Marketplace, configured)
		}
		r := store.Revocation{Kind: store.RevocationRevision, TargetID: target.String(), Reason: "operator configured"}
		e.exact[target.String()] = r
		if _, seen := seenExactTargets[target.String()]; !seen {
			seenExactTargets[target.String()] = struct{}{}
			e.targets = append(e.targets, target)
		}
		add(r.Kind, r.TargetID)
	}
	// A repeated declaration is one active declaration, not multiple rows. Keep
	// the set deterministic for reconciliation and readiness comparisons.
	sort.Slice(e.declarations, func(i, j int) bool { return declarationKey(e.declarations[i]) < declarationKey(e.declarations[j]) })
	return e, nil
}

func validateToken(label, value string) error {
	if value == "" || strings.ContainsAny(value, "/@") {
		return fmt.Errorf("%s must be a non-empty route token", label)
	}
	return nil
}

func parseCoordinateTarget(raw, instanceID string) (store.ResourceCoordinate, error) {
	coordinate, err := store.ParseResourceCoordinate(strings.TrimSpace(raw))
	if err != nil {
		return store.ResourceCoordinate{}, err
	}
	for label, value := range map[string]string{
		"marketplace": string(coordinate.Marketplace), "source": string(coordinate.Source),
		"kind": coordinate.Kind, "name": coordinate.Name,
	} {
		if err := validateToken(label, value); err != nil {
			return store.ResourceCoordinate{}, err
		}
	}
	if !store.IsMarketplaceResourceKind(coordinate.Kind) {
		return store.ResourceCoordinate{}, fmt.Errorf("unsupported resource kind %q", coordinate.Kind)
	}
	if instanceID != "" && string(coordinate.Marketplace) != instanceID {
		return store.ResourceCoordinate{}, fmt.Errorf("marketplace %q does not match instance.id %q", coordinate.Marketplace, instanceID)
	}
	return coordinate, nil
}

func declarationKey(r store.Revocation) string { return string(r.Kind) + "\x00" + r.TargetID }

// Declarations returns the complete live desired set. The returned slice is a
// copy and contains no AppliedAt value, since timestamps are store history.
func (e *Evaluator) Declarations() []store.Revocation {
	if e == nil || len(e.declarations) == 0 {
		return nil
	}
	return append([]store.Revocation(nil), e.declarations...)
}

// Targets returns the exact live declarations. It is retained for callers that
// need exact revision targets (for example collection manifest lookup).
func (e *Evaluator) Targets() []store.RevisionRevocationTarget {
	if e == nil || len(e.targets) == 0 {
		return nil
	}
	return append([]store.RevisionRevocationTarget(nil), e.targets...)
}

// Effective returns the live effective revocation for rev. A broader
// declaration never exempts a revision; specificity only selects the primary
// reason reported to callers.
func (e *Evaluator) Effective(rev store.ResourceRevision) *store.Revocation {
	if e == nil {
		return nil
	}
	if active, ok := e.exact[ExactIdentity(rev.Coordinate, rev.ContentDigest)]; ok {
		result := active
		return &result
	}
	if active, ok := e.coordinate[rev.Coordinate.String()]; ok {
		result := active
		return &result
	}
	if e.marketplace == "" || rev.Coordinate.Marketplace == e.marketplace {
		if active, ok := e.source[string(rev.Coordinate.Source)]; ok {
			result := active
			return &result
		}
	}
	return nil
}

// Apply returns a copy of rev with its effective revocation overlaid.
func (e *Evaluator) Apply(rev store.ResourceRevision) store.ResourceRevision {
	rev.Revocation = e.Effective(rev)
	return rev
}

// IsRevoked reports whether rev is effectively revoked.
func (e *Evaluator) IsRevoked(rev store.ResourceRevision) bool { return e.Effective(rev) != nil }

// ExactIdentity returns the canonical key used by the evaluator.
func ExactIdentity(coordinate store.ResourceCoordinate, digest store.RevisionDigest) string {
	return coordinate.String() + "@" + digest.String()
}
