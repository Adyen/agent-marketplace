package lockfile

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestMinimalDocumentsGolden(t *testing.T) {
	for _, scope := range []Scope{ScopeOrganization, ScopeUser, ScopeRepo, ScopeLocal} {
		t.Run(string(scope), func(t *testing.T) {
			got, err := Encode(Document{APIVersion: APIVersionV1, Scope: scope})
			if err != nil {
				t.Fatal(err)
			}
			want := "apiVersion: agent-marketplace-lock/v1\nscope: " + string(scope) + "\n"
			if string(got) != want {
				t.Fatalf("bytes:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDecodeIntoDoesNotOverwriteOnFailure(t *testing.T) {
	before := Document{APIVersion: APIVersionV1, Scope: ScopeUser}
	destination := before
	if err := DecodeInto([]byte("apiVersion: broken\nscope: repo\n"), &destination); err == nil {
		t.Fatal("DecodeInto succeeded")
	}
	if !reflect.DeepEqual(destination, before) {
		t.Fatalf("destination overwritten: %#v", destination)
	}
}

func TestMarketplaceCollectionAndAcquisitionInvariants(t *testing.T) {
	digest := Digest(sha256a)
	valid := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{{
		URL: "https://MARKET.example/", InstanceID: "mp_1",
		Collections: []MarketplaceCollection{{SourceID: "sets", Name: "base", Revision: digest, Direct: true, Members: []CollectionEdge{{SourceID: "source", Kind: "skill", Name: "review", Revision: digest}}}},
		Resources:   []MarketplaceResource{{SourceID: "source", Kind: "skill", Name: "review", Revision: digest, Acquisition: AcquisitionMirror, ArchiveDigest: digest, SelectedBy: []CollectionEdge{{SourceID: "sets", Name: "base", Revision: digest}}}},
	}}}
	if _, err := Normalize(valid); err != nil {
		t.Fatalf("valid graph: %v", err)
	}
	invalid := cloneDocument(valid)
	invalid.Marketplaces[0].Resources[0].SelectedBy = nil
	if err := Validate(invalid); err == nil {
		t.Fatal("unselected non-direct resource accepted")
	}
	invalid = cloneDocument(valid)
	invalid.Marketplaces[0].Resources[0].SourceCommit = mustOID(t, sha1a)
	if err := Validate(invalid); err == nil {
		t.Fatal("mirror source occurrence accepted")
	}
}

func TestRegistryInputPermutationCannotChangeBytes(t *testing.T) {
	base := make([]GitRegistry, 24)
	for i := range base {
		base[i] = GitRegistry{URL: "https://example.com/repo-" + string(rune('a'+i)) + ".git", Ref: "main", Commit: mustOID(t, sha1a)}
	}
	want, err := Encode(Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: base})
	if err != nil {
		t.Fatal(err)
	}
	random := rand.New(rand.NewSource(7))
	for i := 0; i < 50; i++ {
		permuted := append([]GitRegistry(nil), base...)
		random.Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
		got, err := Encode(Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: permuted})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("permutation %d changed bytes", i)
		}
	}
}

func TestHTTPUsernameIsAuthenticationNotIdentity(t *testing.T) {
	without, err := CanonicalGitURL("https://example.com/team/repo")
	if err != nil {
		t.Fatal(err)
	}
	with, err := CanonicalGitURL("https://someone@example.com/team/repo")
	if err != nil {
		t.Fatal(err)
	}
	if with != without || strings.Contains(with, "someone") {
		t.Fatalf("identities differ or leaked username: %q, %q", with, without)
	}
}
