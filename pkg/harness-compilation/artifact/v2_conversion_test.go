package artifact

import (
	"bytes"
	"errors"
	"testing"
)

func TestArtifactV2Conversion(t *testing.T) {
	source := makeBenchmarkArtifact()
	source.Manifest.License = "MIT"
	source.Manifest.Capabilities = []string{"read", "write"}
	source.Manifest.RegistryID = "detached-registry"
	source.Manifest.RegistrySlug = "detached-slug"
	source.Manifest.Revision = source.Manifest.ContentDigest
	source.Manifest.Provenance.RegistryURL = "https://example.com/registry"
	source.Manifest.Provenance.Ref = "main"
	source.Manifest.Provenance.Path = "skills/review"
	source.Manifest.AnalyzerVersion = "detached-analyzer"
	raw, digest, err := Encode(source)
	if err != nil {
		t.Fatal(err)
	}
	ref := LegacyReference{ArchiveDigest: digest, Kind: source.Manifest.Kind, Name: source.Manifest.Name, ContentDigest: source.Manifest.ContentDigest}
	converted, expected, err := ConvertV1ToV2(raw, ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(converted.Payload, source.Payload) || converted.Manifest.License != "MIT" || converted.Manifest.Capabilities[0] != "read" || converted.Manifest.Capabilities[1] != "write" {
		t.Fatal("retained values changed")
	}
	candidate, err := EncodeV2(converted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Reference.ArtifactDigest != expected {
		t.Fatal("conversion digest differs")
	}
	if err := VerifyV1V2Equivalence(raw, ref, candidate.Bytes, candidate.Reference, nil); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ArtifactV2)
	}{
		{"asset-change", func(a *ArtifactV2) { a.Assets["prompt.md"] = []byte("changed") }},
		{"asset-add", func(a *ArtifactV2) { a.Assets["new.md"] = []byte("new") }},
		{"asset-remove", func(a *ArtifactV2) { delete(a.Assets, "prompt.md") }},
		{"asset-rename", func(a *ArtifactV2) { a.Assets["renamed.md"] = a.Assets["prompt.md"]; delete(a.Assets, "prompt.md") }},
		{"license-change", func(a *ArtifactV2) { a.Licenses["LICENSE"] = []byte("changed") }},
		{"license-add", func(a *ArtifactV2) { a.Licenses["new"] = []byte("new") }},
		{"license-remove", func(a *ArtifactV2) { delete(a.Licenses, "LICENSE") }},
		{"license-rename", func(a *ArtifactV2) { a.Licenses["renamed"] = a.Licenses["LICENSE"]; delete(a.Licenses, "LICENSE") }},
		{"manifest-license", func(a *ArtifactV2) { a.Manifest.License = "Apache-2.0" }},
		{"capability-order", func(a *ArtifactV2) { a.Manifest.Capabilities = []string{"write", "read"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, _, err := ConvertV1ToV2(raw, ref, nil)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(&a)
			candidate, err := EncodeV2(a, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyV2(candidate.Bytes, candidate.Reference, nil); err != nil {
				t.Fatalf("candidate must be independently valid: %v", err)
			}
			if err := VerifyV1V2Equivalence(raw, ref, candidate.Bytes, candidate.Reference, nil); !errors.Is(err, ErrProtectedEntryMismatch) {
				t.Fatalf("want equivalence error, got %v", err)
			}
		})
	}
	wrong := ref
	wrong.Name = "other"
	if _, _, err := ConvertV1ToV2(raw, wrong, nil); !errors.Is(err, ErrArtifactIdentityMismatch) {
		t.Fatalf("want identity error, got %v", err)
	}
	corrupt := bytes.Clone(raw)
	corrupt[len(corrupt)/2] ^= 1
	if _, _, err := ConvertV1ToV2(corrupt, ref, nil); err == nil {
		t.Fatal("accepted corrupt source")
	}
}
