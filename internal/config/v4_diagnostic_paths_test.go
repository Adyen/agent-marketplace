package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestV4DocumentDiagnosticsNameTheAuthoredFile(t *testing.T) {
	const header = "apiVersion: agent-marketplace/v4\n"
	const source = "sources:\n  a: {git: https://example.test/a.git, ref: main}\n"
	for _, tc := range []struct{ name, body, code, path string }{
		{"legacy key", header + "registries: []\n", "v4.legacy-key", "registries"},
		{"unknown top-level field", header + "bogus: 1\n", "v4.unknown-field", "bogus"},
		{"unknown source field", header + "sources:\n  a: {git: https://example.test/a.git, ref: main, bogus: 1}\n", "v4.unknown-field", "sources.a.bogus"},
		{"unknown resource field", header + source + "resources:\n  a: {bogus: 1}\n", "v4.unknown-field", "resources.a.bogus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			writeIncludeConfig(t, file, tc.body)
			_, err := ParseV4Config([]byte(tc.body), file, true)
			var validation *V4ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want V4ValidationError", err)
			}
			if validation.Code != tc.code || validation.Path != tc.path || validation.Line <= 0 {
				t.Fatalf("validation = %+v, want %s at %s with a line", validation, tc.code, tc.path)
			}
			assertSingleConfigPrefix(t, err, file)
		})
	}
}

func TestV4IncludedFragmentDiagnosticsNameTheFragment(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(dir, "fragment.yaml")
	writeIncludeConfig(t, fragment, "registries: []\n")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
	_, err = LoadV4ConfigFromPath(root)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.legacy-key" {
		t.Fatalf("error = %v, want v4.legacy-key", err)
	}
	assertSingleConfigPrefix(t, err, fragment)
	if strings.Contains(err.Error(), fmt.Sprintf("validate config %q", root)) {
		t.Fatalf("error = %v, must not carry a root prefix", err)
	}
}

func assertSingleConfigPrefix(t *testing.T, err error, file string) {
	t.Helper()
	if !strings.Contains(err.Error(), fmt.Sprintf("validate config %q", file)) {
		t.Fatalf("error = %v, want prefix naming %s", err, file)
	}
	if n := strings.Count(err.Error(), "validate config"); n != 1 {
		t.Fatalf("error = %v, has %d validate config prefixes, want 1", err, n)
	}
}
