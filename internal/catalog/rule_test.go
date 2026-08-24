package catalog

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEntriesFromCollectionsParsesRuleMembers(t *testing.T) {
	entries, err := EntriesFromCollections([]collection.Collection{{
		Name: "coding-standards",
		Resources: []collection.CollectionEntry{{
			Name: "secure-coding",
			Kind: "rule",
		}},
	}})
	if err != nil {
		t.Fatalf("EntriesFromCollections: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	resource, ok := entries[0].Resource.(*ir.Collection)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Collection", entries[0].Resource)
	}
	if len(resource.Members) != 1 || resource.Members[0].Kind != ir.KindRule {
		t.Fatalf("collection members = %#v, want one rule member", resource.Members)
	}
}
