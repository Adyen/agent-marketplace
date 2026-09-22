package artifact

import (
	"bytes"
	"os"
	"testing"
)

// The regression fixture is a real production artifact: the canonical archive
// the "adyen" Marketplace instance (server v4.0.0-rc.3, built with Go 1.26)
// indexed for obra-superpowers/skill/using-git-worktrees at content revision
// sha256:74fb553a…. CLI 4.0.0, built with Go 1.27, rejected it with
// "archive digest mismatch: got sha256:f7802276…, want sha256:342126ac…"
// because Go 1.27 changed compress/flate and the archive digest covers the
// compressed stream. The digests below were recorded from the artifact's own
// manifest, its server metadata, and the served bytes, independently of the
// code under test.
const (
	regressionFixturePath = "testdata/regression-using-git-worktrees.tar.gz"

	// regressionManifestDigest is the manifest archive_digest: the digest of
	// the canonical placeholder-form archive, produced by a Go 1.26 toolchain.
	regressionManifestDigest = "sha256:342126ac63b2e4a0081c39a56de1483ae4d74b6accf895eaa678d129d03741cb"

	// regressionRawDigest is the sha256 of the raw final archive bytes as
	// stored and served.
	regressionRawDigest = "sha256:41c41003384a4787f43cc114de20ec42b70eaf559dcca71f10bb2c9b88a96b08"

	// regressionGo127Digest is the digest a Go 1.27 toolchain's compress/gzip
	// produced for the same content; verification must never accept it.
	regressionGo127Digest = "sha256:f78022762d64b4cab5e17db77c0758d0a6b531e35e28edbe400ace85654fba95"

	// regressionContentDigest pins the payload the artifact carries.
	regressionContentDigest = "sha256:74fb553af63d7b7c36be994da60d2e4d0204e07a4f813a9862157853c23b7b7b"
)

// TestCanonicalEncodingMatchesProductionArtifact re-encodes a production
// artifact with the current toolchain and requires the frozen canonical
// gzip encoding to reproduce its recorded digests byte-for-byte. It fails if
// the encoder is ever switched back to compress/gzip on a toolchain whose
// flate output differs from Go 1.26's, which is what broke every marketplace
// install on CLI 4.0.0.
func TestCanonicalEncodingMatchesProductionArtifact(t *testing.T) {
	raw, err := os.ReadFile(regressionFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	a, rawDecoded, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if got := ArchiveDigest(rawDecoded); got != regressionRawDigest {
		t.Fatalf("fixture raw digest = %s, want %s (fixture content drifted)", got, regressionRawDigest)
	}
	if a.Manifest.ArchiveDigest != regressionManifestDigest {
		t.Fatalf("manifest archive_digest = %s, want %s", a.Manifest.ArchiveDigest, regressionManifestDigest)
	}
	if a.Manifest.ContentDigest != regressionContentDigest {
		t.Fatalf("manifest content_digest = %s, want %s", a.Manifest.ContentDigest, regressionContentDigest)
	}

	// The full verification path that failed in production must pass.
	if err := a.ValidateArchive(nil); err != nil {
		t.Fatalf("ValidateArchive on production artifact: %v", err)
	}

	// Re-encoding must reproduce the served archive exactly, and the digest
	// must never be the value a changed flate implementation would produce.
	reencoded, err := encodeArtifact(a)
	if err != nil {
		t.Fatalf("re-encode artifact: %v", err)
	}
	if !bytes.Equal(reencoded, rawDecoded) {
		t.Fatalf("re-encoded archive is not byte-identical to the production archive")
	}
	if got := ArchiveDigest(reencoded); got != regressionRawDigest {
		t.Fatalf("re-encoded archive digest = %s, want %s", got, regressionRawDigest)
	}

	canon := a
	canon.Manifest.ArchiveDigest = archiveDigestPlaceholder
	canon.rawArchive = nil
	canonRaw, err := encodeArtifact(canon)
	if err != nil {
		t.Fatalf("re-encode canonical form: %v", err)
	}
	if got := ArchiveDigest(canonRaw); got != regressionManifestDigest {
		if got == regressionGo127Digest {
			t.Fatalf("canonical digest = %s, which is the Go 1.27 flate drift; the frozen encoder is not in use", got)
		}
		t.Fatalf("canonical digest = %s, want %s", got, regressionManifestDigest)
	}
}
