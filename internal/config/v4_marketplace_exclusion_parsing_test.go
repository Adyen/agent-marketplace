package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func v4MPExclusionParsingPublicFixture(tb testing.TB, value string) *V4Config {
	tb.Helper()
	cfg, err := ParseV4([]byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources: {m: {exclude: [%q]}}\n", value)))
	if err != nil {
		tb.Fatal(err)
	}
	return cfg
}

// Freeze the old runtime guard, including private-input quirks. The input
// parser's stronger validation is deliberately not the oracle.
func legacyV4MPExclusionAccepted(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 3 && parts[1] != "collection" && parts[2] != "*"
}

func checkV4MPExclusionPrivateReplay(t *testing.T, value string, layer layerKind) {
	t.Helper()
	cfg := v4MarketplaceExclusionKindFixture(t, 1)
	group := cfg.Groups["m"]
	group.Exclude[0] = value
	group.Decisions[0].Exclusion = value
	cfg.Groups["m"] = group
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfig(layer)
	after, marshalErr := json.Marshal(cfg)
	if marshalErr != nil || !bytes.Equal(before, after) ||
		cfg.Groups["m"].Exclude[0] != value || cfg.Groups["m"].Decisions[0].Exclusion != value {
		t.Fatal("adapter mutated its input")
	}
	if layer == userLayer && legacyV4MPExclusionAccepted(value) {
		if err != nil || out == nil || len(out.Marketplaces) != 0 || len(out.MarketplaceExclusions) != 1 ||
			len(out.Registries) != 1 || len(out.Registries[0].Commands.Include) != 1 ||
			out.Registries[0].Commands.Include[0] != "kept" {
			t.Fatalf("private value %q: successful output or retained Git control changed: %v", value, err)
		}
		want := MarketplaceExclusion{URL: "https://market.example", Locator: value, Source: UserPersonal}
		if out.MarketplaceExclusions[0] != want {
			t.Fatalf("private value %q: exclusion = %#v, want %#v", value, out.MarketplaceExclusions[0], want)
		}
		return
	}
	code, message, scope := "v4.marketplace-runtime-unsupported", "Marketplace exclusions require an exact non-collection registry/kind/name locator", "user"
	if layer != userLayer {
		code, message = "v4.layer-field", "Marketplace overrides are only supported in the user configuration"
		switch layer {
		case organizationLayer:
			scope = "organization"
		case repoLayer:
			scope = "repo"
		case localLayer:
			scope = "local"
		}
	}
	var refusal *V4ValidationError
	if out != nil || !errors.As(err, &refusal) || refusal.Code != code || refusal.Msg != message ||
		refusal.Path != "resources.m.exclude" || refusal.Line != cfg.Sources["m"].Line ||
		refusal.File != "" || refusal.DiagnosticScope() != scope {
		t.Fatalf("private value %q, layer %v: refusal = %#v (%v)", value, layer, refusal, err)
	}
}

func TestV4MPExclusionParsingPrivateReplay(t *testing.T) {
	values := []string{
		"", "*", "/", "//", "///", "tools", "tools/skill", "tools/skill/item",
		"tools/skill/", "/skill/item", "tools//item", "*/skill/item", "tools/*/item",
		"tools/skill/*", "tools/skill/a*b", "tools/collection/item", "tools/collection/",
		"tools/collections/item", "tools/Collection/item", "tools/collection*/item",
		"tools/skill/item/extra", "tools/skill/item/", "tools//skill/item",
		"tools/skill/\x00", "tools/skill/\xff", "tools/skill/\r\n", " tools/skill/item ",
		"tools/skill/" + strings.Repeat("x", 4096),
	}
	for _, value := range values {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			for _, layer := range []layerKind{userLayer, organizationLayer, repoLayer, localLayer} {
				checkV4MPExclusionPrivateReplay(t, value, layer)
			}
		})
	}
}

func FuzzV4MPExclusionParsingPrivateReplay(f *testing.F) {
	for _, value := range []string{"", "//", "tools/skill/item", "tools/collection/item", "tools/skill/*", "tools/skill/item/extra", "tools/skill/\xff"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		checkV4MPExclusionPrivateReplay(t, value, userLayer)
	})
}

func TestV4MPExclusionParsingPublicKinds(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			value := "tools/" + kind + "/item"
			cfg := v4MPExclusionParsingPublicFixture(t, value)
			out, err := cfg.ToLegacyConfig(userLayer)
			if err != nil || out == nil || len(out.Marketplaces) != 0 || len(out.MarketplaceExclusions) != 1 ||
				out.MarketplaceExclusions[0] != (MarketplaceExclusion{URL: "https://market.example", Locator: value, Source: UserPersonal}) {
				t.Fatalf("public exact exclusion changed: %#v (%v)", out, err)
			}
		})
	}
}

func TestV4MPExclusionParsingPublicRefusals(t *testing.T) {
	for _, value := range []string{"*", "tools/*", "tools/skill/*", "tools/collection/item"} {
		t.Run(value, func(t *testing.T) {
			cfg := v4MPExclusionParsingPublicFixture(t, value)
			for _, layer := range []layerKind{userLayer, organizationLayer, repoLayer, localLayer} {
				out, err := cfg.ToLegacyConfig(layer)
				var refusal *V4ValidationError
				wantCode := "v4.marketplace-runtime-unsupported"
				if layer != userLayer {
					wantCode = "v4.layer-field"
				}
				if out != nil || !errors.As(err, &refusal) || refusal.Code != wantCode || refusal.Path != "resources.m.exclude" {
					t.Fatalf("layer %v refusal = %#v (%v)", layer, refusal, err)
				}
			}
		})
	}
}
