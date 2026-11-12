package sourcerevision

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

// Each manifest entry commits its own plugin format version, so a version
// change in one entry changes the revision digest.
func TestPluginManifestEntryFormatVersionCommitted(t *testing.T) {
	manifest := pluginSourceManifest{
		SchemaVersion: "plugin-v1",
		Generator:     "test",
		Format:        "claude-marketplace",
		Entries: []pluginManifestEntry{
			{Name: "a", Digest: "sha256:aa", FormatVersion: "1.0.0"},
			{Name: "b", Digest: "sha256:bb", FormatVersion: "2.0.0"},
		},
		Resources: []revisionResource{},
	}
	payload, digest, err := encodePluginManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if !strings.Contains(string(payload), `"format_version":"1.0.0"`) || !strings.Contains(string(payload), `"format_version":"2.0.0"`) {
		t.Fatalf("manifest must commit each entry's format version:\n%s", payload)
	}
	manifest.Entries[1].FormatVersion = "3.0.0"
	_, changed, err := encodePluginManifest(manifest)
	if err != nil {
		t.Fatalf("encode changed manifest: %v", err)
	}
	if changed == digest {
		t.Fatal("a version change in one entry must change the revision digest")
	}
}

// Two selected catalog entries resolving to the same external origin, ref,
// and subdirectory are refused before any acquisition or write.
func TestCheckSharedExternalOriginsRefusesCollision(t *testing.T) {
	externals := []PluginExternalEntry{
		{Name: "first", Source: plugindecode.CatalogSource{Kind: "git", URL: "https://example.com/team/repo.git", Ref: "main", Subdirectory: "pkg"}},
		{Name: "second", Source: plugindecode.CatalogSource{Kind: "git", URL: "https://example.com/team/repo.git", Ref: "main", Subdirectory: "pkg"}},
		{Name: "other", Source: plugindecode.CatalogSource{Kind: "git", URL: "https://example.com/team/repo.git", Ref: "main", Subdirectory: "other"}},
	}
	err := CheckSharedExternalOrigins(externals)
	if err == nil {
		t.Fatal("two selected entries sharing an external origin must be refused")
	}
	if !strings.Contains(err.Error(), "plugin.catalog.shared-origin") ||
		!strings.Contains(err.Error(), `"first"`) || !strings.Contains(err.Error(), `"second"`) {
		t.Fatalf("refusal must carry the code and name both entries: %v", err)
	}
}
