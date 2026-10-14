package opencodehook

import (
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEmbeddedAdapterRetainsCompilationBudget(t *testing.T) {
	guard := "if (declarations.length > " + strconv.Itoa(MaxDeclarationsPerToolEvent) + ") throw refusal();"
	if strings.Count(string(Adapter), guard) != 1 {
		t.Fatal("fixed adapter invocation guard differs from the compilation budget")
	}
}

func TestCanonicalToolsMirrorTheEmbeddedAdapter(t *testing.T) {
	match := regexp.MustCompile(`const canonicalTools = \{([^}]*)\};`).FindStringSubmatch(string(Adapter))
	if match == nil {
		t.Fatal("fixed adapter canonicalization table not found")
	}
	adapter := map[string]string{}
	for _, entry := range strings.Split(match[1], ",") {
		native, canonical, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			t.Fatalf("malformed adapter entry %q", entry)
		}
		adapter[strings.TrimSpace(native)] = strings.Trim(strings.TrimSpace(canonical), `"`)
	}
	if !maps.Equal(adapter, CanonicalTools) {
		t.Fatalf("adapter canonicalization %v differs from CanonicalTools %v", adapter, CanonicalTools)
	}
}
