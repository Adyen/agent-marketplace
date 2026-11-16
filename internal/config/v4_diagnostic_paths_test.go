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

func TestV4ParseDiagnosticDoesNotGuessUTF16BOMLine(t *testing.T) {
	data := []byte{0xff, 0xfe}
	for _, b := range []byte("apiVersion: agent-marketplace/v4\n") {
		data = append(data, b, 0)
	}
	data = append(data, 0x00, 0xd8, 0x20, 0x00)

	_, err := ParseV4Config(data, "config.yaml", true)
	assertV4ValidationLocation(t, err, "v4.parse", "config.yaml", "config.yaml", 0)
}

func TestV4DiagnosticProvenanceTracksAuthoredFields(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, root, fragment, code, path string
		line                             int
	}{
		{name: "root exclusion", root: "apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: ./skills}\nresources:\n  team:\n    exclude: [\"skill/**\"]\n", code: "v4.exclusion", path: "resources.team.exclude", line: 6},
		{name: "root auxiliary property", root: "apiVersion: agent-marketplace/v4\nlogging:\n  level: invalid-level\n", code: "v4.auxiliary", path: "logging", line: 3},
		{name: "root bound locator", root: "apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: ./skills}\nresources:\n  team: [plugin/unsafe]\n", code: "v4.locator", path: "resources.team.include", line: 5},
		{name: "undefined source group", root: "apiVersion: agent-marketplace/v4\nresources:\n  missing: [skill/first]\n", code: "v4.undefined-source", path: "resources.missing", line: 3},
		{name: "included source harness", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n", fragment: "sources:\n  team:\n    directory: ./skills\n    source_harness: invalid-harness\nresources:\n  team: [skill/first]\n", code: "v4.harness", path: "sources.team.source_harness", line: 3},
		{name: "source-specific exclusion", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  team: {directory: ./skills}\nresources:\n  team:\n    include: [skill/first]\n    exclude:\n      - registry/skill/*\n", fragment: "resources:\n  team:\n    exclude:\n      - registry/skill/*\n", code: "v4.exclusion", path: "registry/skill/*", line: 4},
		{name: "included bound locator", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  team: {directory: ./skills}\n", fragment: "resources:\n  team: [plugin/unsafe]\n", code: "v4.locator", path: "resources.team.include", line: 2},
		{name: "inherited registry with dotted source name", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  team.one: {directory: ./skills}\nresources:\n  team.one:\n    include: [skill/root]\n", fragment: "resources:\n  team.one:\n    registries: [corp]\n", code: "v4.option-combination", path: "resources.team.one.registries", line: 3},
		{name: "inherited harness with root tags", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources:\n  team: {directory: ./skills}\nresources:\n  team:\n    tags: [frontend]\n", fragment: "resources:\n  team:\n    harnesses:\n      - devin\n      - invalid-harness\n", code: "v4.harness", path: "resources.team.harnesses[1]", line: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(dir, "config.yaml")
			writeIncludeConfig(t, root, tc.root)
			wantFile := root
			if tc.fragment != "" {
				wantFile = filepath.Join(dir, "fragment.yaml")
				writeIncludeConfig(t, wantFile, tc.fragment)
			}
			_, err := LoadV4ConfigFromPath(root)
			assertV4ValidationLocation(t, err, tc.code, tc.path, wantFile, tc.line)
		})
	}
}

func assertV4ValidationLocation(t *testing.T, err error, code, path, file string, line int) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want V4ValidationError", err)
	}
	if validation.Code != code || validation.Path != path || validation.File != file || validation.Line != line {
		t.Fatalf("validation = %+v, want code=%q path=%q file=%q line=%d", validation, code, path, file, line)
	}
}

func assertSingleConfigPrefix(t *testing.T, err error, file string) {
	t.Helper()
	if !strings.Contains(err.Error(), fmt.Sprintf("validate config %q", file)) {
		t.Fatalf("error = %v, want prefix naming %s", err, file)
	}
	if n := strings.Count(err.Error(), `validate config "`); n != 1 {
		t.Fatalf("error = %v, has %d validate config prefixes, want 1", err, n)
	}
}
