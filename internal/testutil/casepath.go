package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SameFileCaseVariant finds a distinct case spelling that resolves to path.
func SameFileCaseVariant(t testing.TB, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat original path %q: %v", path, err)
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	variant := ""
	var statErr error
	for i, component := range parts {
		if component == "" {
			continue
		}
		for _, spelling := range []string{strings.ToUpper(component), strings.ToLower(component)} {
			if spelling == component {
				continue
			}
			candidateParts := append([]string(nil), parts...)
			candidateParts[i] = spelling
			candidate := strings.Join(candidateParts, string(filepath.Separator))
			candidateInfo, err := os.Stat(candidate)
			if err != nil {
				if statErr == nil && !errors.Is(err, os.ErrNotExist) {
					statErr = err
				}
				continue
			}
			if os.SameFile(info, candidateInfo) {
				variant = candidate
				break
			}
		}
		if variant != "" {
			break
		}
	}
	if variant == "" {
		if statErr != nil {
			t.Fatalf("find same-file case variant for %q: %v", path, statErr)
		}
		t.Skipf("filesystem has no same-file case variant for %q", path)
	}
	t.Logf("same-file case variant: original=%q variant=%q", path, variant)
	variantInfo, err := os.Stat(variant)
	if err != nil {
		t.Fatalf("stat case variant %q: %v", variant, err)
	}
	if !os.SameFile(info, variantInfo) {
		t.Fatalf("case variant %q does not identify original path %q", variant, path)
	}
	return variant
}
