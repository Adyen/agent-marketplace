package marketplace

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestParseRevocationOverride(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	override, err := ParseRevocationOverride("mp/src/skill/review@" + digest)
	if err != nil {
		t.Fatalf("ParseRevocationOverride: %v", err)
	}
	policy := &RevocationOverridePolicy{overrides: []RevocationOverride{override}}
	if !policy.AllowRevoked(resolver.MarketplaceRevocationIdentity{
		Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review", ContentDigest: digest,
	}) {
		t.Fatal("exact revoked identity was not allowed")
	}
	for name, identity := range map[string]resolver.MarketplaceRevocationIdentity{
		"wrong marketplace": {Marketplace: "other", Registry: "src", Kind: "skill", Name: "review", ContentDigest: digest},
		"wrong registry":    {Marketplace: "mp", Registry: "other", Kind: "skill", Name: "review", ContentDigest: digest},
		"wrong kind":        {Marketplace: "mp", Registry: "src", Kind: "command", Name: "review", ContentDigest: digest},
		"wrong name":        {Marketplace: "mp", Registry: "src", Kind: "skill", Name: "other", ContentDigest: digest},
		"wrong digest":      {Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review", ContentDigest: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"},
	} {
		if policy.AllowRevoked(identity) {
			t.Errorf("%s unexpectedly allowed", name)
		}
	}
}

func TestParseRevocationOverrideRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{
		"mp/src/skill/review",
		"mp/src/skill/review@sha256:bad",
		"mp/src/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef@extra",
		"mp/src/unknown/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if _, err := ParseRevocationOverride(raw); err == nil {
			t.Errorf("ParseRevocationOverride(%q) succeeded, want error", raw)
		}
	}
}

func TestRevocationOverridePolicyIsNotPersistent(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	policy, err := NewRevocationOverridePolicy([]string{"mp/src/skill/review@" + digest})
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil || !policy.AllowRevoked(resolver.MarketplaceRevocationIdentity{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review", ContentDigest: digest}) {
		t.Fatal("policy did not allow configured identity")
	}
	fresh, err := NewRevocationOverridePolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh != nil && fresh.AllowRevoked(resolver.MarketplaceRevocationIdentity{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review", ContentDigest: digest}) {
		t.Fatal("new invocation inherited revoked override")
	}
}
