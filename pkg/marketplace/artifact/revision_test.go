package artifact

import (
	"bytes"
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
