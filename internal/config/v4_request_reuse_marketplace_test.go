package config

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// Marketplace callers retain the lazy path: protected exact selections must
// suppress their exclusions without suppressing a genuinely unselected one.
func TestV4RequestReuseMarketplaceLazyFallback(t *testing.T) {
	cfg, err := ParseV4([]byte(v4MarketplaceConfigYAML + "    required: true\n    exclude: [tools/skill/review, tools/skill/absent]\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc := v4LockFixture(t, lockfile.ScopeOrganization)
	before, err := json.Marshal([]any{cfg, doc})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfigWithLock(organizationLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Marketplaces) != 1 || !out.Marketplaces[0].Required ||
		len(out.MarketplaceExclusions) != 1 || out.MarketplaceExclusions[0].Locator != "tools/skill/absent" {
		t.Fatalf("lazy Marketplace exclusions or required policy changed: %#v", out)
	}
	found := false
	for _, pin := range out.Marketplaces[0].Resources {
		if pin.Name == "review" {
			found = true
			if out.MarketplacePinExcluded(out.Marketplaces[0], pin) {
				t.Fatal("protected direct selection was excluded")
			}
		}
	}
	if !found {
		t.Fatal("fixture did not retain its protected direct selection")
	}
	after, err := json.Marshal([]any{cfg, doc})
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("lazy adaptation mutated authored configuration or lock: %v", err)
	}
}
