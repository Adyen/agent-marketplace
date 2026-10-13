package lockfile

import (
	"strings"
	"testing"
)

func requiresLockDocument(t *testing.T, mutate func(*MarketplaceResourceLock)) Document {
	t.Helper()
	doc := Document{
		APIVersion: APIVersionV2,
		Scope:      ScopeRepo,
		Marketplaces: []MarketplaceLock{{
			URL:        "https://market.example",
			InstanceID: "market-id",
			RegistryBindings: []MarketplaceRegistryBinding{
				{Name: "tools", RegistryID: "tools-id"},
			},
			Resources: []MarketplaceResourceLock{
				{
					RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: "review",
					Revision: "rev-a", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b),
					Acquisition: MarketplaceAcquisitionMirror,
				},
				{
					RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: "lint",
					Revision: "rev-c", ContentDigest: Digest(sha256c), ArchiveDigest: Digest(sha256d),
					Acquisition: MarketplaceAcquisitionMirror,
				},
			},
		}},
	}
	if mutate != nil {
		mutate(&doc.Marketplaces[0].Resources[1])
	}
	return doc
}

// spec: acquisition-lock-codec/requires-edge-round-trips-canonically
func TestRequiresEdgeRoundTripsCanonically(t *testing.T) {
	edge := MarketplaceSelectionLock{RegistryID: "tools-id", Kind: "skill", Name: "review", Revision: Digest(sha256a)}
	doc := requiresLockDocument(t, func(r *MarketplaceResourceLock) {
		r.SelectedBy = []MarketplaceSelectionLock{edge, edge}
	})
	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatal(err)
	}
	var lint *MarketplaceResourceLock
	for i := range normalized.Marketplaces[0].Resources {
		if normalized.Marketplaces[0].Resources[i].Name == "lint" {
			lint = &normalized.Marketplaces[0].Resources[i]
		}
	}
	if lint == nil {
		t.Fatal("locked lint resource missing")
	}
	selectedBy := lint.SelectedBy
	if len(selectedBy) != 1 || selectedBy[0] != edge {
		t.Fatalf("selected_by = %#v, want %#v", selectedBy, []MarketplaceSelectionLock{edge})
	}
	encoded, err := Encode(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "kind: skill") {
		t.Fatalf("requires edge kind missing in emitted bytes:\n%s", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var roundTripped []MarketplaceSelectionLock
	for _, r := range decoded.Marketplaces[0].Resources {
		if r.Name == "lint" {
			roundTripped = r.SelectedBy
		}
	}
	if len(roundTripped) != 1 || roundTripped[0] != edge {
		t.Fatalf("round-tripped selected_by = %#v", roundTripped)
	}
}

// spec: acquisition-lock-codec/collection-only-lock-re-encodes-byte-identically
func TestCollectionOnlyLockReencodesByteIdentically(t *testing.T) {
	doc := requiresLockDocument(t, nil)
	encoded, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(encoded) {
		t.Fatalf("re-encoded bytes differ:\n%s\n---\n%s", encoded, reencoded)
	}
}

// spec: acquisition-lock-codec/malformed-requires-edges-are-rejected
func TestMalformedRequiresEdgesAreRejected(t *testing.T) {
	edge := func(f func(*MarketplaceSelectionLock)) func(*MarketplaceResourceLock) {
		return func(r *MarketplaceResourceLock) {
			e := MarketplaceSelectionLock{RegistryID: "tools-id", Kind: "skill", Name: "review", Revision: Digest(sha256a)}
			f(&e)
			r.SelectedBy = []MarketplaceSelectionLock{e}
		}
	}
	cases := map[string]func(*MarketplaceResourceLock){
		"explicit collection kind": edge(func(e *MarketplaceSelectionLock) { e.Kind = "collection" }),
		"unknown kind":             edge(func(e *MarketplaceSelectionLock) { e.Kind = "rule" }),
		"missing parent":           edge(func(e *MarketplaceSelectionLock) { e.Name = "absent" }),
		"mismatched digest":        edge(func(e *MarketplaceSelectionLock) { e.Revision = Digest(sha256c) }),
		"self reference": func(r *MarketplaceResourceLock) {
			r.SelectedBy = []MarketplaceSelectionLock{{RegistryID: "tools-id", Kind: "skill", Name: "lint", Revision: Digest(sha256c)}}
		},
		"unbound registry": edge(func(e *MarketplaceSelectionLock) { e.RegistryID = "unbound-id" }),
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := requiresLockDocument(t, mutate)
			if _, err := Normalize(doc); err == nil {
				t.Fatalf("Normalize(%s) succeeded, want invalid document", name)
			}
		})
	}
}
