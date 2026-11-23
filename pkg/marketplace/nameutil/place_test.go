package nameutil

import (
	"strings"
	"testing"
)

func TestEncodedPathSeparator(t *testing.T) {
	for _, raw := range []string{"%2F", "%2f", "%5C", "%5c"} {
		if !ContainsEncodedPathSeparator(raw) {
			t.Errorf("ContainsEncodedPathSeparator(%q) = false, want true", raw)
		}
	}

	nested := "%" + strings.Repeat("25", 4096) + "2F"
	if !ContainsEncodedPathSeparator(nested) {
		t.Error("ContainsEncodedPathSeparator(long nested separator) = false, want true")
	}
	if !ContainsEncodedPathSeparator("%25%32%46") {
		t.Error("ContainsEncodedPathSeparator(assembled separator) = false, want true")
	}
	if ContainsEncodedPathSeparator("100%25") {
		t.Error("ContainsEncodedPathSeparator(ordinary percent data) = true, want false")
	}
}
