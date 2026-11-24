package lockfile

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPlaceMarketplaceLockBinding(t *testing.T) {
	fixture := placeMarketplaceLockFixture(t, ScopeUser)
	decoded, err := Decode(fixture)
	if err != nil {
		t.Fatalf("Decode place lock: %v", err)
	}
	encoded, err := Encode(decoded)
	if err != nil {
		t.Fatalf("Encode place lock: %v", err)
	}
	for _, field := range []string{
		"kind: invented-app/mod\n",
		"assessment_digest: " + sha256e,
		"host_package_identity:\n",
		"package_content_digest: " + sha256a,
		"delivery_mapping_digest: " + sha256c,
		"profile_digest: " + sha256d,
		"profile_identity:\n",
		"target_id: invented-app\n",
		"source_id: builtin:agent-marketplace\n",
		"revision_digest: " + sha256d,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("encoded place lock missing %q:\n%s", field, encoded)
		}
	}
	roundTrip, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode encoded place lock: %v", err)
	}
	if !reflect.DeepEqual(roundTrip, decoded) {
		t.Fatalf("place lock round trip mismatch:\n%#v\nwant:\n%#v", roundTrip, decoded)
	}
	cloned := cloneDocument(decoded)
	originalBinding := decoded.Marketplaces[0].Resources[0].HostPackageIdentity
	clonedBinding := cloned.Marketplaces[0].Resources[0].HostPackageIdentity
	if clonedBinding == nil || clonedBinding == originalBinding {
		t.Fatal("cloneDocument did not copy the place binding pointer")
	}
	clonedBinding.DeliveryMappingDigest = sha256f
	if originalBinding.DeliveryMappingDigest == sha256f {
		t.Fatal("cloneDocument binding copy aliases the original")
	}

	duplicate := decoded
	duplicate.Marketplaces = append([]MarketplaceLock(nil), decoded.Marketplaces...)
	duplicate.Marketplaces[0].Resources = append(
		append([]MarketplaceResourceLock(nil), decoded.Marketplaces[0].Resources...),
		decoded.Marketplaces[0].Resources[0],
	)
	normalized, err := Normalize(duplicate)
	if err != nil {
		t.Fatalf("Normalize identical place records: %v", err)
	}
	if got := len(normalized.Marketplaces[0].Resources); got != 1 {
		t.Fatalf("identical place records normalized to %d entries, want 1", got)
	}

	duplicateFixture := string(fixture)
	resourceStart := strings.Index(duplicateFixture, "      - registry_name:")
	if resourceStart < 0 {
		t.Fatal("place resource fixture has no resource entry")
	}
	duplicateFixture += duplicateFixture[resourceStart:]
	if _, err := Decode([]byte(duplicateFixture)); err != nil {
		t.Fatalf("Decode identical duplicate place records: %v", err)
	}
	conflictingFixture := duplicateFixture
	mappingDigest := "delivery_mapping_digest: " + sha256c
	lastMapping := strings.LastIndex(conflictingFixture, mappingDigest)
	if lastMapping < 0 {
		t.Fatal("duplicate place fixture has no mapping digest")
	}
	conflictingFixture = conflictingFixture[:lastMapping] +
		strings.Replace(conflictingFixture[lastMapping:], sha256c, sha256f, 1)
	requirePlaceLockError(t, decodePlaceFixture(t, conflictingFixture), CodeIdentityConflict, "")

	for _, tc := range []struct {
		name, fixture, message string
		code                   ErrorCode
	}{
		{
			name:    "missing assessment digest",
			fixture: strings.Replace(string(fixture), "        assessment_digest: "+sha256e+"\n", "", 1),
			message: "marketplace.cache.profile-binding-missing",
		},
		{
			name:    "missing binding",
			fixture: strings.SplitN(string(fixture), "        host_package_identity:\n", 2)[0],
			message: "marketplace.cache.profile-binding-missing",
		},
		{
			name:    "malformed assessment digest",
			fixture: strings.Replace(string(fixture), "assessment_digest: "+sha256e, "assessment_digest: malformed", 1),
			code:    CodeMalformedYAML,
		},
		{
			name:    "wrong profile target",
			fixture: strings.Replace(string(fixture), "target_id: invented-app", "target_id: other-app", 1),
			message: "host_package_identity",
		},
		{
			name:    "wrong package content digest",
			fixture: strings.Replace(string(fixture), "package_content_digest: "+sha256a, "package_content_digest: "+sha256f, 1),
			message: "host_package_identity",
		},
		{
			name:    "binding on legacy resource",
			fixture: strings.Replace(string(fixture), "kind: invented-app/mod", "kind: skill", 1),
			message: "host_package_identity",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			if code == "" {
				code = CodeInvalidDocument
			}
			requirePlaceLockError(t, decodePlaceFixture(t, tc.fixture), code, tc.message)
		})
	}

	for _, scope := range []Scope{ScopeOrganization, ScopeUser} {
		if _, err := Decode(placeMarketplaceLockFixture(t, scope)); err != nil {
			t.Errorf("Decode scoped lock in %s scope: %v", scope, err)
		}
	}
	for _, scope := range []Scope{ScopeRepo, ScopeLocal} {
		t.Run(string(scope)+" scope refused", func(t *testing.T) {
			raw := placeMarketplaceLockFixture(t, scope)
			requirePlaceLockError(t, decodePlaceFixture(t, string(raw)), CodeInvalidScope, "")
		})
	}
}

func placeMarketplaceLockFixture(t *testing.T, scope Scope) []byte {
	t.Helper()
	resource := validV2Resource()
	doc := Document{
		APIVersion: APIVersionV2,
		Scope:      scope,
		Marketplaces: []MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []MarketplaceResourceLock{resource},
		}},
	}
	encoded, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode legacy-shaped fixture: %v", err)
	}
	fixture := string(encoded)
	for _, replacement := range [][2]string{
		{"kind: skill\n", "kind: invented-app/mod\n"},
		{"name: review\n", "name: sample\n"},
		{"revision: rev-a\n", "revision: " + sha256a + "\n"},
		{"        acquisition: mirror\n", `        acquisition: mirror
        assessment_digest: ` + sha256e + `
        host_package_identity:
          package_content_digest: ` + sha256a + `
          delivery_mapping_digest: ` + sha256c + `
          profile_digest: ` + sha256d + `
          profile_identity:
            target_id: invented-app
            source_id: builtin:agent-marketplace
            revision_digest: ` + sha256d + `
`},
	} {
		if !strings.Contains(fixture, replacement[0]) {
			t.Fatalf("legacy-shaped fixture missing replacement anchor %q", replacement[0])
		}
		fixture = strings.Replace(fixture, replacement[0], replacement[1], 1)
	}
	return []byte(fixture)
}

func decodePlaceFixture(t *testing.T, fixture string) error {
	t.Helper()
	_, err := Decode([]byte(fixture))
	return err
}

func requirePlaceLockError(t *testing.T, err error, code ErrorCode, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Decode succeeded, want error code %s", code)
	}
	var lockErr *Error
	if !errors.As(err, &lockErr) || lockErr.Code != code {
		t.Fatalf("error = %v, want lockfile code %s", err, code)
	}
	if message != "" && !strings.Contains(err.Error(), message) {
		t.Fatalf("error = %v, want message containing %q", err, message)
	}
}
