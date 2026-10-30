package collection

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// manifestOfSize returns a COLLECTION.yaml document of exactly size bytes.
func manifestOfSize(size int) []byte {
	header := "name: big\ndescription: "
	return []byte(header + strings.Repeat("x", size-len(header)))
}

// spec: catalog-discovery/oversized-collection-manifest-is-refused-before-parsing
func TestDiscoverCollectionsRefusesManifestAboveCeiling(t *testing.T) {
	const ceiling = 256 << 10
	at, err := DiscoverCollectionsFS(fstest.MapFS{"collections/big/COLLECTION.yaml": {Data: manifestOfSize(ceiling)}}, "/registry", "", "", "", ".")
	if err != nil || len(at) != 1 || at[0].Name != "big" {
		t.Fatalf("manifest at the ceiling: collections=%v err=%v", at, err)
	}
	_, err = DiscoverCollectionsFS(fstest.MapFS{"collections/big/COLLECTION.yaml": {Data: manifestOfSize(ceiling + 1)}}, "/registry", "", "", "", ".")
	if !errors.Is(err, ErrManifestTooLarge) {
		t.Fatalf("manifest above the ceiling: err=%v, want ErrManifestTooLarge", err)
	}
}
