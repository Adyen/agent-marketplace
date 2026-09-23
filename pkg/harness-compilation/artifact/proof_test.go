package artifact

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	regressionArtifactPath = "testdata/regression-using-git-worktrees.tar.gz"
	regressionProofPath    = "testdata/regression-using-git-worktrees.proof.tar.gz"
	regressionProofDigest  = "sha256:342126ac63b2e4a0081c39a56de1483ae4d74b6accf895eaa678d129d03741cb"
)

func TestDecodeProofAcceptsGo126ProductionBytesAcrossToolchains(t *testing.T) {
	proof := readProofFixture(t, regressionProofPath)
	if got := ArchiveDigest(proof); got != regressionProofDigest {
		t.Fatalf("proof fixture digest = %s, want %s", got, regressionProofDigest)
	}
	finalRaw := readProofFixture(t, regressionArtifactPath)
	final, _, err := Decode(bytes.NewReader(finalRaw))
	if err != nil {
		t.Fatalf("Decode final fixture: %v", err)
	}
	if final.Manifest.ArchiveDigest != regressionProofDigest {
		t.Fatalf("final fixture archive digest = %s, want %s", final.Manifest.ArchiveDigest, regressionProofDigest)
	}
	a, err := DecodeProof(proof, regressionProofDigest, nil)
	if err != nil {
		t.Fatalf("DecodeProof: %v", err)
	}
	if a.Manifest.ArchiveDigest != regressionProofDigest {
		t.Fatalf("decoded archive digest = %s, want %s", a.Manifest.ArchiveDigest, regressionProofDigest)
	}
	if a.Manifest.Name != "using-git-worktrees" || a.Manifest.Kind != "skill" {
		t.Fatalf("decoded identity = %s/%s", a.Manifest.Kind, a.Manifest.Name)
	}
}

func TestEncodeWithProofReturnsDigestPreimage(t *testing.T) {
	a, err := FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	raw, proof, digest, err := EncodeWithProof(a)
	if err != nil {
		t.Fatalf("EncodeWithProof: %v", err)
	}
	if ArchiveDigest(proof) != digest {
		t.Fatalf("proof digest = %s, want %s", ArchiveDigest(proof), digest)
	}
	final, _, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode final archive: %v", err)
	}
	if final.Manifest.ArchiveDigest != digest {
		t.Fatalf("final archive digest = %s, want %s", final.Manifest.ArchiveDigest, digest)
	}
	verified, err := DecodeProof(proof, digest, nil)
	if err != nil {
		t.Fatalf("DecodeProof generated proof: %v", err)
	}
	if verified.Manifest.ArchiveDigest != digest {
		t.Fatalf("verified digest = %s, want %s", verified.Manifest.ArchiveDigest, digest)
	}
}

func TestDecodeProofRejectsUnboundAndMalformedArchives(t *testing.T) {
	proof := readProofFixture(t, regressionProofPath)
	final := readProofFixture(t, regressionArtifactPath)

	t.Run("wrong digest", func(t *testing.T) {
		wrong := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		_, err := DecodeProof(proof, wrong, nil)
		if !errors.Is(err, ErrProofDigestMismatch) {
			t.Fatalf("error = %v, want ErrProofDigestMismatch", err)
		}
	})

	t.Run("final archive", func(t *testing.T) {
		_, err := DecodeProof(final, ArchiveDigest(final), nil)
		if !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("error = %v, want ErrInvalidProof", err)
		}
	})

	t.Run("duplicate manifest", func(t *testing.T) {
		a, err := DecodeProof(proof, regressionProofDigest, nil)
		if err != nil {
			t.Fatalf("decode control: %v", err)
		}
		a.Manifest.ArchiveDigest = archiveDigestPlaceholder
		manifest, err := json.Marshal(a.Manifest)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		duplicate := proofArchive(t,
			proofMember{name: CanonicalManifestPath, data: manifest},
			proofMember{name: CanonicalManifestPath, data: manifest},
			proofMember{name: PayloadPath(a.Manifest.Kind), data: a.Payload},
		)
		_, err = DecodeProof(duplicate, ArchiveDigest(duplicate), nil)
		if !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("error = %v, want ErrInvalidProof", err)
		}
	})

	t.Run("unexpected member", func(t *testing.T) {
		a, err := DecodeProof(proof, regressionProofDigest, nil)
		if err != nil {
			t.Fatalf("decode control: %v", err)
		}
		a.Manifest.ArchiveDigest = archiveDigestPlaceholder
		manifest, err := json.Marshal(a.Manifest)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		withUnexpected := proofArchive(t,
			proofMember{name: CanonicalManifestPath, data: manifest},
			proofMember{name: CanonicalPayloadDir, dir: true},
			proofMember{name: PayloadPath(a.Manifest.Kind), data: a.Payload},
			proofMember{name: "unexpected.txt", data: []byte("not covered")},
		)
		_, err = DecodeProof(withUnexpected, ArchiveDigest(withUnexpected), nil)
		if !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("error = %v, want ErrInvalidProof", err)
		}
	})

	t.Run("cumulative size limit", func(t *testing.T) {
		_, err := DecodeProof(proof, regressionProofDigest, &ArchiveLimits{MaxTotalBytes: 1})
		if !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("error = %v, want ErrInvalidProof", err)
		}
	})
}

func readProofFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return raw
}

type proofMember struct {
	name string
	data []byte
	dir  bool
}

func proofArchive(t *testing.T, members ...proofMember) []byte {
	t.Helper()
	return writeTarGz(t, func(tw *tar.Writer) {
		for _, member := range members {
			header := &tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.data)), Typeflag: tar.TypeReg}
			if member.dir {
				header.Mode = 0o755
				header.Size = 0
				header.Typeflag = tar.TypeDir
			}
			if err := tw.WriteHeader(header); err != nil {
				t.Fatalf("write %s header: %v", member.name, err)
			}
			if !member.dir {
				_, err := tw.Write(member.data)
				if err != nil {
					t.Fatalf("write %s body: %v", member.name, err)
				}
			}
		}
	})
}
