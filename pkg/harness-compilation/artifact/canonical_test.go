package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
