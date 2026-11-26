package pipeline

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestPlaceCededPathsExcludesRetainedDirectories(t *testing.T) {
	prunes := []dependencytxn.PlacePrune{
		{Path: "mods/late-edit"},
		{Path: "mods/unchanged"},
		{Path: "mods", Directory: &fsboundary.Identity{}},
	}
	got := placeCededPaths([]string{"mods/missing"}, prunes, []string{"mods", "mods/late-edit"})
	if want := []string{"mods/late-edit", "mods/missing"}; !slices.Equal(got, want) {
		t.Fatalf("ceded paths = %v, want %v", got, want)
	}
}
