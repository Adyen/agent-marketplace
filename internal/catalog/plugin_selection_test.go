package catalog

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestNormalizedRootPathCleansEquivalents(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"foo", "./foo", "foo/"} {
		if got := normalizedRootPath(p); got != "foo" {
			t.Fatalf("normalizedRootPath(%q) = %q, want %q", p, got, "foo")
		}
	}
	if got := normalizedRootPath(""); got != "." {
		t.Fatalf("normalizedRootPath(%q) = %q, want %q", "", got, ".")
	}
}

func TestSameSourceLocationMatchesCleanedRootPaths(t *testing.T) {
	t.Parallel()
	entry := func(rootPath string) config.RegistryEntry {
		return config.RegistryEntry{URL: "https://example.com/registry.git", Ref: "main", Path: rootPath}
	}
	a := entry("./foo")
	for _, p := range []string{"foo", "foo/"} {
		if !sameSourceLocation(a, entry(p)) {
			t.Fatalf("sameSourceLocation did not match root path %q against %q", p, "./foo")
		}
	}
	if sameSourceLocation(a, entry("bar")) {
		t.Fatal("sameSourceLocation matched differing root paths")
	}
}
