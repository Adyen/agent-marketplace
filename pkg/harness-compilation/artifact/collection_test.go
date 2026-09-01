package artifact

import (
	"bytes"
	"encoding/json"
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

func TestCollectionMemberRegistryIdentityJSON(t *testing.T) {
	payload, err := json.Marshal(CollectionMemberEntry{RegistryID: "registry-id"})
	if err != nil {
		t.Fatalf("marshal collection member: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"registry_id":"registry-id"`)) {
		t.Fatalf("registry identity field missing from collection member: %s", payload)
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

func TestNestedCollectionManifestPreservesIncludesLeavesAndPaths(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	manifest := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "root",
		Members: []CollectionMemberEntry{
			{Kind: "collection", Name: "base", RegistryID: "registry", Revision: digest},
			{Kind: "skill", Name: "direct", RegistryID: "registry", Revision: digest},
		},
		Includes: []string{"base"},
		Leaves: []CollectionMemberEntry{{
			Kind: "skill", Name: "review", RegistryID: "registry", Revision: digest,
			InclusionPaths: [][]string{{"root", "base"}, {"root", "security"}},
		}},
		SupportedSchemas: []string{CollectionSchemaV1},
	}
	payload, _, err := EncodeCollectionManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCollectionManifest(got); err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 2 || got.Members[0].Kind != "collection" {
		t.Fatalf("direct members were not preserved: %#v", got.Members)
	}
	if len(got.Includes) != 1 || got.Includes[0] != "base" {
		t.Fatalf("direct includes were not preserved: %#v", got.Includes)
	}
	if len(got.Leaves) != 1 || len(got.Leaves[0].InclusionPaths) != 2 {
		t.Fatalf("flattened leaf paths were not preserved: %#v", got.Leaves)
	}
}

func TestCollectionManifestRejectsConflictingLeafRevisions(t *testing.T) {
	manifest := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "root",
		Leaves: []CollectionMemberEntry{
			{Kind: "skill", Name: "review", RegistryID: "registry", Revision: "sha256:" + strings.Repeat("a", 64), InclusionPaths: [][]string{{"root"}}},
			{Kind: "skill", Name: "review", RegistryID: "registry", Revision: "sha256:" + strings.Repeat("b", 64), InclusionPaths: [][]string{{"root"}}},
		},
	}
	if err := ValidateCollectionManifest(manifest); err == nil || !strings.Contains(err.Error(), "duplicate collection leaf") {
		t.Fatalf("conflicting leaf revisions error = %v", err)
	}
}

func TestValidateAndExpandCollectionManifestGraphAllowsSameNameDistinctRevisions(t *testing.T) {
	leafDigest := "sha256:" + strings.Repeat("a", 64)
	firstDigest := "sha256:" + strings.Repeat("b", 64)
	secondDigest := "sha256:" + strings.Repeat("c", 64)
	second := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "shared",
		ContentDigest: secondDigest,
		Members:       []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest}},
		Leaves:        []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"shared"}}}},
	}
	first := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "shared",
		ContentDigest: firstDigest,
		Members:       []CollectionMemberEntry{{Kind: "collection", Name: "shared", RegistryID: "reg", Revision: secondDigest}},
		Includes:      []string{"shared"},
		Leaves:        []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"shared", "shared"}}}},
	}
	leaves, err := ValidateAndExpandCollectionManifestGraph(first, map[string]CollectionManifest{secondDigest: second})
	if err != nil {
		t.Fatalf("same-name distinct collection revisions were treated as a cycle: %v", err)
	}
	if len(leaves) != 1 || len(leaves[0].InclusionPaths) != 1 || !slicesEqual(leaves[0].InclusionPaths[0], []string{"shared", "shared"}) {
		t.Fatalf("expanded leaves = %#v", leaves)
	}
}

func TestValidateAndExpandCollectionManifestGraphRejectsStaleFlattenedLeaves(t *testing.T) {
	leafDigest := "sha256:" + strings.Repeat("a", 64)
	childDigest := "sha256:" + strings.Repeat("b", 64)
	child := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "child",
		ContentDigest: childDigest,
		Members:       []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest}},
		Leaves:        []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"child"}}}},
	}
	root := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "root",
		Members:       []CollectionMemberEntry{{Kind: "collection", Name: "child", RegistryID: "reg", Revision: childDigest}, {Kind: "skill", Name: "direct", RegistryID: "reg", Revision: leafDigest}},
		Includes:      []string{"child"},
		Leaves: []CollectionMemberEntry{
			{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"root", "child"}}},
			{Kind: "skill", Name: "extra", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"root"}}},
		},
	}
	nested := map[string]CollectionManifest{childDigest: child}
	if _, err := ValidateAndExpandCollectionManifestGraph(root, nested); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("stale flattened leaves error = %v, want missing direct leaf", err)
	}

	root.Leaves = nil
	if _, err := ValidateAndExpandCollectionManifestGraph(root, nested); err == nil || !strings.Contains(err.Error(), "flattened leaves") {
		t.Fatalf("absent flattened leaves error = %v, want fail closed", err)
	}
}

func TestValidateAndExpandCollectionManifestGraphRejectsInvalidInclusionPath(t *testing.T) {
	leafDigest := "sha256:" + strings.Repeat("a", 64)
	childDigest := "sha256:" + strings.Repeat("b", 64)
	child := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "child",
		ContentDigest: childDigest,
		Members:       []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest}},
		Leaves:        []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"child", "not-a-member"}}}},
	}
	root := CollectionManifest{
		SchemaVersion: CollectionSchemaV1,
		Name:          "root",
		Members:       []CollectionMemberEntry{{Kind: "collection", Name: "child", RegistryID: "reg", Revision: childDigest}},
		Includes:      []string{"child"},
		Leaves:        []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "reg", Revision: leafDigest, InclusionPaths: [][]string{{"root", "child", "not-a-member"}}}},
	}
	if _, err := ValidateAndExpandCollectionManifestGraph(root, map[string]CollectionManifest{childDigest: child}); err == nil || !strings.Contains(err.Error(), "inclusion paths") {
		t.Fatalf("invalid inclusion path error = %v, want graph-path rejection", err)
	}
}

func TestValidateAndExpandCollectionManifestGraphExpandsDirectLeavesWhenFlat(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	base := CollectionManifest{
		SchemaVersion:    CollectionSchemaV1,
		Name:             "bundle",
		SupportedSchemas: []string{CollectionSchemaV1},
		Members:          []CollectionMemberEntry{{Kind: "skill", Name: "review", RegistryID: "registry", Revision: digest}},
	}
	cases := []struct {
		name   string
		mutate func(*CollectionManifest)
		want   string
	}{
		{"missing registry", func(m *CollectionManifest) { m.Members[0].RegistryID = "" }, "registry_id"},
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
