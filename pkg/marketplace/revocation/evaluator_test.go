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

func TestEvaluatorRejectsDifferentMarketplaceRevisionDeclaration(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	_, err := New(config.Config{
		Instance:    config.Instance{ID: "configured-mp"},
		Revocations: config.Revocations{Revisions: []string{"other-mp/src/skill/review@" + digest}},
	})
	if err == nil || !strings.Contains(err.Error(), "configured instance") {
		t.Fatalf("New error = %v, want configured Marketplace mismatch", err)
	}
}

func TestEvaluatorRejectsUnsupportedRevisionKind(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	_, err := New(config.Config{Revocations: config.Revocations{Revisions: []string{"mp/src/rule/review@" + digest}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported resource kind") {
		t.Fatalf("New error = %v, want unsupported resource kind", err)
	}
}

func TestEvaluatorSpecificityMatrixBroaderDeclarationsStillRevoke(t *testing.T) {
	digestA := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	digestB := store.RevisionDigest("sha256:" + strings.Repeat("b", 64))
	cfg := config.Config{Instance: config.Instance{ID: "mp"}, Revocations: config.Revocations{
		SourceIDs:   []string{"src"},
		Coordinates: []string{"mp/src/skill/review"},
		Revisions:   []string{"mp/src/skill/review@" + digestA.String()},
	}}
	evaluator, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"}
	for _, test := range []struct {
		name, marketplace, digest, kind, source, resourceName, wantKind string
	}{
		{"exact", "mp", digestA.String(), "skill", "src", "review", string(store.RevocationRevision)},
		{"coordinate other revision", "mp", digestB.String(), "skill", "src", "review", string(store.RevocationCoordinate)},
		{"source other coordinate", "mp", digestB.String(), "command", "src", "other", string(store.RevocationSource)},
		{"unrelated source", "mp", digestB.String(), "skill", "other", "review", ""},
		{"other marketplace source", "other", digestB.String(), "skill", "src", "review", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(test.digest)}
			rev.Coordinate.Marketplace = store.MarketplaceID(test.marketplace)
			rev.Coordinate.Source = store.SourceID(test.source)
			rev.Coordinate.Kind = test.kind
			rev.Coordinate.Name = test.resourceName
			got := evaluator.Effective(rev)
			if test.wantKind == "" {
				if got != nil {
					t.Fatalf("effective revocation = %#v, want nil", got)
				}
				return
			}
			if got == nil || string(got.Kind) != test.wantKind {
				t.Fatalf("effective revocation = %#v, want kind %s", got, test.wantKind)
			}
		})
	}
}

func TestEvaluatorDeclarationsContainsCompleteDesiredSet(t *testing.T) {
	evaluator, err := New(config.Config{Instance: config.Instance{ID: "mp"}, Revocations: config.Revocations{
		SourceIDs: []string{"src"}, Coordinates: []string{"mp/src/skill/review"},
		Revisions: []string{"mp/src/skill/review@sha256:" + strings.Repeat("a", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	declarations := evaluator.Declarations()
	if len(declarations) != 3 {
		t.Fatalf("declarations = %d, want 3: %#v", len(declarations), declarations)
	}
}
