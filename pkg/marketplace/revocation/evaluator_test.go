package revocation

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func assessedRevision(marketplace, source, kind, name string, digest store.RevisionDigest) store.AssessedResourceRevision {
	return store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(marketplace), Source: store.SourceID(source), Kind: kind, Name: name},
		ContentDigest: digest,
	}}
}

func TestEvaluatorMatchesExactCoordinateAndDigest(t *testing.T) {
	digest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	evaluator, err := New(config.Config{Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + digest.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	matching := assessedRevision("mp", "src", "skill", "review", digest)
	if evaluator.Effective(matching) == nil {
		t.Fatal("exact live declaration did not revoke matching revision")
	}
	for name, revision := range map[string]store.AssessedResourceRevision{
		"other marketplace": assessedRevision("other", "src", "skill", "review", digest),
		"other source":      assessedRevision("mp", "other", "skill", "review", digest),
		"other kind":        assessedRevision("mp", "src", "command", "review", digest),
		"other name":        assessedRevision("mp", "src", "skill", "other", digest),
		"other digest":      assessedRevision("mp", "src", "skill", "review", store.RevisionDigest("sha256:"+strings.Repeat("b", 64))),
	} {
		if evaluator.Effective(revision) != nil {
			t.Errorf("%s unexpectedly revoked", name)
		}
	}
}

func TestEvaluatorApplyOverlaysProjectionWithoutMutatingIntrinsicRevision(t *testing.T) {
	digest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	revision := assessedRevision("mp", "src", "skill", "review", digest)
	revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: "stale"}
	evaluator, err := New(config.Config{Revocations: config.Revocations{Revisions: []string{"mp/src/skill/review@" + digest.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	got := evaluator.Apply(revision)
	if got.Revocation == nil || got.Revocation.TargetID != "mp/src/skill/review@"+digest.String() {
		t.Fatalf("effective revocation = %#v, want live exact declaration", got.Revocation)
	}
	if revision.ResourceRevision != got.ResourceRevision {
		t.Fatal("Apply mutated intrinsic resource revision")
	}

	clear, err := New(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := clear.Apply(revision).Revocation; got != nil {
		t.Fatalf("stale projection revocation remained effective: %#v", got)
	}
}

func TestEvaluatorRejectsInvalidDeclarations(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for name, cfg := range map[string]config.Config{
		"digest only":           {Revocations: config.Revocations{Revisions: []string{digest}}},
		"different marketplace": {Instance: config.Instance{ID: "configured-mp"}, Revocations: config.Revocations{Revisions: []string{"other-mp/src/skill/review@" + digest}}},
		"unsupported kind":      {Revocations: config.Revocations{Revisions: []string{"mp/src/rule/review@" + digest}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg); err == nil {
				t.Fatal("invalid declaration unexpectedly accepted")
			}
		})
	}
}

func TestEvaluatorSpecificityAndDeclarations(t *testing.T) {
	digestA := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	digestB := store.RevisionDigest("sha256:" + strings.Repeat("b", 64))
	evaluator, err := New(config.Config{Instance: config.Instance{ID: "mp"}, Revocations: config.Revocations{
		SourceIDs: []string{"src"}, Coordinates: []string{"mp/src/skill/review"}, Revisions: []string{"mp/src/skill/review@" + digestA.String()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		rev  store.AssessedResourceRevision
		want store.RevocationKind
	}{
		{"exact", assessedRevision("mp", "src", "skill", "review", digestA), store.RevocationRevision},
		{"coordinate", assessedRevision("mp", "src", "skill", "review", digestB), store.RevocationCoordinate},
		{"source", assessedRevision("mp", "src", "command", "other", digestB), store.RevocationSource},
		{"unrelated", assessedRevision("mp", "other", "skill", "review", digestB), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := evaluator.Effective(test.rev)
			if test.want == "" {
				if got != nil {
					t.Fatalf("effective revocation = %#v, want nil", got)
				}
				return
			}
			if got == nil || got.Kind != test.want {
				t.Fatalf("effective revocation = %#v, want %s", got, test.want)
			}
		})
	}
	if declarations := evaluator.Declarations(); len(declarations) != 3 {
		t.Fatalf("declarations = %d, want 3", len(declarations))
	}
}
