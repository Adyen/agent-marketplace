package yamlfm

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantFM     map[string]any
		wantBody   string
		wantErr    bool
		errContain string
	}{
		{
			name:     "standard frontmatter",
			input:    "---\nname: my-skill\ndescription: A skill\n---\nBody content here\n",
			wantFM:   map[string]any{"name": "my-skill", "description": "A skill"},
			wantBody: "Body content here\n",
		},
		{
			name:     "no frontmatter",
			input:    "Just body content\nNo frontmatter\n",
			wantFM:   nil,
			wantBody: "Just body content\nNo frontmatter\n",
		},
		{
			name:     "empty frontmatter",
			input:    "---\n---\nBody here\n",
			wantFM:   map[string]any{},
			wantBody: "Body here\n",
		},
		{
			name:     "frontmatter with list values",
			input:    "---\ntriggers:\n  - click\n  - mention\n---\nBody\n",
			wantFM:   map[string]any{"triggers": []any{"click", "mention"}},
			wantBody: "Body\n",
		},
		{
			name:     "frontmatter with nested map",
			input:    "---\nenv:\n  KEY: value\n---\nBody\n",
			wantFM:   map[string]any{"env": map[string]any{"KEY": "value"}},
			wantBody: "Body\n",
		},
		{
			name:     "empty input",
			input:    "",
			wantFM:   nil,
			wantBody: "",
		},
		{
			name:       "only opening delimiter",
			input:      "---\nname: foo\n",
			wantErr:    true,
			errContain: "closing",
		},
		{
			name:     "delimiter not at start",
			input:    "some text\n---\nname: foo\n---\nbody\n",
			wantFM:   nil,
			wantBody: "some text\n---\nname: foo\n---\nbody\n",
		},
		{
			name:       "invalid YAML between delimiters",
			input:      "---\n: invalid\n---\nbody\n",
			wantErr:    true,
			errContain: "yaml",
		},
		{
			name:     "body with leading newline after delimiter",
			input:    "---\nname: foo\n---\n\nBody paragraph\n",
			wantFM:   map[string]any{"name": "foo"},
			wantBody: "\nBody paragraph\n",
		},
		{
			name:     "multiple document markers only first pair consumed",
			input:    "---\nname: foo\n---\nbody\n---\nmore\n---\n",
			wantFM:   map[string]any{"name": "foo"},
			wantBody: "body\n---\nmore\n---\n",
		},
		{
			name:     "frontmatter with boolean and numeric",
			input:    "---\nenabled: true\ncount: 3\n---\nbody\n",
			wantFM:   map[string]any{"enabled": true, "count": 3},
			wantBody: "body\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := ParseYAMLFrontmatter(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContain)
				}
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantFM == nil {
				if fm != nil {
					t.Errorf("expected nil frontmatter, got %v", fm)
				}
			} else {
				if fm == nil {
					t.Errorf("expected frontmatter %v, got nil", tt.wantFM)
				} else {
					for k, v := range tt.wantFM {
						got, ok := fm[k]
						if !ok {
							t.Errorf("missing key %q in frontmatter", k)
						} else if !equalAny(got, v) {
							t.Errorf("key %q: got %v (%T), want %v (%T)", k, got, got, v, v)
						}
					}
					if len(fm) != len(tt.wantFM) {
						t.Errorf("frontmatter has %d keys, want %d", len(fm), len(tt.wantFM))
					}
				}
			}
			if body != tt.wantBody {
				t.Errorf("body: got %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestParseYAMLFrontmatter_ReaderError(t *testing.T) {
	_, _, err := ParseYAMLFrontmatter(errReader{})
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
}

// errReader always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

// equalAny compares two values from YAML parsing, handling type differences
// between int/float that yaml.v3 may produce.
func equalAny(a, b any) bool {
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalAny(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !equalAny(v, bv[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func TestReadAllAndSplit(t *testing.T) {
	// Verify the helper correctly reads an io.Reader and splits at frontmatter.
	long := strings.Repeat("x", 65536)
	input := "---\nname: test\n---\n" + long
	fm, body, err := ParseYAMLFrontmatter(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil || fm["name"] != "test" {
		t.Errorf("frontmatter: got %v", fm)
	}
	if body != long {
		t.Errorf("body length: got %d, want %d", len(body), len(long))
	}
}

func TestQuoteScalar(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain word", "demo-skill", "demo-skill"},
		{"plain sentence", "Identify growth loops (flywheels) for traction.", "Identify growth loops (flywheels) for traction."},
		{"empty string quoted", "", "''"},
		{"colon space", "Evaluates 5 loop types: Viral, Usage", "'Evaluates 5 loop types: Viral, Usage'"},
		{"trailing colon", "see here:", "'see here:'"},
		{"colon no space is fine", "ratio 16:9 widescreen", "ratio 16:9 widescreen"},
		{"comment marker", "do this # then that", "'do this # then that'"},
		{"hash not preceded by space is fine", "color#ffffff", "color#ffffff"},
		{"leading indicator", "@mention", "'@mention'"},
		{"leading dash space", "- item", "'- item'"},
		{"leading quote", "\"quoted\"", "'\"quoted\"'"},
		{"apostrophe mid-word stays plain", "it's fine", "it's fine"},
		{"embedded single quote when quoting", "it's a list: a, b", "'it''s a list: a, b'"},
		{"bool-like", "true", "'true'"},
		{"null-like", "null", "'null'"},
		{"integer-like", "42", "'42'"},
		{"float-like", "3.14", "'3.14'"},
		{"leading space", " padded", "' padded'"},
		{"trailing space", "padded ", "'padded '"},
		{"newline falls back to double quote", "line1\nline2", "\"line1\\nline2\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteScalar(tt.in); got != tt.want {
				t.Errorf("QuoteScalar(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuoteScalar_RoundTrips is the regression guard for the bug where a
// description containing ": " produced invalid frontmatter that strict parsers
// silently skipped. Every quoted value must parse back to the original string.
func TestQuoteScalar_RoundTrips(t *testing.T) {
	values := []string{
		"",
		"demo-skill",
		"Identify growth loops (flywheels). Evaluates 5 loop types: Viral, Usage, Collaboration.",
		"trailing colon:",
		"do this # then that",
		"@mention",
		"- item",
		"it's a \"quoted\" value: with everything # here",
		"true",
		"42",
		" padded ",
		"line1\nline2",
	}
	for _, v := range values {
		doc := "---\ndescription: " + QuoteScalar(v) + "\n---\nbody\n"
		fm, _, err := ParseYAMLFrontmatter(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("frontmatter for %q failed to parse: %v\ndoc:\n%s", v, err, doc)
		}
		got, ok := fm["description"].(string)
		if !ok {
			t.Fatalf("description for %q did not parse as string: %#v", v, fm["description"])
		}
		if got != v {
			t.Errorf("round-trip mismatch: got %q, want %q", got, v)
		}
	}
}

func BenchmarkQuoteScalar(b *testing.B) {
	inputs := []string{
		"demo-skill",
		"Identify growth loops (flywheels) for sustainable traction.",
		"Evaluates 5 loop types: Viral, Usage, Collaboration, User-Generated, and Referral.",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = QuoteScalar(inputs[i%len(inputs)])
	}
}

func BenchmarkParseYAMLFrontmatter(b *testing.B) {
	input := "---\nname: bench-skill\ndescription: A skill for benchmarking\ntriggers:\n  - click\n  - mention\n---\nBody content here with some more text to simulate a real skill file.\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = ParseYAMLFrontmatter(strings.NewReader(input))
	}
}

var _ io.Reader = errReader{}
