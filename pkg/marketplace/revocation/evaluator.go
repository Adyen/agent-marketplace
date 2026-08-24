// Package revocation computes the effective control-plane revocation for an
// immutable Marketplace resource revision.
package revocation

import (
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Evaluator is an invocation-independent view of the operator's live exact
// revision declarations. It overlays declarations on immutable metadata at
// read time; it never mutates a stored revision.
type Evaluator struct {
	exact   map[string]store.Revocation
	targets []store.RevisionRevocationTarget
}

// New builds an evaluator from the live operator configuration. Revision
// declarations must identify both the complete coordinate and content digest.
func New(cfg config.Config) (*Evaluator, error) {
	e := &Evaluator{exact: make(map[string]store.Revocation, len(cfg.Revocations.Revisions))}
	for _, raw := range cfg.Revocations.Revisions {
		target, err := store.ParseRevisionRevocationTarget(raw)
		if err != nil {
			return nil, err
		}
		e.exact[target.String()] = store.Revocation{
			Kind:     store.RevocationRevision,
			TargetID: target.String(),
			Reason:   "operator configured",
		}
		e.targets = append(e.targets, target)
	}
	return e, nil
}

// Targets returns the exact live declarations. The returned slice is a copy.
func (e *Evaluator) Targets() []store.RevisionRevocationTarget {
	if e == nil || len(e.targets) == 0 {
		return nil
	}
	return append([]store.RevisionRevocationTarget(nil), e.targets...)
}

// Effective returns the live effective revocation for rev. A live exact
// declaration is authoritative; embedded metadata is deliberately ignored so
// a stale historical field cannot keep a withdrawn declaration active or
// weaken a current one.
func (e *Evaluator) Effective(rev store.ResourceRevision) *store.Revocation {
	if e == nil {
		return nil
	}
	key := ExactIdentity(rev.Coordinate, rev.ContentDigest)
	active, ok := e.exact[key]
	if !ok {
		return nil
	}
	result := active
	return &result
}

// Apply returns a copy of rev with its effective revocation overlaid.
func (e *Evaluator) Apply(rev store.ResourceRevision) store.ResourceRevision {
	rev.Revocation = e.Effective(rev)
	return rev
}

// IsRevoked reports whether rev is effectively revoked.
func (e *Evaluator) IsRevoked(rev store.ResourceRevision) bool {
	return e.Effective(rev) != nil
}

// ExactIdentity returns the canonical key used by the evaluator.
func ExactIdentity(coordinate store.ResourceCoordinate, digest store.RevisionDigest) string {
	return coordinate.String() + "@" + digest.String()
}
