package store

import (
	"os"
	"path/filepath"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func readToolchainUploadFixture(t *testing.T, version, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "harness-compilation", "artifact", "testdata", "toolchain", version, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// spec: marketplace-publication/publication-stores-placeholder-with-final-archive
func TestPublicationStoresPlaceholderWithFinalArchive(t *testing.T) {
	for _, version := range []string{"go1.26", "go1.27"} {
		t.Run("recorded-by-"+version, func(t *testing.T) {
			final := readToolchainUploadFixture(t, version, "mcpserver.tar.gz")
			placeholder := readToolchainUploadFixture(t, version, "mcpserver.placeholder.tar.gz")
			digest := ArchiveDigest(hccartifact.ArchiveDigest(placeholder))
			resources := []ResourceRevision{{ArchiveDigest: digest}}
			uploads := func(placeholderData []byte) []BlobUpload {
				return []BlobUpload{{Digest: digest, Data: final}, {Digest: PlaceholderArchiveKey(digest), Data: placeholderData}}
			}

			stored, err := prepareUploads(RegistryRevisionPublication{Resources: resources, ExtraBlobs: uploads(placeholder)})
			if err != nil {
				t.Fatalf("prepareUploads: %v", err)
			}
			if len(stored) != 2 {
				t.Fatalf("uploads = %d, want final and placeholder archives", len(stored))
			}

			tampered := append([]byte(nil), placeholder...)
			tampered[len(tampered)/2] ^= 0xff
			if _, err := prepareUploads(RegistryRevisionPublication{Resources: resources, ExtraBlobs: uploads(tampered)}); err == nil {
				t.Fatal("publication accepted a placeholder archive whose bytes do not hash to its digest")
			}

			if _, err := prepareUploads(RegistryRevisionPublication{ExtraBlobs: uploads(placeholder)}); err == nil {
				t.Fatal("publication accepted a placeholder archive with no matching resource")
			}

			badFinal := append([]byte(nil), final...)
			badFinal[len(badFinal)/2] ^= 0xff
			if _, err := prepareUploads(RegistryRevisionPublication{Resources: resources, ExtraBlobs: []BlobUpload{{Digest: digest, Data: badFinal}, {Digest: PlaceholderArchiveKey(digest), Data: placeholder}}}); err == nil {
				t.Fatal("publication accepted a final archive whose content does not match its placeholder")
			}
		})
	}
}
