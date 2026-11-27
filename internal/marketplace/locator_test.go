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
			name:  "alias scoped package kind",
			input: "public:registry-one/team%2Fmod/example",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Registry: "registry-one", Kind: "team/mod", Name: "example"},
		},
		{
			name:  "alias target profile",
			input: "public:registry-one/target-profile/team",
			want:  MarketplaceLocator{Marketplace: "public", Alias: "public", Registry: "registry-one", Kind: "target-profile", Name: "team"},
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
			name:  "browser scoped package kind",
			input: "https://market.example/r/registry-one/team%2Fmod/example",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Registry: "registry-one", Kind: "team/mod", Name: "example"},
		},
		{
			name:  "browser target profile",
			input: "https://market.example/r/registry-one/target-profile/team",
			want:  MarketplaceLocator{Marketplace: "https://market.example", URL: "https://market.example", Registry: "registry-one", Kind: "target-profile", Name: "team"},
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
		{
			name:  "browser revision digest with endpoint prefix",
			input: "https://MARKET.example/services/marketplace/r/registry-one/skill/review/revisions/sha256%3A3333333333333333333333333333333333333333333333333333333333333333",
			want:  MarketplaceLocator{Marketplace: "https://market.example/services/marketplace", URL: "https://market.example/services/marketplace", Registry: "registry-one", Kind: "skill", Name: "review", Revision: "sha256:3333333333333333333333333333333333333333333333333333333333333333", Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333"},
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
		"public:registry/team/mod/example",
		"public:registry/team%2fmod/example",
		"public:registry/team%252Fmod/example",
		"public:registry/team%2Fmod%2Fother/example",
		"public:registry/team%2F/example",
		"public:registry/%2Fmod/example",
		"public:registry/team%2Fmod/bad%2Fname",
		"public:registry/team%2Fmod/.",
		"public:registry/team%2Fmod/Bad",
		"public:registry/target-profile/Bad",
		"https://market.example/r/registry/skill/name/revisions/main",
		"public:registry\\evil/skill/name",
		"https://market.example/r/registry/skill/name/extra",
		"https://market.example/r/registry/skill/name?x=1",
		"https://market.example/r/registry%2Funsafe/skill/name",
		"https://market.example/r/registry/team%2fmod/name",
		"https://market.example/r/registry/team%252Fmod/name",
		"https://market.example/r/registry/team%2Fmod%2Fother/name",
		"https://market.example/r/registry/team%2F/name",
		"https://market.example/r/registry/%2Fmod/name",
		"https://market.example/r/registry/team%2Fmod/bad%2Fname",
		"https://market.example/r/registry/team%2Fmod/.",
		"https://market.example/r/registry/team%2Fmod/Bad",
		"https://market.example/r/registry/target-profile/Bad",
		"https://market.example/%2Fprefix/r/registry/team%2Fmod/name",
		"https://market.example/%252Fprefix/r/registry/team%2Fmod/name",
		"https://market.example/r/registry/team%2Fmod/name/revisions/sha256%2F0000000000000000000000000000000000000000000000000000000000000000",
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
