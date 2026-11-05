package catalog

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEntriesFromCollectionsAcceptsPluginMemberKind(t *testing.T) {
	entries, err := EntriesFromCollections([]collection.Collection{{
		Name:      "suite",
		Version:   "1.0.0",
		Resources: []collection.CollectionEntry{{Name: "review-kit", Kind: "plugin"}},
	}})
	if err != nil {
		t.Fatalf("EntriesFromCollections: %v", err)
	}
	col, ok := entries[0].Resource.(*ir.Collection)
	if !ok {
		t.Fatalf("entry resource = %T, want collection", entries[0].Resource)
	}
	if len(col.Members) != 1 || col.Members[0].Kind != ir.KindPlugin || col.Members[0].Name != "review-kit" {
		t.Fatalf("collection members = %#v, want one plugin member review-kit", col.Members)
	}
}
