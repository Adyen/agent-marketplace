package store

import (
	"strings"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

const artifactProofBlobPrefix = "artifact-proof-v1-sha256-"

// ArtifactProofBlobKey returns the distinct immutable blob-store key for the
// placeholder-form proof of one v1 artifact digest. Proof bytes cannot use the
// digest itself as their key because the legacy final archive already occupies
// that address.
func ArtifactProofBlobKey(digest ArchiveDigest) ArchiveDigest {
	value := string(digest)
	if !validInspectionDigest(value) {
		return ""
	}
	return ArchiveDigest(artifactProofBlobPrefix + strings.TrimPrefix(value, "sha256:"))
}

func archiveDigestFromProofBlobKey(key ArchiveDigest) (ArchiveDigest, bool) {
	value := string(key)
	if !strings.HasPrefix(value, artifactProofBlobPrefix) {
		return "", false
	}
	digest := ArchiveDigest("sha256:" + strings.TrimPrefix(value, artifactProofBlobPrefix))
	if !validInspectionDigest(string(digest)) {
		return "", false
	}
	return digest, true
}

func verifyArtifactProofBlob(upload BlobUpload, expected ArchiveDigest) bool {
	_, err := hccartifact.DecodeProof(upload.Data, string(expected), nil)
	return err == nil
}
