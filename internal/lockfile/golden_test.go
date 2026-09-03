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

func TestCompleteDocumentGoldensCoverEveryScope(t *testing.T) {
	headers := map[Scope]string{
		ScopeOrganization: "apiVersion: agent-marketplace-lock/v1\nscope: organization",
		ScopeUser:         "apiVersion: agent-marketplace-lock/v1\nscope: user",
		ScopeRepo:         "apiVersion: agent-marketplace-lock/v1\nscope: repo",
		ScopeLocal:        "apiVersion: agent-marketplace-lock/v1\nscope: local",
	}
	for _, scope := range []Scope{ScopeOrganization, ScopeUser, ScopeRepo, ScopeLocal} {
		t.Run(string(scope), func(t *testing.T) {
			got, err := Encode(completeGoldenDocument(scope))
			if err != nil {
				t.Fatal(err)
			}
			// This expected YAML is authored independently of Encode and
			// Normalize. Keep every scope-specific header explicit while the
			// complete record body remains one readable byte fixture.
			want := headers[scope] + `
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
          - kind: command
            name: alpha
            content_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
            archive_digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
          - kind: skill
            name: zeta
            content_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
            support_files:
              docs/README.md: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
              examples/config.yaml: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
marketplaces:
  - url: https://market.example/api
    instance_id: market-1
    collections:
      - source_id: sets
        name: child
        revision: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        members:
          - source_id: source-a
            kind: command
            name: nested-mirror
            revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
          - source_id: source-a
            kind: skill
            name: nested-index
            revision: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
      - source_id: sets
        name: root
        revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        direct: true
        members:
          - source_id: sets
            kind: collection
            name: child
            revision: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
          - source_id: source-a
            kind: skill
            name: direct-mirror
            revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
    resources:
      - source_id: source-a
        kind: command
        name: nested-mirror
        revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
        acquisition: mirror
        archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        selected_by:
          - source_id: sets
            name: root
            revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - source_id: source-a
        kind: skill
        name: direct-mirror
        revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
        acquisition: mirror
        archive_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        direct: true
        selected_by:
          - source_id: sets
            name: root
            revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - source_id: source-a
        kind: skill
        name: nested-index
        revision: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        acquisition: index-only
        source_commit: sha1:89abcdef0123456789abcdef0123456789abcdef
        source_manifest_digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
        selected_by:
          - source_id: sets
            name: root
            revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - source_id: source-b
        kind: mcpserver
        name: direct-index
        revision: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        acquisition: index-only
        source_commit: sha1:0123456789abcdef0123456789abcdef01234567
        source_manifest_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        direct: true
`
			if string(got) != want {
				t.Fatalf("complete lock bytes:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func completeGoldenDocument(scope Scope) Document {
	return Document{
		APIVersion: APIVersionV1,
		Scope:      scope,
		Registries: []GitRegistry{{
			URL: "https://git.example.com/team/tools.git", Ref: "main", Commit: mustOIDForGolden("sha1:0123456789abcdef0123456789abcdef01234567"),
			Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest("sha256:" + strings.Repeat("a", 64)), Resources: []GitResource{
				{Kind: "skill", Name: "zeta", ContentDigest: Digest("sha256:" + strings.Repeat("a", 64)), ArchiveDigest: Digest("sha256:" + strings.Repeat("b", 64)), SupportFiles: map[string]Digest{
					"examples/config.yaml": Digest("sha256:" + strings.Repeat("d", 64)),
					"docs/README.md":       Digest("sha256:" + strings.Repeat("c", 64)),
				}},
				{Kind: "command", Name: "alpha", ContentDigest: Digest("sha256:" + strings.Repeat("d", 64)), ArchiveDigest: Digest("sha256:" + strings.Repeat("c", 64))},
			}}},
		}},
		Marketplaces: []Marketplace{{
			URL: "https://market.example/api/", InstanceID: "market-1",
			Collections: []MarketplaceCollection{
				{SourceID: "sets", Name: "root", Revision: Digest("sha256:" + strings.Repeat("a", 64)), Direct: true, Members: []CollectionEdge{{SourceID: "sets", Kind: "collection", Name: "child", Revision: Digest("sha256:" + strings.Repeat("b", 64))}, {SourceID: "source-a", Kind: "skill", Name: "direct-mirror", Revision: Digest("sha256:" + strings.Repeat("c", 64))}}},
				{SourceID: "sets", Name: "child", Revision: Digest("sha256:" + strings.Repeat("b", 64)), Members: []CollectionEdge{{SourceID: "source-a", Kind: "skill", Name: "nested-index", Revision: Digest("sha256:" + strings.Repeat("d", 64))}, {SourceID: "source-a", Kind: "command", Name: "nested-mirror", Revision: Digest("sha256:" + strings.Repeat("c", 64))}}},
			},
			Resources: []MarketplaceResource{
				{SourceID: "source-a", Kind: "command", Name: "nested-mirror", Revision: Digest("sha256:" + strings.Repeat("c", 64)), Acquisition: AcquisitionMirror, ArchiveDigest: Digest("sha256:" + strings.Repeat("b", 64)), SelectedBy: []CollectionEdge{{SourceID: "sets", Name: "root", Revision: Digest("sha256:" + strings.Repeat("a", 64))}}},
				{SourceID: "source-a", Kind: "skill", Name: "direct-mirror", Revision: Digest("sha256:" + strings.Repeat("c", 64)), Acquisition: AcquisitionMirror, ArchiveDigest: Digest("sha256:" + strings.Repeat("d", 64)), Direct: true, SelectedBy: []CollectionEdge{{SourceID: "sets", Name: "root", Revision: Digest("sha256:" + strings.Repeat("a", 64))}}},
				{SourceID: "source-a", Kind: "skill", Name: "nested-index", Revision: Digest("sha256:" + strings.Repeat("d", 64)), Acquisition: AcquisitionIndexOnly, SourceCommit: mustOIDForGolden("sha1:89abcdef0123456789abcdef0123456789abcdef"), SourceManifestDigest: Digest("sha256:" + strings.Repeat("c", 64)), SelectedBy: []CollectionEdge{{SourceID: "sets", Name: "root", Revision: Digest("sha256:" + strings.Repeat("a", 64))}}},
				{SourceID: "source-b", Kind: "mcpserver", Name: "direct-index", Revision: Digest("sha256:" + strings.Repeat("b", 64)), Acquisition: AcquisitionIndexOnly, SourceCommit: mustOIDForGolden("sha1:0123456789abcdef0123456789abcdef01234567"), SourceManifestDigest: Digest("sha256:" + strings.Repeat("d", 64)), Direct: true},
			},
		}},
	}
}

func mustOIDForGolden(value string) GitOID {
	oid, err := ParseGitOID(value)
	if err != nil {
		panic(err)
	}
	return oid
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
