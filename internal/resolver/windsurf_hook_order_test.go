package resolver

import (
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestWindsurfHookSelectionOrderUsesCatalogOrder(t *testing.T) {
	entriesA := []catalog.Entry{
		{Kind: ir.KindHook, Name: "zeta", SourceHarness: "windsurf"},
		{Kind: ir.KindHook, Name: "alpha", SourceHarness: "windsurf"},
		{Kind: ir.KindCommand, Name: "aaa-command", SourceHarness: "windsurf"},
	}
	entriesB := []catalog.Entry{
		{Kind: ir.KindHook, Name: "middle", SourceHarness: "windsurf"},
		{Kind: ir.KindHook, Name: "beta", SourceHarness: "windsurf"},
	}
	entryA := config.RegistryEntry{Default: true}
	entryB := config.RegistryEntry{Default: true}
	got := append(resolveSelectedResources(entryA, entriesA), resolveSelectedResources(entryB, entriesB)...)
	if names := hookSelectionNames(got); !reflect.DeepEqual(names, []string{"zeta", "alpha", "middle", "beta"}) {
		t.Fatalf("Windsurf hook selection = %v", names)
	}
	entryA.Hooks.Include = []string{"alpha"}
	got = append(resolveSelectedResources(entryA, entriesA), resolveSelectedResources(entryB, entriesB)...)
	if names := hookSelectionNames(got); !reflect.DeepEqual(names, []string{"alpha", "middle", "beta"}) {
		t.Fatalf("filtered Windsurf hook selection = %v", names)
	}
}

func TestNonWindsurfHookSelectionKeepsProviderAlphabeticalOrder(t *testing.T) {
	entriesA := []catalog.Entry{
		{Kind: ir.KindHook, Name: "zeta", SourceHarness: "factory"},
		{Kind: ir.KindHook, Name: "alpha", SourceHarness: "factory"},
		{Kind: ir.KindCommand, Name: "aaa-command", SourceHarness: "factory"},
	}
	entriesB := []catalog.Entry{
		{Kind: ir.KindHook, Name: "middle", SourceHarness: "factory"},
		{Kind: ir.KindHook, Name: "beta", SourceHarness: "factory"},
	}
	got := append(resolveSelectedResources(config.RegistryEntry{Default: true}, entriesA), resolveSelectedResources(config.RegistryEntry{Default: true}, entriesB)...)
	if names := hookSelectionNames(got); !reflect.DeepEqual(names, []string{"alpha", "zeta", "beta", "middle"}) {
		t.Fatalf("Factory hook selection = %v", names)
	}
	if got[0].kind != ir.KindCommand || got[0].name != "aaa-command" {
		t.Fatalf("non-hook selection moved: %v", got)
	}
}

func hookSelectionNames(resources resourceSelections) []string {
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.kind == ir.KindHook {
			names = append(names, resource.name)
		}
	}
	return names
}
