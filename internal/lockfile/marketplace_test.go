package lockfile

import (
	"reflect"
	"strings"
	"testing"
)

const (
	sha256b = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sha256c = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	sha256d = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	sha256e = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	sha256f = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	commitA = "0123456789abcdef0123456789abcdef01234567"
)

// v2GoldenDocument intentionally lists bindings, resources, and selections out
// of canonical order so the golden asserts normalization, not input order.
func v2GoldenDocument() Document {
	return Document{
		APIVersion: APIVersionV2,
		Scope:      ScopeRepo,
		Registries: []GitRegistry{{
			URL: "https://git.example.com/team/tools.git", Ref: "main", Commit: mustOIDForGolden(sha1a),
			Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a), Resources: []GitResource{
				{Kind: "skill", Name: "zeta", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b)},
			}}},
		}},
		Marketplaces: []MarketplaceLock{{
			URL:        "https://MARKET.example/",
			InstanceID: "market-id",
			RegistryBindings: []MarketplaceRegistryBinding{
				{Name: "tools", RegistryID: "tools-id"},
				{Name: "collections", RegistryID: "collections-id"},
				{Name: "tools-alias", RegistryID: "tools-id"},
			},
			Resources: []MarketplaceResourceLock{
				{
					RegistryName: "tools-alias", RegistryID: "tools-id", Kind: "skill", Name: "review",
					Revision: "rev-a", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b),
					Acquisition: MarketplaceAcquisitionMirror,
					SelectedBy: []MarketplaceSelectionLock{
						{RegistryID: "collections-id", Name: "onboarding", Revision: Digest(sha256c)},
						{RegistryID: "collections-id", Name: "onboarding", Revision: Digest(sha256c)},
					},
				},
				{
					RegistryName: "collections", RegistryID: "collections-id", Kind: "collection", Name: "onboarding",
					Revision: "rev-c", ContentDigest: Digest(sha256c),
					CollectionManifestDigest: Digest(sha256c), Acquisition: MarketplaceAcquisitionMirror,
				},
				{
					RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: "index-skill",
					Revision: "rev-e", ContentDigest: Digest(sha256e), ArchiveDigest: Digest(sha256f),
					Acquisition:    MarketplaceAcquisitionIndexOnly,
					RegistryCommit: commitA, RegistryManifestDigest: Digest(sha256d),
					SourcePath: "skills/./index",
				},
			},
		}},
	}
}

const v2GoldenBytes = `apiVersion: agent-marketplace-lock/v2
scope: repo
registries:
  - url: https://git.example.com/team/tools
    ref: main
    commit: sha1:0123456789abcdef0123456789abcdef01234567
    roots:
      - path: skills
        source_harness: claude
        revision_schema: v1
        revision_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        resources:
          - kind: skill
            name: zeta
            content_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
marketplaces:
  - url: https://market.example
    instance_id: market-id
    registry_bindings:
      - name: collections
        registry_id: collections-id
      - name: tools
        registry_id: tools-id
      - name: tools-alias
        registry_id: tools-id
    resources:
      - registry_name: collections
        registry_id: collections-id
        kind: collection
        name: onboarding
        revision: rev-c
        content_digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
        collection_manifest_digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
        acquisition: mirror
      - registry_name: tools
        registry_id: tools-id
        kind: skill
        name: index-skill
        revision: rev-e
        content_digest: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
        archive_digest: sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
        acquisition: index-only
        registry_commit: 0123456789abcdef0123456789abcdef01234567
        registry_manifest_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        source_path: skills/index
      - registry_name: tools-alias
        registry_id: tools-id
        kind: skill
        name: review
        revision: rev-a
        content_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        acquisition: mirror
        selected_by:
          - registry_id: collections-id
            name: onboarding
            revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
`

// spec: acquisition-lock-codec/v2-marketplace-lock-emits-stable-golden-bytes
// spec: acquisition-lock-codec/emitted-bytes-decode-to-the-normalized-document
func TestV2MarketplaceGolden(t *testing.T) {
	got, err := Encode(v2GoldenDocument())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != v2GoldenBytes {
		t.Fatalf("v2 lock bytes:\n%s\nwant:\n%s", got, v2GoldenBytes)
	}
	decoded, err := Decode([]byte(v2GoldenBytes))
	if err != nil {
		t.Fatal(err)
	}
	want, err := Normalize(v2GoldenDocument())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("v2 round trip mismatch:\n%#v\nwant:\n%#v", decoded, want)
	}
}

func validV2Resource() MarketplaceResourceLock {
	return MarketplaceResourceLock{
		RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: "review",
		Revision: "rev-a", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b),
		Acquisition: MarketplaceAcquisitionMirror,
	}
}

func validV2Document() Document {
	return Document{
		APIVersion: APIVersionV2,
		Scope:      ScopeRepo,
		Marketplaces: []MarketplaceLock{{
			URL:        "https://market.example",
			InstanceID: "market-id",
			RegistryBindings: []MarketplaceRegistryBinding{
				{Name: "tools", RegistryID: "tools-id"},
				{Name: "collections", RegistryID: "collections-id"},
			},
			Resources: []MarketplaceResourceLock{
				{
					RegistryName: "collections", RegistryID: "collections-id", Kind: "collection", Name: "onboarding",
					Revision: "rev-c", ContentDigest: Digest(sha256c),
					CollectionManifestDigest: Digest(sha256c), Acquisition: MarketplaceAcquisitionMirror,
				},
				func() MarketplaceResourceLock {
					r := validV2Resource()
					r.SelectedBy = []MarketplaceSelectionLock{
						{RegistryID: "collections-id", Name: "onboarding", Revision: Digest(sha256c)},
					}
					return r
				}(),
			},
		}},
	}
}

func TestMarketplaceInvalidDocuments(t *testing.T) {
	mutate := func(f func(*Document)) Document {
		doc := validV2Document()
		f(&doc)
		return doc
	}
	mutateResource := func(i int, f func(*MarketplaceResourceLock)) func(*Document) {
		return func(d *Document) { f(&d.Marketplaces[0].Resources[i]) }
	}
	cases := map[string]Document{
		"v1 with marketplace records": func() Document {
			doc := validV2Document()
			doc.APIVersion = APIVersionV1
			return doc
		}(),
		"unsafe endpoint": mutate(func(d *Document) {
			d.Marketplaces[0].URL = "https://user@market.example"
		}),
		"endpoint traversal": mutate(func(d *Document) {
			d.Marketplaces[0].URL = "https://market.example/../escape"
		}),
		"empty instance": mutate(func(d *Document) {
			d.Marketplaces[0].InstanceID = " "
		}),
		"unsafe instance segment": mutate(func(d *Document) {
			d.Marketplaces[0].InstanceID = "bad/id"
		}),
		"unsafe binding name": mutate(func(d *Document) {
			d.Marketplaces[0].RegistryBindings[0].Name = "bad\\name"
		}),
		"unsafe binding id": mutate(func(d *Document) {
			d.Marketplaces[0].RegistryBindings[0].RegistryID = "bad id\t"
		}),
		"unsafe resource registry id": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryID = "tools/id"
		})),
		"unsafe resource name": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Name = "a/b"
		})),
		"unsafe resource revision": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Revision = "rev/1"
		})),
		"unsafe selection name": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SelectedBy[0].Name = "bad/name"
		})),
		"empty revision": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Revision = ""
		})),
		"invalid content digest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.ContentDigest = "sha256:zzz"
		})),
		"invalid kind": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Kind = "rule"
		})),
		"invalid acquisition": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = "fetch"
		})),
		"non-collection missing archive digest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.ArchiveDigest = ""
		})),
		"non-collection invalid archive digest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.ArchiveDigest = "sha256:nothex"
		})),
		"non-collection with collection manifest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.CollectionManifestDigest = Digest(sha256d)
		})),
		"mirror with registry commit": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryCommit = commitA
		})),
		"mirror with registry manifest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryManifestDigest = Digest(sha256d)
		})),
		"mirror with source path": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SourcePath = "skills/review"
		})),
		"index-only missing commit": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
			r.RegistryManifestDigest = Digest(sha256d)
			r.SourcePath = "skills/review"
		})),
		"index-only bad commit": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
			r.RegistryCommit = "ABCDEF"
			r.RegistryManifestDigest = Digest(sha256d)
			r.SourcePath = "skills/review"
		})),
		"index-only missing manifest digest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
			r.RegistryCommit = commitA
			r.SourcePath = "skills/review"
		})),
		"index-only missing source path": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
			r.RegistryCommit = commitA
			r.RegistryManifestDigest = Digest(sha256d)
		})),
		"index-only traversal source path": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
			r.RegistryCommit = commitA
			r.RegistryManifestDigest = Digest(sha256d)
			r.SourcePath = "../escape"
		})),
		"collection with archive digest": mutate(mutateResource(0, func(r *MarketplaceResourceLock) {
			r.ArchiveDigest = Digest(sha256b)
		})),
		"collection missing manifest digest": mutate(mutateResource(0, func(r *MarketplaceResourceLock) {
			r.CollectionManifestDigest = ""
		})),
		"collection manifest digest differs from content digest": mutate(mutateResource(0, func(r *MarketplaceResourceLock) {
			r.CollectionManifestDigest = Digest(sha256d)
		})),
		"collection index-only": mutate(mutateResource(0, func(r *MarketplaceResourceLock) {
			r.Acquisition = MarketplaceAcquisitionIndexOnly
		})),
		"missing registry name": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryName = ""
		})),
		"unbound registry name": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryName = "ghost"
		})),
		"registry name bound to different id": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryName = "collections"
		})),
		"resource without binding": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.RegistryID = "unbound-id"
		})),
		"selection without binding": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SelectedBy[0].RegistryID = "unbound-id"
		})),
		"dangling selection name": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SelectedBy[0].Name = "missing"
		})),
		"dangling selection revision": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SelectedBy[0].Revision = Digest(sha256f)
		})),
		"selection to non-collection": mutate(func(d *Document) {
			d.Marketplaces[0].Resources[1].SelectedBy = []MarketplaceSelectionLock{
				{RegistryID: "tools-id", Name: "review", Revision: Digest(sha256a)},
			}
		}),
		"invalid selection revision digest": mutate(mutateResource(1, func(r *MarketplaceResourceLock) {
			r.SelectedBy[0].Revision = "sha256:bad"
		})),
		"endpoint identity conflict": mutate(func(d *Document) {
			dup := d.Marketplaces[0]
			dup.InstanceID = "other-id"
			d.Marketplaces = append(d.Marketplaces, dup)
		}),
		"binding identity conflict": mutate(func(d *Document) {
			d.Marketplaces[0].RegistryBindings = append(d.Marketplaces[0].RegistryBindings,
				MarketplaceRegistryBinding{Name: "tools", RegistryID: "different-id"})
		}),
		"resource identity conflict": mutate(func(d *Document) {
			conflict := validV2Resource()
			conflict.ArchiveDigest = Digest(sha256f)
			d.Marketplaces[0].Resources = append(d.Marketplaces[0].Resources, conflict)
		}),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Normalize(doc); err == nil {
				t.Fatalf("Normalize accepted invalid document: %#v", doc)
			}
			data, err := Encode(doc)
			if err == nil {
				t.Fatalf("Encode accepted invalid document:\n%s", data)
			}
		})
	}
}

// spec: acquisition-lock-codec/marketplace-records-are-ordered-and-exact-duplicates-collapse
// spec: acquisition-lock-codec/selection-edges-are-ordered-and-deduplicated
func TestMarketplaceEndpointAndSelectionOrderingDedup(t *testing.T) {
	edgeOnboarding := MarketplaceSelectionLock{RegistryID: "collections-id", Name: "onboarding", Revision: Digest(sha256c)}
	edgeStarters := MarketplaceSelectionLock{RegistryID: "collections-id", Name: "starters", Revision: Digest(sha256e)}
	primary := MarketplaceLock{
		URL:        "https://market.example",
		InstanceID: "market-id",
		RegistryBindings: []MarketplaceRegistryBinding{
			{Name: "tools", RegistryID: "tools-id"},
			{Name: "collections", RegistryID: "collections-id"},
		},
		Resources: []MarketplaceResourceLock{
			{
				RegistryName: "tools", RegistryID: "tools-id", Kind: "skill", Name: "review",
				Revision: "rev-a", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b),
				Acquisition: MarketplaceAcquisitionMirror,
				SelectedBy:  []MarketplaceSelectionLock{edgeStarters, edgeOnboarding, edgeStarters},
			},
			{
				RegistryName: "collections", RegistryID: "collections-id", Kind: "collection", Name: "starters",
				Revision: "rev-e", ContentDigest: Digest(sha256e),
				CollectionManifestDigest: Digest(sha256e), Acquisition: MarketplaceAcquisitionMirror,
			},
			{
				RegistryName: "collections", RegistryID: "collections-id", Kind: "collection", Name: "onboarding",
				Revision: "rev-c", ContentDigest: Digest(sha256c),
				CollectionManifestDigest: Digest(sha256c), Acquisition: MarketplaceAcquisitionMirror,
			},
		},
	}
	secondary := MarketplaceLock{
		URL:        "https://zeta.example",
		InstanceID: "zeta-id",
		RegistryBindings: []MarketplaceRegistryBinding{
			{Name: "zeta", RegistryID: "zeta-registry-id"},
		},
		Resources: []MarketplaceResourceLock{{
			RegistryName: "zeta", RegistryID: "zeta-registry-id", Kind: "skill", Name: "lone",
			Revision: "rev-b", ContentDigest: Digest(sha256b), ArchiveDigest: Digest(sha256c),
			Acquisition: MarketplaceAcquisitionMirror,
		}},
	}
	duplicatePrimary := primary
	doc := Document{
		APIVersion:   APIVersionV2,
		Scope:        ScopeRepo,
		Marketplaces: []MarketplaceLock{secondary, primary, duplicatePrimary},
	}
	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Marketplaces) != 2 {
		t.Fatalf("endpoint dedupe failed: %#v", normalized.Marketplaces)
	}
	if normalized.Marketplaces[0].URL != "https://market.example" || normalized.Marketplaces[0].InstanceID != "market-id" ||
		normalized.Marketplaces[1].URL != "https://zeta.example" || normalized.Marketplaces[1].InstanceID != "zeta-id" {
		t.Fatalf("endpoint order: %#v", normalized.Marketplaces)
	}
	resources := normalized.Marketplaces[0].Resources
	if len(resources) != 3 || resources[0].Name != "onboarding" || resources[1].Name != "starters" || resources[2].Name != "review" {
		t.Fatalf("resource order: %#v", resources)
	}
	wantEdges := []MarketplaceSelectionLock{edgeOnboarding, edgeStarters}
	if !reflect.DeepEqual(resources[2].SelectedBy, wantEdges) {
		t.Fatalf("selection edges = %#v, want %#v", resources[2].SelectedBy, wantEdges)
	}
	encoded, err := Encode(normalized)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, url := range []string{"url: https://market.example", "url: https://zeta.example"} {
		if strings.Count(output, url) != 1 {
			t.Fatalf("endpoint %q occurrences in emitted bytes = %d, want 1", url, strings.Count(output, url))
		}
	}
	if strings.Index(output, "url: https://market.example") > strings.Index(output, "url: https://zeta.example") {
		t.Fatalf("endpoint order in emitted bytes:\n%s", output)
	}
	onboarding := strings.Index(output, "\n        name: onboarding\n")
	starters := strings.Index(output, "\n        name: starters\n")
	review := strings.Index(output, "\n        name: review\n")
	if onboarding < 0 || starters < 0 || review < 0 || onboarding >= starters || starters >= review {
		t.Fatalf("resource order in emitted bytes:\n%s", output)
	}
	if strings.Count(output, "\n            name: starters\n") != 1 {
		t.Fatalf("selection edge dedupe failed in emitted bytes:\n%s", output)
	}
}

func TestV2AllowsGitOnlyAndEmptyMarketplaceState(t *testing.T) {
	gitRegistries := []GitRegistry{{
		URL: "https://git.example.com/team/tools", Ref: "main", Commit: mustOID(t, sha1a),
		Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a), Resources: []GitResource{
			{Kind: "skill", Name: "review", ContentDigest: Digest(sha256a), ArchiveDigest: Digest(sha256b), SupportFiles: map[string]Digest{}},
		}}},
	}}
	for _, tc := range []struct {
		name           string
		doc            Document
		wantRegistries []GitRegistry
	}{
		{name: "empty", doc: Document{APIVersion: APIVersionV2, Scope: ScopeRepo}},
		{name: "git-only", doc: Document{APIVersion: APIVersionV2, Scope: ScopeUser, Registries: gitRegistries}, wantRegistries: gitRegistries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Encode(tc.doc)
			if err != nil {
				t.Fatalf("Encode rejected %s v2 lock: %v", tc.name, err)
			}
			decoded, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode rejected %s v2 lock: %v", tc.name, err)
			}
			if decoded.APIVersion != APIVersionV2 || decoded.Scope != tc.doc.Scope || len(decoded.Marketplaces) != 0 {
				t.Fatalf("%s v2 lock decoded to %#v", tc.name, decoded)
			}
			if !reflect.DeepEqual(decoded.Registries, tc.wantRegistries) {
				t.Fatalf("%s v2 lock registries = %#v, want %#v", tc.name, decoded.Registries, tc.wantRegistries)
			}
		})
	}
}

// spec: acquisition-lock-codec/non-canonical-marketplace-endpoint-spellings-normalize-on-read
func TestV2NormalizesNonCanonicalEndpointOnDecode(t *testing.T) {
	data := []byte(strings.Replace(v2GoldenBytes, "url: https://market.example", "url: https://MARKET.example/", 1))
	doc, err := Decode(data)
	if err != nil {
		t.Fatalf("non-canonical endpoint rejected instead of normalized: %v", err)
	}
	if doc.Marketplaces[0].URL != "https://market.example" {
		t.Fatalf("endpoint not canonicalized: %q", doc.Marketplaces[0].URL)
	}
}
