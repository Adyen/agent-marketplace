package inspection

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkBuild(b *testing.B) {
	coordinate := store.ResourceCoordinate{Marketplace: "marketplace", Source: "source", Kind: "skill", Name: "review"}
	revision := "sha256:" + strings.Repeat("a", 64)
	body := []byte("# Review\nInspect this resource.\n")
	support := map[string][]byte{"notes.txt": []byte("support notes")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Build(coordinate, revision, body, support, nil, 10, 4096, 16384); err != nil {
			b.Fatal(err)
		}
	}
}
