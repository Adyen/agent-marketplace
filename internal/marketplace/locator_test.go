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
			input: "public:source-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Source: "source-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "alias revision digest",
			input: "public:source-one/command/check@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Source: "source-one", Kind: "command", Name: "check", Revision: "sha256:1111111111111111111111111111111111111111111111111111111111111111", Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		},
		{
			name:  "alias digest",
			input: "public:source-one/skill/review@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Source: "source-one", Kind: "skill", Name: "review", Revision: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
		{
			name:  "browser",
			input: "HTTPS://MARKET.example/s/source-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Source: "source-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "browser endpoint authority",
			input: "https://MARKET.example:443/s/source-one/skill/review",
			want:  MarketplaceLocator{Marketplace: "https://market.example:443", URL: "https://market.example:443", Source: "source-one", Kind: "skill", Name: "review"},
		},
		{
			name:  "browser revision digest",
			input: "https://market.example/s/source-one/skill/review/revisions/sha256:2222222222222222222222222222222222222222222222222222222222222222",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Source: "source-one", Kind: "skill", Name: "review", Revision: "sha256:2222222222222222222222222222222222222222222222222222222222222222", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
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
	for _, input := range []string{"public:source/skill/name@v2", "https://market.example/s/source/skill/name/revisions/main"} {
		if _, err := ParseMarketplaceLocator(input); err == nil || !strings.Contains(err.Error(), "exact content digest (sha256:<64 lowercase hex>)") {
			t.Fatalf("ParseMarketplaceLocator(%q) error = %v", input, err)
		}
	}
}

func TestParseMarketplaceLocatorRejectsUnsafeAndMalformedInput(t *testing.T) {
	inputs := []string{
		"public:source/unknown/name",
		"public:source/skill/name/extra",
		"public:source/skill/../name",
		"public:source/skill/name@",
		"public:source/skill/name@one@two",
		"public:source/skill/name@v2",
		"public:source/skill/name@SHA256:0000000000000000000000000000000000000000000000000000000000000000",
		"https://market.example/s/source/skill/name/revisions/main",
		"public:source\\evil/skill/name",
		"https://market.example/s/source/skill/name/extra",
		"https://market.example/s/source/skill/name?x=1",
		"https://market.example/s/source%2Funsafe/skill/name",
		"https://market.example/s/source/skill/../name",
		"https://market.example/s/source/skill/name/revisions/../evil",
		"https:///s/source/skill/name",
		"https://:443/s/source/skill/name",
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
