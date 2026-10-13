package opencodehook

import (
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
