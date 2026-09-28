package lockfile

import (
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

func TestCompleteGitDocumentGoldensCoverEveryScope(t *testing.T) {
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
