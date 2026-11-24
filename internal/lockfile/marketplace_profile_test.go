package lockfile

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

const virtualProfileLockYAML = `apiVersion: agent-marketplace-lock/v2
scope: user
marketplaces:
  - url: https://market.example
    instance_id: market-id
    registry_bindings:
      - name: tools
        registry_id: tools-id
    resources:
      - registry_name: tools
        registry_id: tools-id
        kind: target-profile
        name: invented-app
        revision: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        content_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
        acquisition: mirror
        profile_identity:
          target_id: invented-app
          source_id: 'marketplace:https%3A%2F%2Fmarket.example#market-id/tools-id/profiles'
          revision_digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
`

func TestMarketplaceVirtualProfileLock(t *testing.T) {
	doc, err := Decode([]byte(virtualProfileLockYAML))
	if err != nil {
		t.Fatalf("Decode virtual profile lock: %v", err)
	}
	resource := doc.Marketplaces[0].Resources[0]
	if resource.ProfileIdentity == nil {
		t.Fatal("virtual profile identity was not decoded")
	}
	wantIdentity := targetprofile.Identity{
		TargetID:       "invented-app",
		SourceID:       "marketplace:https%3A%2F%2Fmarket.example#market-id/tools-id/profiles",
		RevisionDigest: sha256d,
	}
	if *resource.ProfileIdentity != wantIdentity || resource.Kind != "target-profile" || resource.HostPackageIdentity != nil ||
		resource.AssessmentDigest != "" || resource.ArchiveDigest != "" {
		t.Fatalf("decoded virtual profile resource = %#v", resource)
	}
	encoded, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode virtual profile lock: %v", err)
	}
	for _, field := range []string{
		"kind: target-profile",
		"name: invented-app",
		"profile_identity:",
		"target_id: invented-app",
		"source_id: marketplace:https%3A%2F%2Fmarket.example#market-id/tools-id/profiles",
		"revision_digest: " + sha256d,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("encoded virtual profile lock missing %q:\n%s", field, encoded)
		}
	}
	roundTrip, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode encoded virtual profile lock: %v", err)
	}
	if !reflect.DeepEqual(roundTrip, doc) {
		t.Fatalf("virtual profile lock round trip mismatch:\n%#v\nwant:\n%#v", roundTrip, doc)
	}
	organization := strings.Replace(virtualProfileLockYAML, "scope: user", "scope: organization", 1)
	if _, err := Decode([]byte(organization)); err != nil {
		t.Fatalf("organization profile lock: %v", err)
	}

	cases := []struct {
		name, fixture string
		code          ErrorCode
	}{
		{
			name:    "missing identity",
			fixture: strings.SplitN(virtualProfileLockYAML, "        profile_identity:\n", 2)[0],
			code:    CodeInvalidDocument,
		},
		{
			name:    "name mismatch",
			fixture: strings.Replace(virtualProfileLockYAML, "        name: invented-app", "        name: other-app", 1),
			code:    CodeInvalidDocument,
		},
		{
			name:    "identity digest mismatch",
			fixture: strings.Replace(virtualProfileLockYAML, "revision_digest: "+sha256d, "revision_digest: "+sha256e, 1),
			code:    CodeInvalidDocument,
		},
		{
			name:    "archive forbidden",
			fixture: strings.TrimSuffix(virtualProfileLockYAML, "\n") + "\n        archive_digest: " + sha256b + "\n",
			code:    CodeInvalidDocument,
		},
		{
			name:    "assessment forbidden",
			fixture: strings.TrimSuffix(virtualProfileLockYAML, "\n") + "\n        assessment_digest: " + sha256c + "\n",
			code:    CodeInvalidDocument,
		},
		{
			name:    "repository scope",
			fixture: strings.Replace(virtualProfileLockYAML, "scope: user", "scope: repo", 1),
			code:    CodeInvalidScope,
		},
		{
			name:    "local scope",
			fixture: strings.Replace(virtualProfileLockYAML, "scope: user", "scope: local", 1),
			code:    CodeInvalidScope,
		},
		{
			name:    "identity on legacy resource",
			fixture: strings.Replace(virtualProfileLockYAML, "kind: target-profile", "kind: skill", 1),
			code:    CodeInvalidDocument,
		},
		{
			name:    "marketplace endpoint mismatch",
			fixture: strings.Replace(virtualProfileLockYAML, "https%3A%2F%2Fmarket.example", "https%3A%2F%2Fother.example", 1),
			code:    CodeInvalidDocument,
		},
		{
			name:    "marketplace instance mismatch",
			fixture: strings.Replace(virtualProfileLockYAML, "#market-id/tools-id/profiles", "#other-id/tools-id/profiles", 1),
			code:    CodeInvalidDocument,
		},
		{
			name:    "marketplace registry mismatch",
			fixture: strings.Replace(virtualProfileLockYAML, "#market-id/tools-id/profiles", "#market-id/other-id/profiles", 1),
			code:    CodeInvalidDocument,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.fixture))
			if err == nil {
				t.Fatal("Decode succeeded, want refusal")
			}
			var lockErr *Error
			if !errors.As(err, &lockErr) || lockErr.Code != tc.code {
				t.Fatalf("error = %v, want lockfile code %s", err, tc.code)
			}
		})
	}
}
