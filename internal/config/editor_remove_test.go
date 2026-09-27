package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateRegistryRefInConfigBumpsLegacyRegistriesAndV4Sources(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         int
	}{
		{name: "legacy", want: 1, config: `apiVersion: agent-marketplace/v2
registries:
  - url: https://example.com/target.git
    ref: v1.0 # pinned release
  - url: https://example.com/other.git
    ref: v1.0
`},
		{name: "v4", want: 2, config: `apiVersion: agent-marketplace/v4
sources:
  target: {git: 'https://example.com/target', ref: v1.0}
  path-copy:
    git: https://example.com/target.git
    ref: v1.0 # pinned release
    path: skills
  other: {git: 'https://example.com/other.git', ref: v1.0}
  pinned-elsewhere: {git: 'https://example.com/target.git', ref: v0.9}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.config), 0o644); err != nil {
				t.Fatal(err)
			}
			updated, err := UpdateRegistryRefInConfig(path, "https://example.com/target.git", "v1.0", "v1.1")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			if updated != tc.want || strings.Count(got, "v1.1") != tc.want {
				t.Fatalf("updated %d entries (want %d):\n%s", updated, tc.want, got)
			}
			if !strings.Contains(got, "# pinned release") || !strings.Contains(got, "other.git") || strings.Count(got, "v1.0") != 1 {
				t.Fatalf("bump touched an unrelated entry or dropped a comment:\n%s", got)
			}
			if tc.name == "v4" && (!strings.Contains(got, "ref: v0.9") || !strings.Contains(got, "path: skills")) {
				t.Fatalf("bump changed a source at another ref or its path:\n%s", got)
			}
		})
	}
}
