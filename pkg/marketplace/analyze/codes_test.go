package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindingCodes_Documented(t *testing.T) {
	path := filepath.Join("..", "..", "..", "pkg", "marketplace", "analyze", "codes.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read codes.md: %v", err)
	}
	content := string(data)

	for _, code := range FindingCodes() {
		if !strings.Contains(content, code) {
			t.Errorf("codes.md missing documentation for %q", code)
		}
	}
}

func TestFindingCodesAreStable(t *testing.T) {
	codes := FindingCodes()
	seen := make(map[string]bool)
	for _, code := range codes {
		if seen[code] {
			t.Fatalf("duplicate finding code %q", code)
		}
		seen[code] = true
		if !strings.HasPrefix(code, "marketplace.find.") {
			t.Errorf("finding code %q does not use marketplace.find. prefix", code)
		}
	}
}
