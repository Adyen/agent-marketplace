package lockfile

import (
	"bytes"
	"testing"
)

func TestArtifactLockV3(t *testing.T) {
	old := v2GoldenDocument()
	doc, err := UpgradeArtifactLockFormat(old)
	if err != nil {
		t.Fatal(err)
	}
	if old.APIVersion != APIVersionV2 || old.Marketplaces[0].Resources[0].Artifact != nil {
		t.Fatal("format upgrade mutated input")
	}
	var selected *MarketplaceResourceLock
	for i := range doc.Marketplaces[0].Resources {
		r := &doc.Marketplaces[0].Resources[i]
		if r.Name == "review" {
			selected = r
		}
	}
	if selected == nil || selected.Artifact == nil || selected.Artifact.Schema != "v1" || selected.Artifact.ArchiveDigest != Digest(sha256b) || selected.ArchiveDigest != "" {
		t.Fatal("lost exact legacy identity")
	}
	selected.Artifact = &MarketplaceArtifactLock{Schema: "v2", ArtifactDigest: Digest(sha256b), BlobDigest: Digest(sha256c)}
	raw, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(decoded)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("unstable v3 encoding", err)
	}
	for _, bad := range []MarketplaceArtifactLock{{Schema: "unknown"}, {Schema: "v1", ArchiveDigest: Digest(sha256b), BlobDigest: Digest(sha256c)}, {Schema: "v2", ArtifactDigest: Digest(sha256b)}, {Schema: "v2", ArtifactDigest: Digest(sha256b), BlobDigest: Digest(sha256c), ArchiveDigest: Digest(sha256a)}} {
		selected.Artifact = &bad
		if _, err := Encode(doc); err == nil {
			t.Fatalf("accepted malformed selection: %+v", bad)
		}
	}
}
