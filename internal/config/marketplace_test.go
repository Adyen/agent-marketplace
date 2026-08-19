package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateMarketplaceEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []MarketplaceEntry
		wantErr string
	}{
		{
			name: "valid",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "01JEXAMPLE",
				Resources: []MarketplaceResourcePin{{
					SourceID: "01JSOURCE",
					Kind:     "skill",
					Name:     "review",
					Revision: "v1.0.0",
					Digest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				}},
			}},
		},
		{
			name:    "missing url",
			entries: []MarketplaceEntry{{InstanceID: "x"}},
			wantErr: "marketplaces[0].url is required",
		},
		{
			name:    "missing instance_id",
			entries: []MarketplaceEntry{{URL: "https://market.example"}},
			wantErr: "marketplaces[0].instance_id is required",
		},
		{
			name:    "path traversal",
			entries: []MarketplaceEntry{{URL: "https://market.example/../evil", InstanceID: "x"}},
			wantErr: "path traversal",
		},
		{
			name: "invalid pin kind",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{Kind: "rule"}},
			}},
			wantErr: "not a valid marketplace kind",
		},
		{
			name: "invalid digest",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{SourceID: "s", Kind: "skill", Name: "n", Revision: "r", Digest: "bad"}},
			}},
			wantErr: "digest",
		},
		{
			name: "invalid acquisition",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{SourceID: "s", Kind: "skill", Name: "n", Revision: "r", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Acquisition: "clone"}},
			}},
			wantErr: "acquisition",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateMarketplaceEntries(tc.entries)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, errs)
			}
		})
	}
}

func TestValidateMarketplaceURLs(t *testing.T) {
	entries := []MarketplaceEntry{
		{URL: "https://market.example.com/skills"},
		{URL: "https://evil.example.com/skills"},
	}

	err := ValidateMarketplaceURLs(entries, []string{`market\.example\.com`})
	if err == nil {
		t.Fatalf("expected blocked URL error, got nil")
	}
	if !strings.Contains(err.Error(), "evil.example.com") {
		t.Fatalf("expected error to mention evil.example.com, got %v", err)
	}

	if err := ValidateMarketplaceURLs(entries, nil); err != nil {
		t.Fatalf("nil allowlist should permit all URLs: %v", err)
	}

	if err := ValidateMarketplaceURLs(entries, []string{}); err == nil {
		t.Fatalf("empty allowlist should deny all URLs")
	}
}

func TestMarketplaceConfigRoundTrip(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory"},
		Marketplaces: []MarketplaceEntry{{
			URL:        "https://market.example",
			InstanceID: "01JEXAMPLE",
			Resources: []MarketplaceResourcePin{{
				SourceID:    "01JSOURCE",
				Kind:        "skill",
				Name:        "review",
				Revision:    "v1.0.0",
				Digest:      "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Acquisition: "mirror",
			}},
		}},
		AllowedMarketplaces: &[]string{`market\.example\.com`},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Config
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Marketplaces) != 1 || got.Marketplaces[0].InstanceID != cfg.Marketplaces[0].InstanceID {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if got.AllowedMarketplaces == nil || !slices.Equal(*got.AllowedMarketplaces, *cfg.AllowedMarketplaces) {
		t.Fatalf("AllowedMarketplaces round-trip mismatch: %v", got.AllowedMarketplaces)
	}
}

func TestMarketplaceMerge(t *testing.T) {
	base := &Config{
		AllowedMarketplaces: &[]string{`base\.example`},
		Marketplaces: []MarketplaceEntry{
			{URL: "https://base.example", InstanceID: "base"},
		},
	}
	repo := &Config{
		AllowedMarketplaces: &[]string{`repo\.example`},
		Marketplaces: []MarketplaceEntry{
			{URL: "https://repo.example", InstanceID: "repo"},
		},
	}

	merged := Merge(base, repo)

	// First non-nil wins for allowlist.
	if merged.AllowedMarketplaces == nil || !slices.Equal(*merged.AllowedMarketplaces, []string{`base\.example`}) {
		t.Fatalf("AllowedMarketplaces should be first-non-nil-wins, got %v", merged.AllowedMarketplaces)
	}

	// Marketplaces accumulate.
	if len(merged.Marketplaces) != 2 {
		t.Fatalf("expected 2 merged marketplaces, got %d", len(merged.Marketplaces))
	}
}

func TestMarketplaceLayerAllowlist(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.local.yaml")
	writeFile(t, path, "marketplaces:\n  - url: https://local.example\n    instance_id: 01JLOCAL\n    resources: []\n")

	localCfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadLocalConfigFromPath: %v", err)
	}
	if len(localCfg.Marketplaces) != 1 {
		t.Fatalf("expected 1 marketplace in local config, got %d", len(localCfg.Marketplaces))
	}
}
