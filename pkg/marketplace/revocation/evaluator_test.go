package revocation

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestEvaluatorMatchesExactCoordinateAndDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	cfg := config.Config{Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + digest}}}
	evaluator, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	matching := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(digest)}
	if evaluator.Effective(matching) == nil {
		t.Fatal("exact live declaration did not revoke matching revision")
	}
	for name, revision := range map[string]store.ResourceRevision{
		"other marketplace": {Coordinate: store.ResourceCoordinate{Marketplace: "other", Source: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(digest)},
		"other source":      {Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "other", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(digest)},
		"other kind":        {Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "command", Name: "review"}, ContentDigest: store.RevisionDigest(digest)},
		"other name":        {Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "other"}, ContentDigest: store.RevisionDigest(digest)},
		"other digest":      {Coordinate: matching.Coordinate, ContentDigest: store.RevisionDigest("sha256:" + "b" + "0" + "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")},
	} {
		if evaluator.Effective(revision) != nil {
			t.Errorf("%s unexpectedly revoked", name)
		}
	}
}

func TestEvaluatorLiveDeclarationOverridesStaleEmbeddedState(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		Revocation:    &store.Revocation{Kind: store.RevocationRevision, TargetID: "stale", Reason: "stale metadata"},
	}
	evaluator, err := New(config.Config{Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + digest}}})
	if err != nil {
		t.Fatal(err)
	}
	got := evaluator.Apply(revision)
	if got.Revocation == nil || got.Revocation.TargetID != "mp/src/skill/review@"+digest {
		t.Fatalf("effective revocation = %#v, want live exact declaration", got.Revocation)
	}
}

func TestEvaluatorRemovalIgnoresStaleEmbeddedRevocation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
		Revocation:    &store.Revocation{Kind: store.RevocationRevision, TargetID: "old-declaration"},
	}
	evaluator, err := New(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluator.Apply(revision).Revocation; got != nil {
		t.Fatalf("stale embedded revocation remained effective: %#v", got)
	}
}

func TestEvaluatorRejectsDigestOnlyRevisionDeclaration(t *testing.T) {
	if _, err := New(config.Config{Revocations: config.Revocations{Revisions: []string{"sha256:" + "a" + "0" + "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}); err == nil {
		t.Fatal("digest-only declaration unexpectedly accepted")
	}
}
