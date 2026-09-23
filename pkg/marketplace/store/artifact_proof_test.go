package store

import (
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPrepareUploadsAcceptsBoundArtifactProof(t *testing.T) {
	a, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	raw, proof, digest, err := hccartifact.EncodeWithProof(a)
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := ArchiveDigest(digest)
	req := RegistryRevisionPublication{
		Resources: []ResourceRevision{{
			Coordinate:    ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
			ContentDigest: RevisionDigest(a.Manifest.ContentDigest),
			ArchiveDigest: archiveDigest,
		}},
		ExtraBlobs: []BlobUpload{
			{Digest: archiveDigest, Data: raw},
			{Digest: ArtifactProofBlobKey(archiveDigest), Data: proof},
		},
	}

	uploads, err := prepareUploads(req)
	if err != nil {
		t.Fatalf("prepareUploads: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("uploads = %d, want 2", len(uploads))
	}
	if uploads[1].Digest == archiveDigest {
		t.Fatal("proof blob key collides with the final archive")
	}
}

func TestPrepareUploadsRejectsArtifactProofMismatch(t *testing.T) {
	a, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	_, proof, digest, err := hccartifact.EncodeWithProof(a)
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := ArchiveDigest(digest)
	proof[len(proof)-1] ^= 0xff

	_, err = prepareUploads(RegistryRevisionPublication{
		Resources:  []ResourceRevision{{ArchiveDigest: archiveDigest}},
		ExtraBlobs: []BlobUpload{{Digest: ArtifactProofBlobKey(archiveDigest), Data: proof}},
	})
	if err == nil {
		t.Fatal("prepareUploads accepted a proof whose bytes do not match its artifact digest")
	}
}
