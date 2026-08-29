package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSourceRevisionManifestRoundTrip(t *testing.T) {
	m := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		SourceURL:     "https://example.com/repo.git",
		Ref:           "main",
		Path:          "skills",
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "review", ContentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			{Kind: "command", Name: "lint", ContentDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		},
	}

	payload, digest, err := EncodeSourceRevisionManifest(m)
	if err != nil {
		t.Fatalf("EncodeSourceRevisionManifest: %v", err)
	}

	got, payload2, err := DecodeSourceRevisionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("DecodeSourceRevisionManifest: %v", err)
	}

	if got.RevisionDigest != digest {
		t.Fatalf("digest mismatch: %s vs %s", got.RevisionDigest, digest)
	}
	if got.SourceURL != "" || got.Ref != "" || got.Path != "" {
		t.Fatalf("wire manifest exposed mutable source metadata: %#v", got)
	}

	if err := VerifySourceRevisionManifest(got, payload2); err != nil {
		t.Fatalf("VerifySourceRevisionManifest: %v", err)
	}
}

func TestSourceRevisionManifestDeterministic(t *testing.T) {
	m := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "b"},
			{Kind: "skill", Name: "a"},
		},
	}

	payload1, digest1, err := EncodeSourceRevisionManifest(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	payload2, digest2, err := EncodeSourceRevisionManifest(m)
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

func TestSourceRevisionManifestDigestIgnoresMutableSourceMetadata(t *testing.T) {
	base := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     "test",
		Resources:     []RevisionResource{{Kind: "skill", Name: "review", ContentDigest: "sha256:" + strings.Repeat("a", 64)}},
	}
	withSourceMetadata := base
	withSourceMetadata.SourceURL = "https://user:secret@example.com/repo.git"
	withSourceMetadata.Ref = "refs/heads/main"
	withSourceMetadata.Path = "skills"

	basePayload, baseDigest, err := EncodeSourceRevisionManifest(base)
	if err != nil {
		t.Fatalf("encode base: %v", err)
	}
	metadataPayload, metadataDigest, err := EncodeSourceRevisionManifest(withSourceMetadata)
	if err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	if baseDigest != metadataDigest || !bytes.Equal(basePayload, metadataPayload) {
		t.Fatalf("mutable source metadata changed canonical manifest: %s/%s\n%s\n%s", baseDigest, metadataDigest, basePayload, metadataPayload)
	}
	for _, field := range []string{"source_url", "ref", "path", "secret"} {
		if bytes.Contains(metadataPayload, []byte(field)) {
			t.Fatalf("canonical manifest exposed %q: %s", field, metadataPayload)
		}
	}
}

func TestSourceRevisionManifestValidation(t *testing.T) {
	m := SourceRevisionManifest{SchemaVersion: "v2"}
	if _, _, err := EncodeSourceRevisionManifest(m); err == nil {
		t.Fatal("expected validation error for unsupported schema")
	}

	m = SourceRevisionManifest{SchemaVersion: SourceRevisionSchemaV1, Generator: "test", Resources: []RevisionResource{{Kind: "skill"}}}
	if _, _, err := EncodeSourceRevisionManifest(m); err == nil {
		t.Fatal("expected validation error for missing name")
	}
}

func TestSourceRevisionManifestRejectDuplicateResources(t *testing.T) {
	m := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "review"},
			{Kind: "skill", Name: "review"},
		},
	}
	if _, _, err := EncodeSourceRevisionManifest(m); err == nil {
		t.Fatal("expected duplicate resource error")
	}
}

func TestVerifySourceRevisionManifestRejectsNonCanonicalPayload(t *testing.T) {
	m := SourceRevisionManifest{
		SchemaVersion: SourceRevisionSchemaV1,
		Generator:     "test",
		Resources: []RevisionResource{
			{Kind: "skill", Name: "a", ContentDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}

	canonical, digest, err := EncodeSourceRevisionManifest(m)
	if err != nil {
		t.Fatalf("EncodeSourceRevisionManifest: %v", err)
	}

	// Pretty-printed JSON has the correct digest but is not canonical.
	var indented bytes.Buffer
	if err := json.Indent(&indented, canonical, "", "  "); err != nil {
		t.Fatalf("json.Indent: %v", err)
	}

	var dec SourceRevisionManifest
	if err := json.Unmarshal(indented.Bytes(), &dec); err != nil {
		t.Fatalf("unmarshal indented payload: %v", err)
	}
	if err := VerifySourceRevisionManifest(dec, indented.Bytes()); err == nil || !strings.Contains(err.Error(), "not canonical") {
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
	if err := VerifySourceRevisionManifest(dec, reordered); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("expected non-canonical reordered keys rejection, got %v", err)
	}
}
