package mdc

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRender_FullFrontmatter(t *testing.T) {
	r := ir.NewRule("typescript", "TypeScript conventions", "# Body\nUse interfaces.\n",
		[]string{"*.ts", "*.tsx"}, true, nil, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}

	want := "---\n" +
		"description: TypeScript conventions\n" +
		"globs: '*.ts,*.tsx'\n" +
		"alwaysApply: true\n" +
		"---\n" +
		"# Body\nUse interfaces.\n"
	if got != want {
		t.Fatalf("Render mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRender_AlwaysApplyFalseAndNoOptionalKeys(t *testing.T) {
	r := ir.NewRule("plain", "", "Just a body", nil, false, nil, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}

	// description and globs omitted when empty; alwaysApply always present.
	if strings.Contains(got, "description:") {
		t.Errorf("expected no description key, got %q", got)
	}
	if strings.Contains(got, "globs:") {
		t.Errorf("expected no globs key, got %q", got)
	}
	if !strings.Contains(got, "alwaysApply: false\n") {
		t.Errorf("expected alwaysApply: false, got %q", got)
	}
	if !strings.HasSuffix(got, "Just a body\n") {
		t.Errorf("expected trailing-newline body, got %q", got)
	}
}

func TestRender_QuotesDescriptionWithColon(t *testing.T) {
	r := ir.NewRule("x", "note: be careful", "body\n", nil, false, nil, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	// A raw "description: note: be careful" would be invalid YAML; QuoteScalar must wrap it.
	if strings.Contains(got, "description: note: be careful\n") {
		t.Errorf("description with colon must be quoted, got %q", got)
	}
	if !strings.Contains(got, "description: 'note: be careful'\n") {
		t.Errorf("expected single-quoted description, got %q", got)
	}
}

func TestRender_QuotesExtraKeysAndReturnsErrors(t *testing.T) {
	r := ir.NewRule("x", "d", "body\n", nil, false, map[string]any{"extra: key": "true"}, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "'extra: key': 'true'\n") {
		t.Fatalf("frontmatter did not quote extra key/scalar: %q", got)
	}

	r.Extra["unsupported"] = make(chan int)
	if _, err := Render(r); err == nil {
		t.Fatal("Render accepted an unsupported YAML value")
	}
}

func TestRender_PreservesExtraSorted(t *testing.T) {
	r := ir.NewRule("x", "d", "body\n", nil, false,
		map[string]any{"zeta": "z", "alpha": "a"}, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}
	ai := strings.Index(got, "alpha:")
	zi := strings.Index(got, "zeta:")
	if ai < 0 || zi < 0 || ai > zi {
		t.Errorf("expected extra keys in sorted order (alpha before zeta), got %q", got)
	}
}

// TestRender_NestedExtraRendersBlockYAML locks in block-style rendering for
// complex Extra values. A single-key map or single-item slice must not collapse
// into invalid inline frontmatter (e.g. "key: x: 1" or "key: - a"); the
// frontmatter must round-trip through a YAML parser.
func TestRender_NestedExtraRendersBlockYAML(t *testing.T) {
	r := ir.NewRule("x", "d", "body\n", nil, false, map[string]any{
		"singleMap":   map[string]any{"only": "value"},
		"singleSlice": []any{"one"},
		"nestedMap":   map[string]any{"outer": map[string]any{"inner": "v"}},
	}, ir.SourceRef{})
	got, err := Render(r)
	if err != nil {
		t.Fatal(err)
	}

	// Invalid inline collapses that would break frontmatter parsing.
	for _, bad := range []string{"singleMap: only:", "singleSlice: - one", "nestedMap: outer:"} {
		if strings.Contains(got, bad) {
			t.Errorf("complex value collapsed to invalid inline YAML %q in:\n%s", bad, got)
		}
	}

	fm := frontmatterBlock(t, got)
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v\n%s", err, fm)
	}
	sm, ok := parsed["singleMap"].(map[string]any)
	if !ok || sm["only"] != "value" {
		t.Errorf("singleMap did not round-trip, got %#v", parsed["singleMap"])
	}
	ss, ok := parsed["singleSlice"].([]any)
	if !ok || len(ss) != 1 || ss[0] != "one" {
		t.Errorf("singleSlice did not round-trip, got %#v", parsed["singleSlice"])
	}
}

// frontmatterBlock returns the YAML frontmatter between the leading and closing
// "---" delimiters of a rendered .mdc file.
func frontmatterBlock(t *testing.T, rendered string) string {
	t.Helper()
	if !strings.HasPrefix(rendered, "---\n") {
		t.Fatalf("rendered content missing opening frontmatter delimiter:\n%s", rendered)
	}
	rest := rendered[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatalf("rendered content missing closing frontmatter delimiter:\n%s", rendered)
	}
	return rest[:end]
}
