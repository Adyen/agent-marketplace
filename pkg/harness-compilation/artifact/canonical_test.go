package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/canonical-artifact-round-trip
// spec: marketplace-api/payload-contains-resource-name
func TestCanonicalArtifactRoundTripForKinds(t *testing.T) {
	cases := []struct {
		name string
		r    ir.Resource
	}{
		{"skill", ir.NewSkill("review", "Review code", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})},
		{"command", ir.NewCommand("lint", "Lint", "echo lint", nil, ir.SourceRef{})},
		{"subagent", ir.NewSubagent("planner", "Plan", "body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})},
		{"hook", ir.NewHook("pre-tool", "PreToolUse", "echo", "command", nil, ir.SourceRef{})},
		{"mcpserver", ir.NewMcpServer("server", "npx", []string{"-y", "@server"}, nil, nil, ir.SourceRef{})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := FromIR(tc.r)
			if err != nil {
				t.Fatalf("FromIR: %v", err)
			}
			if a.Manifest.Kind != tc.name {
				t.Fatalf("kind = %q, want %q", a.Manifest.Kind, tc.name)
			}
			if a.Manifest.Name != tc.r.Name() {
				t.Fatalf("name = %q, want %q", a.Manifest.Name, tc.r.Name())
			}
			if !strings.Contains(string(a.Payload), tc.r.Name()) {
				t.Fatalf("payload missing resource name: %s", a.Payload)
			}

			raw := a.rawArchive
			a2, raw2, err := Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !bytes.Equal(a.Payload, a2.Payload) {
				t.Fatalf("payload mismatch")
			}
			if a2.Manifest.ArchiveDigest != a.Manifest.ArchiveDigest {
				t.Fatalf("archive digest mismatch: %s vs %s", a2.Manifest.ArchiveDigest, a.Manifest.ArchiveDigest)
			}
			if err := a2.VerifyArchiveDigest(raw2); err != nil {
				t.Fatalf("VerifyArchiveDigest: %v", err)
			}
			if err := a2.VerifyContentDigest(); err != nil {
				t.Fatalf("VerifyContentDigest: %v", err)
			}
			if err := a2.ValidateArchive(nil); err != nil {
				t.Fatalf("ValidateArchive: %v", err)
			}
		})
	}
}

func TestCanonicalProvenanceRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		r    ir.Resource
		want string
		key  string
	}{
		{"hook", ir.NewHookWithOptions("h", "SessionEnd", "x", "command", nil, ir.SourceRef{}, ir.WithHookSource("codex")), "codex", "HookSource"},
		{"subagent", ir.NewSubagentWithOptions("a", "", "", "", nil, "", nil, "", nil, ir.SourceRef{}, ir.WithSubagentToolSource("devin")), "devin", "ToolSource"},
		{"skill", ir.NewSkillWithOptions("s", "", "", nil, nil, ir.SourceRef{}, ir.WithSkillToolSource("gemini")), "gemini", "ToolSource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := FromIR(tc.r)
			if err != nil {
				t.Fatalf("FromIR: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(a.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if payload[tc.key] != tc.want {
				t.Fatalf("payload[%q] = %v, want %q", tc.key, payload[tc.key], tc.want)
			}
			back, err := DecodeResource(tc.name, a.Payload)
			if err != nil {
				t.Fatalf("DecodeResource: %v", err)
			}
			var got string
			switch r := back.(type) {
			case *ir.Hook:
				got = r.HookSource
			case *ir.Subagent:
				got = r.ToolSource
			case *ir.Skill:
				got = r.ToolSource
			}
			if got != tc.want {
				t.Fatalf("round-tripped provenance = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalProvenanceExtraCollisionSkipped(t *testing.T) {
	hook := ir.NewHook("h", "Stop", "x", "command", map[string]any{"hooksource": "spoofed"}, ir.SourceRef{})
	hook.HookSource = "codex"
	a, err := FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(a.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["HookSource"] != "codex" {
		t.Fatalf("extra collision leaked into payload: %v", payload)
	}
}

func TestCanonicalArtifactValidateArchiveRejectsAdversarialManifestFields(t *testing.T) {
	base, err := FromIR(ir.NewSkill("review", "Review code", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	tests := []struct {
		name string
		edit func(*CanonicalArtifact)
		want string
	}{
		{name: "schema", edit: func(a *CanonicalArtifact) { a.Manifest.SchemaVersion = "v99" }, want: "unsupported schema"},
		{name: "finding severity", edit: func(a *CanonicalArtifact) {
			a.Manifest.Findings = []CanonicalFinding{{Code: "scanner", Severity: "critical", PolicyOutcome: "warn"}}
		}, want: "severity"},
		{name: "policy outcome", edit: func(a *CanonicalArtifact) {
			a.Manifest.Findings = []CanonicalFinding{{Code: "scanner", Severity: "warn", PolicyOutcome: "escalate"}}
		}, want: "policy_outcome"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artifact := base
			artifact.rawArchive = nil
			tc.edit(&artifact)
			if err := artifact.ValidateArchive(nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateArchive error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCanonicalArtifactDeterministic(t *testing.T) {
	skill := ir.NewSkill("review", "Review code", "Review the code carefully.", []string{"user"}, nil, ir.SourceRef{})
	a1, err := FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a2, err := FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	if a1.Manifest.ArchiveDigest != a2.Manifest.ArchiveDigest {
		t.Fatalf("artifact digest not deterministic: %s vs %s", a1.Manifest.ArchiveDigest, a2.Manifest.ArchiveDigest)
	}
	if !bytes.Equal(a1.rawArchive, a2.rawArchive) {
		t.Fatalf("artifact raw archive not deterministic")
	}
}

func TestCanonicalManifestRegistryIdentityJSON(t *testing.T) {
	manifest := CanonicalManifest{RegistryID: "registry-id", RegistrySlug: "registry-slug"}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal canonical manifest: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"registry_id":"registry-id"`)) || !bytes.Contains(payload, []byte(`"registry_slug":"registry-slug"`)) {
		t.Fatalf("registry identity fields missing from canonical manifest: %s", payload)
	}
}

func TestCanonicalArtifactEncodeRejectsBadPaths(t *testing.T) {
	a := CanonicalArtifact{
		Manifest: CanonicalManifest{
			SchemaVersion:   CanonicalArtifactSchemaV1,
			ArtifactVersion: CanonicalArtifactSchemaV1,
			Kind:            "skill",
			Name:            "review",
			Revision:        "v1",
			ContentDigest:   ContentDigest([]byte("payload")),
			ArchiveDigest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Provenance:      CanonicalProvenance{Generator: "test"},
		},
		Payload: []byte("payload"),
		Assets: map[string][]byte{
			"../etc/passwd": []byte("bad"),
		},
	}

	if _, _, err := Encode(a); err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("expected path traversal error, got %v", err)
	}

	a.Assets = map[string][]byte{"image.png": []byte("ok")}
	raw, _, err := Encode(a)
	if err != nil {
		t.Fatalf("Encode valid archive: %v", err)
	}

	_, _, err = Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode valid archive: %v", err)
	}
}

// spec: marketplace-api/security-rejection-during-decode
func TestCanonicalArtifactDecodeRejectsLinks(t *testing.T) {
	raw := writeTarGz(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Name:     "link",
			Typeflag: tar.TypeSymlink,
			Linkname: "/etc/passwd",
			Mode:     0o777,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
	})

	if _, _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

// spec: marketplace-api/security-rejection-during-decode
func TestCanonicalArtifactDecodeRejectsHardLinks(t *testing.T) {
	raw := writeTarGz(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Name:     "hardlink",
			Typeflag: tar.TypeLink,
			Linkname: "resource/skill.json",
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
	})

	if _, _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("expected hard link rejection, got %v", err)
	}
}

// spec: marketplace-api/security-rejection-during-decode
func TestCanonicalArtifactDecodeRejectsAbsolutePaths(t *testing.T) {
	raw := writeTarGz(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Name:     "/etc/passwd",
			Size:     0,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
	})

	if _, _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute path rejection, got %v", err)
	}
}

// spec: marketplace-api/security-rejection-during-decode
func TestCanonicalArtifactDecodeRejectsPathTraversal(t *testing.T) {
	raw := writeTarGz(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Name:     "foo/../../etc/passwd",
			Size:     0,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
	})

	if _, _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected path traversal rejection, got %v", err)
	}
}

// spec: marketplace-api/unsafe-entry-name-is-not-echoed
func TestCanonicalArtifactDecodeRejectionOmitsEntryName(t *testing.T) {
	for name, reason := range map[string]string{
		"/etc/unsafe-entry":           "absolute",
		"foo/../../etc/unsafe-entry": "traversal",
	} {
		raw := writeTarGz(t, func(tw *tar.Writer) {
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
		})
		_, _, err := Decode(bytes.NewReader(raw))
		if err == nil || !strings.Contains(err.Error(), reason) || strings.Contains(err.Error(), "unsafe-entry") {
			t.Fatalf("Decode(%q) error = %v, want %s rejection without the entry name", name, err, reason)
		}
	}
}

// spec: marketplace-api/security-rejection-during-decode
func TestCanonicalArtifactDecodeRejectsDeviceFiles(t *testing.T) {
	raw := writeTarGz(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Name:     "dev/block",
			Typeflag: tar.TypeBlock,
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
	})

	if _, _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected device file rejection, got %v", err)
	}
}

func TestCanonicalArtifactVerifyRejectsBadContentDigest(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a.Payload = []byte("tampered")

	if err := a.VerifyContentDigest(); err == nil || !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("expected content digest mismatch, got %v", err)
	}
}

// spec: marketplace-api/archive-digest-verification-catches-archive-tampering
func TestCanonicalArtifactVerifyRejectsBadArchiveDigest(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	// Mutate the stored archive digest to a different valid-looking value.
	a.Manifest.ArchiveDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := a.VerifyArchiveDigest(a.rawArchive); err == nil || !strings.Contains(err.Error(), "archive digest mismatch") {
		t.Fatalf("expected archive digest mismatch, got %v", err)
	}

	// Keep a valid manifest but corrupt the raw archive bytes.
	good, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	badRaw := append([]byte(nil), good.rawArchive...)
	badRaw = append(badRaw, 0xff)
	if err := good.VerifyArchiveDigest(badRaw); err == nil || !strings.Contains(err.Error(), "raw archive bytes do not match") {
		t.Fatalf("expected raw archive mismatch, got %v", err)
	}
}

// spec: marketplace-api/oversized-resource-rejection
func TestCanonicalArtifactValidateRejectsOversizedPayload(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	limits := &ArchiveLimits{
		MaxPayloadBytes: 1,
		MaxAssetFiles:   256,
		MaxAssetBytes:   2 * 1024 * 1024,
	}
	if err := a.ValidateArchive(limits); err == nil || !strings.Contains(err.Error(), "payload exceeds") {
		t.Fatalf("expected oversized payload rejection, got %v", err)
	}
}

// spec: marketplace-api/oversized-resource-rejection
func TestCanonicalArtifactValidateRejectsOversizedAsset(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a.Assets = map[string][]byte{"big.png": make([]byte, 1024)}

	// Re-encode so the raw archive matches the updated artifact.
	raw, digest, err := Encode(a)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	a.rawArchive = raw
	a.Manifest.ArchiveDigest = digest

	limits := &ArchiveLimits{
		MaxPayloadBytes: 8 * 1024 * 1024,
		MaxAssetFiles:   256,
		MaxAssetBytes:   512,
	}
	if err := a.ValidateArchive(limits); err == nil || !strings.Contains(err.Error(), "big.png exceeds") {
		t.Fatalf("expected oversized asset rejection, got %v", err)
	}
}

func TestCanonicalArtifactEncodeRejectsInvalidManifest(t *testing.T) {
	a := CanonicalArtifact{
		Manifest: CanonicalManifest{
			SchemaVersion: CanonicalArtifactSchemaV1,
			Kind:          "skill",
			Name:          "review",
			// Missing ContentDigest.
		},
		Payload: []byte("payload"),
	}

	if _, _, err := Encode(a); err == nil {
		t.Fatal("expected Encode to reject invalid manifest")
	}
}

// writeTarGz creates a gzip-compressed tar archive using prepare to populate it.
func writeTarGz(t *testing.T, prepare func(tw *tar.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	gw.Name = ""
	gw.Comment = ""
	gw.Extra = nil
	gw.ModTime = time.Time{}
	gw.OS = 0xff

	tw := tar.NewWriter(gw)
	prepare(tw)

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// spec: marketplace-api/payload-contains-resource-name
func TestFromIRPreservesExtraAndNameWins(t *testing.T) {
	extra := map[string]any{
		"x-agent-marketplace-foo": "bar",
		"nested":                  map[interface{}]interface{}{"k": "v"},
	}
	s := ir.NewSkill("review", "Review", "body", nil, extra, ir.SourceRef{})
	a, err := FromIR(s)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(a.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got := payload["x-agent-marketplace-foo"]; got != "bar" {
		t.Fatalf("extra key missing: got %v", got)
	}
	nested, ok := payload["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested map not normalized to map[string]any: %T", payload["nested"])
	}
	if nested["k"] != "v" {
		t.Fatalf("nested value missing: got %v", nested["k"])
	}
	if payload["name"] != "review" {
		t.Fatalf("name = %v, want %q", payload["name"], "review")
	}

	// Extra containing "name" should not override r.Name().
	override := map[string]any{
		"name":   "wrong",
		"custom": "value",
	}
	s2 := ir.NewSkill("canonical-name", "Canonical", "body", nil, override, ir.SourceRef{})
	a2, err := FromIR(s2)
	if err != nil {
		t.Fatalf("FromIR with name override: %v", err)
	}
	var payload2 map[string]any
	if err := json.Unmarshal(a2.Payload, &payload2); err != nil {
		t.Fatalf("unmarshal payload2: %v", err)
	}
	if payload2["name"] != "canonical-name" {
		t.Fatalf("name override: got %v, want %q", payload2["name"], "canonical-name")
	}
	if payload2["custom"] != "value" {
		t.Fatalf("custom extra missing: got %v", payload2["custom"])
	}
}

func TestCanonicalArtifactEncodeRejectsNestedPaths(t *testing.T) {
	a := CanonicalArtifact{
		Manifest: CanonicalManifest{
			SchemaVersion:   CanonicalArtifactSchemaV1,
			ArtifactVersion: CanonicalArtifactSchemaV1,
			Kind:            "skill",
			Name:            "review",
			Revision:        "v1",
			ContentDigest:   ContentDigest([]byte("payload")),
			ArchiveDigest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Provenance:      CanonicalProvenance{Generator: "test"},
		},
		Payload: []byte("payload"),
		Assets: map[string][]byte{
			"nested/image.png": []byte("ok"),
		},
	}

	if _, _, err := Encode(a); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("expected nested path rejection, got %v", err)
	}

	a.Assets = map[string][]byte{"image.png": []byte("ok")}
	raw, _, err := Encode(a)
	if err != nil {
		t.Fatalf("Encode valid archive: %v", err)
	}

	_, _, err = Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode valid archive: %v", err)
	}
}

func TestCanonicalManifestValidateFindings(t *testing.T) {
	validFinding := CanonicalFinding{
		Code:          "marketplace.find.tool-grant",
		Severity:      "warn",
		PolicyOutcome: "warn",
	}
	m := CanonicalManifest{
		SchemaVersion:   CanonicalArtifactSchemaV1,
		ArtifactVersion: CanonicalArtifactSchemaV1,
		Kind:            "skill",
		Name:            "review",
		ContentDigest:   ContentDigest([]byte("payload")),
		ArchiveDigest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Findings:        []CanonicalFinding{validFinding},
		Provenance:      CanonicalProvenance{Generator: "test"},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	cases := []struct {
		name    string
		finding CanonicalFinding
		wantErr string
	}{
		{
			name:    "missing code",
			finding: CanonicalFinding{Severity: "warn", PolicyOutcome: "warn"},
			wantErr: "code is required",
		},
		{
			name:    "invalid policy outcome",
			finding: CanonicalFinding{Code: "x", Severity: "warn", PolicyOutcome: "block"},
			wantErr: "policy_outcome",
		},
		{
			name:    "invalid severity",
			finding: CanonicalFinding{Code: "x", Severity: "critical", PolicyOutcome: "warn"},
			wantErr: "severity",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm := m
			cm.Findings = []CanonicalFinding{tc.finding}
			err := cm.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}

// spec: marketplace-api/canonical-artifact-round-trip
func TestCanonicalArtifactRoundTripCarriesAssetsLicensesAndSupport(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	a.Assets = map[string][]byte{"diagram.png": []byte("png-bytes")}
	a.Licenses = map[string][]byte{"LICENSE": []byte("MIT")}
	a.Support = map[string]SupportFile{
		"run.sh":        {Data: []byte("#!/bin/sh\necho hi\n"), Executable: true},
		"docs/guide.md": {Data: []byte("guide")},
	}
	raw, digest, err := Encode(a)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	again, digestAgain, err := Encode(a)
	if err != nil || digestAgain != digest || !bytes.Equal(raw, again) {
		t.Fatalf("Encode is not deterministic: err=%v digest %s vs %s", err, digest, digestAgain)
	}

	got, gotRaw, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Manifest.ArchiveDigest != digest {
		t.Fatalf("decoded archive digest = %s, want %s", got.Manifest.ArchiveDigest, digest)
	}
	if !bytes.Equal(got.Payload, a.Payload) || !reflect.DeepEqual(got.Assets, a.Assets) ||
		!reflect.DeepEqual(got.Licenses, a.Licenses) || !reflect.DeepEqual(got.Support, a.Support) {
		t.Fatalf("decoded artifact differs:\npayload %q\nassets %v\nlicenses %v\nsupport %v", got.Payload, got.Assets, got.Licenses, got.Support)
	}
	if err := got.VerifyContentDigest(); err != nil {
		t.Fatalf("VerifyContentDigest: %v", err)
	}
	if err := got.VerifyArchiveDigest(gotRaw); err != nil {
		t.Fatalf("VerifyArchiveDigest: %v", err)
	}
	if err := got.ValidateArchive(nil); err != nil {
		t.Fatalf("ValidateArchive: %v", err)
	}
}
