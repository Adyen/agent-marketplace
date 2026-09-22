package artifact

import (
	"archive/tar"
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyTarCompatibility(t *testing.T) {
	if runtime.Version() != "go1.26.6" {
		t.Skip("reference encoder is Go 1.26.6")
	}
	for _, name := range []string{"manifest.json", "assets/café.txt", "assets/" + strings.Repeat("a", 90), "assets/" + strings.Repeat("a", 100), "assets/" + strings.Repeat("a", 110), "assets/" + strings.Repeat("é", 60), "assets/line\nbreak", "assets/" + strings.Repeat("x", 92) + "/long", strings.Repeat("a", 156) + "/" + strings.Repeat("b", 99)} {
		for _, size := range []int{0, 1, 511, 512, 513, 65535} {
			t.Run(fmt.Sprintf("%s/%d", name, size), func(t *testing.T) {
				var got, want bytes.Buffer
				old := newLegacyTarWriter(&got)
				reference := tar.NewWriter(&want)
				for _, writer := range []interface {
					WriteHeader(*tar.Header) error
					Write([]byte) (int, error)
					Close() error
				}{old, reference} {
					if err := writer.WriteHeader(&tar.Header{Name: "resource", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
						t.Fatal(err)
					}
					if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(size)}); err != nil {
						t.Fatal(err)
					}
					if _, err := writer.Write(bytes.Repeat([]byte{'x'}, size)); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if !bytes.Equal(got.Bytes(), want.Bytes()) {
					for i := range min(got.Len(), want.Len()) {
						if got.Bytes()[i] != want.Bytes()[i] {
							t.Fatalf("tar differs at %d: got %q want %q", i, got.Bytes()[i], want.Bytes()[i])
						}
					}
					t.Fatalf("length got %d want %d", got.Len(), want.Len())
				}
			})
		}
	}
}
