package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateRegistryRefInConfigBumpsV4Sources(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         int
	}{
		{name: "single source", want: 1, config: `apiVersion: agent-marketplace/v4
sources:
  target:
    git: https://example.com/target.git
    ref: v1.0 # pinned release
  other: {git: 'https://example.com/other.git', ref: v1.0}
`},
		{name: "normalized origin and path copy", want: 2, config: `apiVersion: agent-marketplace/v4
sources:
  target: {git: 'https://example.com/target', ref: v1.0}
  path-copy:
    git: https://example.com/target.git
    ref: v1.0 # pinned release
    path: skills
  other: {git: 'https://example.com/other.git', ref: v1.0}
  pinned-elsewhere: {git: 'https://example.com/target.git', ref: v0.9}
resources:
  path-copy: [skill/review]
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.config), 0o640); err != nil {
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
			if tc.want == 2 && (!strings.Contains(got, "ref: v0.9") || !strings.Contains(got, "path: skills") || !strings.Contains(got, "skill/review")) {
				t.Fatalf("bump changed another ref, path, or selection:\n%s", got)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("mode not preserved: %v, %v", info, err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			updated, err = UpdateRegistryRefInConfig(path, "https://example.com/target.git", "v1.0", "v1.1")
			if err != nil || updated != 0 {
				t.Fatalf("repeat bump = %d, %v", updated, err)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("repeat bump rewrote file: %v", err)
			}
			if repeated, err := os.ReadFile(path); err != nil || string(repeated) != got {
				t.Fatalf("repeat bump changed bytes: %s, %v", repeated, err)
			}
		})
	}
}

func TestUpdateRegistryRefInConfigRefusesLegacyWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const original = "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.com/target.git\n    ref: v1.0 # pinned release\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := UpdateRegistryRefInConfig(path, "https://example.com/target.git", "v1.0", "v1.1")
	var validation *V4ValidationError
	if updated != 0 || !errors.As(err, &validation) || validation.Code != "v4.api-version" || !strings.Contains(validation.Path, path) || !strings.Contains(validation.Msg, "apiVersion") {
		t.Fatalf("legacy bump = %d, %v", updated, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != original {
		t.Fatalf("refusal changed config: %s, %v", data, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("refusal changed metadata: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("refusal left staging residue: %v, %v", entries, err)
	}
}
