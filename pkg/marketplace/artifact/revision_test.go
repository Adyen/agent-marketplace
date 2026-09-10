package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRegistryRevisionManifestRoundTrip(t *testing.T) {
	m := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		RegistryURL:   "https://example.com/repo.git",
		Ref:           "main",
		Path:          "skills",
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "review", ContentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			{Kind: "command", Name: "lint", ContentDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		},
	}

	payload, digest, err := EncodeRegistryRevisionManifest(m)
	if err != nil {
		t.Fatalf("EncodeRegistryRevisionManifest: %v", err)
	}

	got, payload2, err := DecodeRegistryRevisionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("DecodeRegistryRevisionManifest: %v", err)
	}

	if got.RevisionDigest != digest {
		t.Fatalf("digest mismatch: %s vs %s", got.RevisionDigest, digest)
	}
	if got.RegistryURL != "" || got.Ref != "" || got.Path != "" {
		t.Fatalf("wire manifest exposed mutable registry metadata: %#v", got)
	}

	if err := VerifyRegistryRevisionManifest(got, payload2); err != nil {
		t.Fatalf("VerifyRegistryRevisionManifest: %v", err)
	}
}

func TestRegistryRevisionManifestDeterministic(t *testing.T) {
	m := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "b"},
			{Kind: "skill", Name: "a"},
		},
	}

	payload1, digest1, err := EncodeRegistryRevisionManifest(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	payload2, digest2, err := EncodeRegistryRevisionManifest(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if digest1 != digest2 {
		t.Fatalf("revision digest not deterministic")
	}
	if !bytes.Equal(payload1, payload2) {
		t.Fatalf("revision payload not deterministic")
	}
}

func TestRegistryRevisionManifestDigestIgnoresMutableRegistryMetadata(t *testing.T) {
	base := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		Generator:     "test",
		Resources:     []RevisionResource{{Kind: "skill", Name: "review", ContentDigest: "sha256:" + strings.Repeat("a", 64)}},
	}
	withRegistryMetadata := base
	withRegistryMetadata.RegistryURL = "https://user:secret@example.com/repo.git"
	withRegistryMetadata.Ref = "refs/heads/main"
	withRegistryMetadata.Path = "skills"

	basePayload, baseDigest, err := EncodeRegistryRevisionManifest(base)
	if err != nil {
		t.Fatalf("encode base: %v", err)
	}
	metadataPayload, metadataDigest, err := EncodeRegistryRevisionManifest(withRegistryMetadata)
	if err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	if baseDigest != metadataDigest || !bytes.Equal(basePayload, metadataPayload) {
		t.Fatalf("mutable registry metadata changed canonical manifest: %s/%s\n%s\n%s", baseDigest, metadataDigest, basePayload, metadataPayload)
	}
	for _, field := range []string{"registry_url", "ref", "path", "secret"} {
		if bytes.Contains(metadataPayload, []byte(field)) {
			t.Fatalf("canonical manifest exposed %q: %s", field, metadataPayload)
		}
	}
}

func TestRegistryRevisionManifestValidation(t *testing.T) {
	m := RegistryRevisionManifest{SchemaVersion: "v2"}
	if _, _, err := EncodeRegistryRevisionManifest(m); err == nil {
		t.Fatal("expected validation error for unsupported schema")
	}

	m = RegistryRevisionManifest{SchemaVersion: RegistryRevisionSchemaV1, Generator: "test", Resources: []RevisionResource{{Kind: "skill"}}}
	if _, _, err := EncodeRegistryRevisionManifest(m); err == nil {
		t.Fatal("expected validation error for missing name")
	}
}

func TestRegistryRevisionManifestRejectDuplicateResources(t *testing.T) {
	m := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "review"},
			{Kind: "skill", Name: "review"},
		},
	}
	if _, _, err := EncodeRegistryRevisionManifest(m); err == nil {
		t.Fatal("expected duplicate resource error")
	}
}

func TestVerifyRegistryRevisionManifestRejectsNonCanonicalPayload(t *testing.T) {
	m := RegistryRevisionManifest{
		SchemaVersion: RegistryRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "a", ContentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}

	canonical, digest, err := EncodeRegistryRevisionManifest(m)
	if err != nil {
		t.Fatalf("EncodeRegistryRevisionManifest: %v", err)
	}

	// Pretty-printed JSON has the correct digest but is not canonical.
	var indented bytes.Buffer
	if err := json.Indent(&indented, canonical, "", "  "); err != nil {
		t.Fatalf("json.Indent: %v", err)
	}

	var dec RegistryRevisionManifest
	if err := json.Unmarshal(indented.Bytes(), &dec); err != nil {
		t.Fatalf("unmarshal indented payload: %v", err)
	}
	if err := VerifyRegistryRevisionManifest(dec, indented.Bytes()); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("expected non-canonical whitespace rejection, got %v", err)
	}

	// Reordered keys with extra whitespace, but correct digest.
	reordered := []byte(fmt.Sprintf(`{
  "generator" : "test",
  "resources" : [
    {"name" : "a", "kind" : "skill", "content_digest" : "sha256:0000000000000000000000000000000000000000000000000000000000000000", "rejected" : false}
  ],
  "schema_version" : "v1",
  "revision_digest" : "%s"
}`, digest))

	if err := json.Unmarshal(reordered, &dec); err != nil {
		t.Fatalf("unmarshal reordered payload: %v", err)
	}
	if err := VerifyRegistryRevisionManifest(dec, reordered); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("expected non-canonical reordered keys rejection, got %v", err)
	}
}
