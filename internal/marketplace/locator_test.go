package marketplace

import (
	"strings"
	"testing"
)

func TestParseMarketplaceLocator(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    MarketplaceLocator
		wantErr string
	}{
		{
			name:  "alias",
			input: "public:registry-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Registry: "registry-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "alias revision digest",
			input: "public:registry-one/command/check@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Registry: "registry-one", Kind: "command", Name: "check", Revision: "sha256:1111111111111111111111111111111111111111111111111111111111111111", Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		},
		{
			name:  "alias digest",
			input: "public:registry-one/skill/review@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Registry: "registry-one", Kind: "skill", Name: "review", Revision: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
		{
			name:  "browser",
			input: "HTTPS://MARKET.example/r/registry-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Registry: "registry-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "browser endpoint authority",
			input: "https://MARKET.example:443/r/registry-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "https://market.example:443", URL: "https://market.example:443", Registry: "registry-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "browser revision digest",
			input: "https://market.example/r/registry-one/skill/review/revisions/sha256:2222222222222222222222222222222222222222222222222222222222222222",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Registry: "registry-one", Kind: "skill", Name: "review", Revision: "sha256:2222222222222222222222222222222222222222222222222222222222222222", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMarketplaceLocator(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMarketplaceLocator: %v", err)
			}
			if got != tt.want {
				t.Fatalf("locator = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseMarketplaceLocatorRejectsNamedRevisionsWithExplicitError(t *testing.T) {
	for _, input := range []string{"public:registry/skill/name@v2", "https://market.example/r/registry/skill/name/revisions/main"} {
		if _, err := ParseMarketplaceLocator(input); err == nil || !strings.Contains(err.Error(), "exact content digest (sha256:<64 lowercase hex>)") {
			t.Fatalf("ParseMarketplaceLocator(%q) error = %v", input, err)
		}
	}
}

func TestParseMarketplaceLocatorRejectsUnsafeAndMalformedInput(t *testing.T) {
	inputs := []string{
		"public:registry/unknown/name",
		"public:registry/skill/name/extra",
		"public:registry/skill/../name",
		"public:registry/skill/name@",
		"public:registry/skill/name@one@two",
		"public:registry/skill/name@v2",
		"public:registry/skill/name@SHA256:0000000000000000000000000000000000000000000000000000000000000000",
		"https://market.example/r/registry/skill/name/revisions/main",
		"public:registry\\evil/skill/name",
		"https://market.example/r/registry/skill/name/extra",
		"https://market.example/r/registry/skill/name?x=1",
		"https://market.example/r/registry%2Funsafe/skill/name",
		"https://market.example/r/registry/skill/../name",
		"https://market.example/r/registry/skill/name/revisions/../evil",
		"https:///r/registry/skill/name",
		"https://:443/r/registry/skill/name",
	}
	for _, input := range inputs {
		t.Run(strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := ParseMarketplaceLocator(input); err == nil {
				t.Fatalf("ParseMarketplaceLocator(%q) succeeded; want error", input)
			}
		})
	}
}

func TestNormalizeMarketplaceURL(t *testing.T) {
	got, err := NormalizeMarketplaceURL("HTTPS://MARKET.example/")
	if err != nil {
		t.Fatalf("NormalizeMarketplaceURL: %v", err)
	}
	if got != "https://market.example" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestNormalizeMarketplaceURLRejectsUnsafeEndpointForms(t *testing.T) {
	for _, input := range []string{
		"http://market.example?token=secret",
		"http://market.example#fragment",
		"http://user:pass@market.example",
		"http://market.example/../escape",
		"http://market.example/%2fescape",
		"http:///missing-host",
	} {
		t.Run(strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := NormalizeMarketplaceURL(input); err == nil {
				t.Fatalf("NormalizeMarketplaceURL(%q) succeeded; want error", input)
			}
		})
	}
}

func TestValidateInstalledName(t *testing.T) {
	for _, name := range []string{
		"", ".", "..", "a/b", `a\\b`, "a\n b", "bad:name", "bad<name>",
		"review.", "review ", "CON", "con.txt", "COM1", "LPT9",
	} {
		if err := ValidateInstalledName(name); err == nil {
			t.Errorf("ValidateInstalledName(%q) succeeded; want error", name)
		}
	}
	for _, name := range []string{"review-copy", "review.copy", "review copy", "COM0", "résumé"} {
		if err := ValidateInstalledName(name); err != nil {
			t.Errorf("valid installed name %q rejected: %v", name, err)
		}
	}
}
