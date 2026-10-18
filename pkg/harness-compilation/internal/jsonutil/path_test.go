package jsonutil

import (
	"strings"
	"testing"
)

func TestJSONPathReusesCapacityAcrossSiblings(t *testing.T) {
	parent := RootPath()
	for range 16 {
		parent = parent.Key("k")
	}
	var child JSONPath
	allocs := testing.AllocsPerRun(3, func() {
		for range 64 {
			child = parent.Key("leaf")
		}
	})
	if allocs > 1 {
		t.Fatalf("sibling traversal copies the ancestor stack: %g allocations", allocs)
	}
	if got, want := child.String(), "$"+strings.Repeat(".k", 16)+".leaf"; got != want {
		t.Fatalf("child path = %q, want %q", got, want)
	}
	if got, want := parent.String(), "$"+strings.Repeat(".k", 16); got != want {
		t.Fatalf("parent path = %q, want %q", got, want)
	}
}
