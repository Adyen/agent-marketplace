package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeIncludeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// spec: consumer-config-format/include-trees-stay-within-the-include-limit
func TestLoadV4RefusesIncludeTreesAboveLimit(t *testing.T) {
	t.Run("diamond", func(t *testing.T) {
		// 3d+1 files expand to 2^d include paths; depth 8 loads 1,021 files.
		dir := t.TempDir()
		writeIncludeFixture(t, dir, "root.yaml", "apiVersion: agent-marketplace/v4\ninclude: [a1.yaml, b1.yaml]\n")
		const depth = 8
		for i := 1; i <= depth; i++ {
			writeIncludeFixture(t, dir, fmt.Sprintf("a%d.yaml", i), fmt.Sprintf("include: [s%d.yaml]\n", i))
			writeIncludeFixture(t, dir, fmt.Sprintf("b%d.yaml", i), fmt.Sprintf("include: [s%d.yaml]\n", i))
			next := "sources: {}\n"
			if i < depth {
				next = fmt.Sprintf("include: [a%d.yaml, b%d.yaml]\n", i+1, i+1)
			}
			writeIncludeFixture(t, dir, fmt.Sprintf("s%d.yaml", i), next)
		}
		_, err := LoadV4ConfigFromPath(filepath.Join(dir, "root.yaml"))
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.include-limit" {
			t.Fatalf("err = %v, want v4.include-limit", err)
		}
	})
	for _, tc := range []struct {
		includes int
		refused  bool
	}{{255, false}, {256, true}} {
		t.Run(fmt.Sprintf("flat %d includes", tc.includes), func(t *testing.T) {
			// The root plus its includes count as loaded files: 256 is the limit.
			dir := t.TempDir()
			names := make([]string, tc.includes)
			for i := range names {
				names[i] = fmt.Sprintf("f%03d.yaml", i)
				writeIncludeFixture(t, dir, names[i], "sources: {}\n")
			}
			writeIncludeFixture(t, dir, "root.yaml", "apiVersion: agent-marketplace/v4\ninclude: ["+strings.Join(names, ", ")+"]\n")
			_, err := LoadV4ConfigFromPath(filepath.Join(dir, "root.yaml"))
			var validation *V4ValidationError
			refused := errors.As(err, &validation) && validation.Code == "v4.include-limit"
			if refused != tc.refused || (!tc.refused && err != nil) {
				t.Fatalf("%d includes: err = %v, want refused=%t", tc.includes, err, tc.refused)
			}
		})
	}
}

// spec: consumer-config-format/include-trees-stay-within-the-include-limit
func TestLoadV4RefusesLongIncludeChainBeforeReadingPastLimit(t *testing.T) {
	// A chain of 257 includes ends in a missing file: reading it would report
	// v4.include, so v4.include-limit proves the load stopped before it.
	dir := t.TempDir()
	writeIncludeFixture(t, dir, "root.yaml", "apiVersion: agent-marketplace/v4\ninclude: [c001.yaml]\n")
	for i := 1; i < 257; i++ {
		writeIncludeFixture(t, dir, fmt.Sprintf("c%03d.yaml", i), fmt.Sprintf("include: [c%03d.yaml]\n", i+1))
	}
	_, err := LoadV4ConfigFromPath(filepath.Join(dir, "root.yaml"))
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.include-limit" {
		t.Fatalf("err = %v, want v4.include-limit before reading the missing file", err)
	}
}
