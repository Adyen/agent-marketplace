package artifact

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCollectionManifestRoundTrip(t *testing.T) {
	col := ir.NewCollection("defaults", "1.0.0", "Default collection", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
		{Kind: ir.KindCommand, Name: "lint"},
	}, []string{"base"}, nil, ir.SourceRef{})

	m, err := CollectionFromIR(col)
	if err != nil {
		t.Fatalf("CollectionFromIR: %v", err)
	}

	payload, digest, err := EncodeCollectionManifest(m)
	if err != nil {
		t.Fatalf("EncodeCollectionManifest: %v", err)
	}

	got, payload2, err := DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("DecodeCollectionManifest: %v", err)
	}

	if got.ContentDigest != digest {
		t.Fatalf("digest mismatch: %s vs %s", got.ContentDigest, digest)
	}

	if err := VerifyCollectionManifest(got, payload2); err != nil {
		t.Fatalf("VerifyCollectionManifest: %v", err)
	}
	_ = payload2
}

func TestCollectionManifestDeterministic(t *testing.T) {
	col := ir.NewCollection("defaults", "1.0.0", "Default collection", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "b"},
		{Kind: ir.KindSkill, Name: "a"},
	}, []string{"z", "a"}, nil, ir.SourceRef{})

	m, err := CollectionFromIR(col)
	if err != nil {
		t.Fatalf("CollectionFromIR: %v", err)
	}

	payload1, digest1, err := EncodeCollectionManifest(m)
	if err != nil {
		t.Fatalf("EncodeCollectionManifest: %v", err)
	}

	payload2, digest2, err := EncodeCollectionManifest(m)
	if err != nil {
		t.Fatalf("EncodeCollectionManifest: %v", err)
	}

	if digest1 != digest2 {
		t.Fatalf("collection manifest not deterministic: %s vs %s", digest1, digest2)
	}
	if !bytes.Equal(payload1, payload2) {
		t.Fatalf("collection payload not deterministic")
	}
}

func TestCollectionManifestValidation(t *testing.T) {
	m := CollectionManifest{SchemaVersion: "v2"}
	if _, _, err := EncodeCollectionManifest(m); err == nil {
		t.Fatalf("expected validation error for unsupported schema")
	}

	m = CollectionManifest{SchemaVersion: CollectionSchemaV1, Members: []CollectionMemberEntry{{Kind: "rule", Name: "x"}}}
	if _, _, err := EncodeCollectionManifest(m); err == nil {
		t.Fatalf("expected validation error for invalid kind")
	}
}

func TestCollectionFromIRAllKinds(t *testing.T) {
	col := ir.NewCollection("defaults", "1.0.0", "Default collection", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
		{Kind: ir.KindCommand, Name: "lint"},
		{Kind: ir.KindSubagent, Name: "planner"},
		{Kind: ir.KindHook, Name: "pre-tool"},
		{Kind: ir.KindMcpServer, Name: "server"},
		{Kind: ir.KindCollection, Name: "base"},
	}, []string{"base"}, nil, ir.SourceRef{})

	m, err := CollectionFromIR(col)
	if err != nil {
		t.Fatalf("CollectionFromIR: %v", err)
	}

	want := map[string]string{
		"skill":      "review",
		"command":    "lint",
		"subagent":   "planner",
		"hook":       "pre-tool",
		"mcpserver":  "server",
		"collection": "base",
	}
	if len(m.Members) != len(want) {
		t.Fatalf("expected %d members, got %d", len(want), len(m.Members))
	}
	got := make(map[string]string, len(m.Members))
	for _, mem := range m.Members {
		got[mem.Kind] = mem.Name
	}
	for kind, name := range want {
		if got[kind] != name {
			t.Fatalf("member %q = %q, want %q", kind, got[kind], name)
		}
	}
	if len(m.Includes) != 1 || m.Includes[0] != "base" {
		t.Fatalf("includes = %v, want [base]", m.Includes)
	}
}

func TestVerifyCollectionManifestRejectsTrailingWhitespace(t *testing.T) {
	payload, _, err := EncodeCollectionManifest(CollectionManifest{
		SchemaVersion:    CollectionSchemaV1,
		Name:             "bundle",
		Members:          []CollectionMemberEntry{{Kind: "skill", Name: "review"}},
		SupportedSchemas: []string{CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range [][]byte{{'\n'}, {' ', '\n'}} {
		tampered := append(append([]byte(nil), payload...), suffix...)
		if err := VerifyCollectionManifest(manifest, tampered); err == nil || !strings.Contains(err.Error(), "not canonical") {
			t.Fatalf("VerifyCollectionManifest suffix %q error = %v, want strict canonical rejection", suffix, err)
		}
	}
}

func TestCollectionManifestContentDigestExcludesDigest(t *testing.T) {
	col := ir.NewCollection("defaults", "1.0.0", "Default collection", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
	}, nil, nil, ir.SourceRef{})

	m, err := CollectionFromIR(col)
	if err != nil {
		t.Fatalf("CollectionFromIR: %v", err)
	}

	payload, digest, err := EncodeCollectionManifest(m)
	if err != nil {
		t.Fatalf("EncodeCollectionManifest: %v", err)
	}

	// Tamper with the stored digest: verification must fail because the payload
	// (with content_digest cleared) hashes to the original digest, not the tampered one.
	got, _, err := DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("DecodeCollectionManifest: %v", err)
	}
	if got.ContentDigest != digest {
		t.Fatalf("decoded digest = %s, want %s", got.ContentDigest, digest)
	}

	got.ContentDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := VerifyCollectionManifest(got, payload); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestCollectionManifestRejectsDuplicateMembers(t *testing.T) {
	m := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "dup",
		Members: []CollectionMemberEntry{
			{Kind: "skill", Name: "review"},
			{Kind: "skill", Name: "review"},
		},
	}
	if _, _, err := EncodeCollectionManifest(m); err == nil || !strings.Contains(err.Error(), "duplicate collection member") {
		t.Fatalf("expected duplicate member error, got %v", err)
	}
}

func TestValidateFlatMirrorManifestRequiresExactLeaves(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	base := CollectionManifest{
		SchemaVersion:    CollectionSchemaV1,
		Name:             "bundle",
		SupportedSchemas: []string{CollectionSchemaV1},
		Members:          []CollectionMemberEntry{{Kind: "skill", Name: "review", SourceID: "source", Revision: digest}},
	}
	cases := []struct {
		name   string
		mutate func(*CollectionManifest)
		want   string
	}{
		{"missing source", func(m *CollectionManifest) { m.Members[0].SourceID = "" }, "source_id"},
		{"missing revision", func(m *CollectionManifest) { m.Members[0].Revision = "" }, "revision"},
		{"nested member", func(m *CollectionManifest) { m.Members[0].Kind = "collection" }, "nested"},
		{"includes", func(m *CollectionManifest) { m.Includes = []string{"base"} }, "includes"},
		{"duplicate", func(m *CollectionManifest) { m.Members = append(m.Members, m.Members[0]) }, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			m.Members = append([]CollectionMemberEntry(nil), base.Members...)
			tc.mutate(&m)
			if err := ValidateFlatMirrorManifest(m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateFlatMirrorManifest error = %v, want %q", err, tc.want)
			}
		})
	}
}
