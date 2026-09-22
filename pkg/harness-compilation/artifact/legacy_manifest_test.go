package artifact

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

func TestLegacyManifestEncoding(t *testing.T) {
	cases := []CanonicalManifest{{}, makeBenchmarkArtifact().Manifest, {
		SchemaVersion: "v1", ArtifactVersion: "v1", Kind: "skill", Name: "é<>&/\u2028\u2029\x00\b\t\n\f\r\x01\xff�", RegistryID: "id", RegistrySlug: "slug", Revision: "revision", ContentDigest: "digest", ArchiveDigest: "archive", License: "MIT", AnalyzerVersion: "analyzer", Capabilities: []string{"write", "read"}, Findings: []CanonicalFinding{{Code: "code", Severity: "warn", Message: "message", Line: 2, Column: 3, File: "file", PolicyOutcome: "warn", Analyzer: "a", AnalyzerVersion: "v"}}, Provenance: CanonicalProvenance{RegistryURL: "url", Ref: "ref", Path: "path", Generator: "generator"}, SupportedSchemas: []string{"v1"},
	}, {Capabilities: []string{}, Findings: []CanonicalFinding{}, SupportedSchemas: []string{}}}
	for i, m := range cases {
		got, err := legacyManifestBytes(m)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 && !bytes.Contains(got, []byte(`"name":"é\u003c\u003e\u0026/\u2028\u2029\u0000\b\t\n\f\r\u0001\ufffd�"`)) {
			t.Fatalf("legacy string bytes changed: %s", got)
		}
		if runtime.Version() != "go1.26.6" {
			continue
		}
		want, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("case %d: got %s want %s", i, got, want)
		}
	}
}
