// Package testutil provides test helpers for the harness-compilation library,
// including golden-file assertion with UPDATE_GOLDEN=1 support.
package testutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// AssertGolden compares the got bytes against the golden file at pathToExpected.
// If UPDATE_GOLDEN=1 is set in the environment, the golden file is (re)written
// with got instead of comparing.
func AssertGolden(t *testing.T, got []byte, pathToExpected string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		dir := filepath.Dir(pathToExpected)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create golden dir %s: %v", dir, err)
		}
		if err := os.WriteFile(pathToExpected, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", pathToExpected, err)
		}
		t.Logf("updated golden: %s", pathToExpected)
		return
	}

	expected, err := os.ReadFile(pathToExpected)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden file %s does not exist; run with UPDATE_GOLDEN=1 to create it", pathToExpected)
		}
		t.Fatalf("read golden %s: %v", pathToExpected, err)
	}

	if string(got) != string(expected) {
		t.Fatalf("golden mismatch for %s:\n--- GOT ---\n%s\n--- EXPECTED ---\n%s", pathToExpected, got, expected)
	}
}

// AssertGoldenDir asserts SET EQUALITY between the files written by
// Materialize and the golden files in the expected directory: every output
// file must have a matching golden file with identical bytes, and every golden
// file must have a counterpart in the output. Walking outDir alone would let a
// golden file that is no longer produced go undetected.
func AssertGoldenDir(t *testing.T, outDir, expectedDir string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		updateGoldenDir(t, outDir, expectedDir)
		return
	}

	// Walk outDir and compare each file.
	produced := make(map[string]struct{})
	if err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return nil
		}
		// Skip staging directories.
		if strings.HasPrefix(filepath.Base(rel), ".tmp-") {
			return nil
		}
		produced[rel] = struct{}{}

		goldenPath := filepath.Join(expectedDir, rel)
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("read output %s: %v", path, readErr)
			return nil
		}
		AssertGolden(t, got, goldenPath)
		return nil
	}); err != nil {
		t.Fatalf("walk output dir %s: %v", outDir, err)
	}

	// Walk expectedDir and fail on any golden file the run did not produce.
	// A missing counterpart means an artifact regressed out of existence,
	// which the outDir walk alone cannot see.
	var missing []string
	if err := filepath.WalkDir(expectedDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(expectedDir, path)
		if relErr != nil {
			return relErr
		}
		if _, ok := produced[rel]; !ok {
			missing = append(missing, rel)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk golden dir %s: %v", expectedDir, err)
	}
	sort.Strings(missing)
	for _, rel := range missing {
		t.Errorf("golden file %s has no counterpart in the compiled output; run with UPDATE_GOLDEN=1 only if the artifact was intentionally removed",
			filepath.Join(expectedDir, rel))
	}
}

// updateGoldenDir copies all files from outDir to expectedDir.
func updateGoldenDir(t *testing.T, outDir, expectedDir string) {
	t.Helper()

	if err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return nil
		}
		if strings.HasPrefix(filepath.Base(rel), ".tmp-") {
			return nil
		}

		goldenPath := filepath.Join(expectedDir, rel)
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if err := os.WriteFile(goldenPath, data, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
		t.Logf("updated golden: %s", goldenPath)
		return nil
	}); err != nil {
		t.Fatalf("walk output dir %s: %v", outDir, err)
	}
}
