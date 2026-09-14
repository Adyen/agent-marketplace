package bundle

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadRule(t *testing.T) {
	tests := []struct {
		name            string
		fs              fstest.MapFS
		path            string
		wantName        string
		wantDesc        string
		wantBody        string
		wantGlobs       []string
		wantAlwaysApply bool
		wantExtra       map[string]any
		wantErr         bool
		errContain      string
	}{
		{
			name: "plain markdown factory rule, no frontmatter",
			fs: fstest.MapFS{
				"rules/typescript.md": {Data: []byte("# TypeScript Rules\nUse interfaces.\n")},
			},
			path:     "rules/typescript.md",
			wantName: "typescript",
			wantBody: "# TypeScript Rules\nUse interfaces.\n",
		},
		{
			name: "mdc rule with frontmatter and comma globs",
			fs: fstest.MapFS{
				"rules/typescript.mdc": {Data: []byte("---\ndescription: TS conventions\nglobs: '*.ts,*.tsx'\nalwaysApply: true\n---\nBody here\n")},
			},
			path:            "rules/typescript.mdc",
			wantName:        "typescript",
			wantDesc:        "TS conventions",
			wantBody:        "Body here\n",
			wantGlobs:       []string{"*.ts", "*.tsx"},
			wantAlwaysApply: true,
		},
		{
			name: "mdc rule with yaml list globs",
			fs: fstest.MapFS{
				"rules/api.mdc": {Data: []byte("---\ndescription: API rules\nglobs:\n  - src/api/**\n  - internal/api/**\nalwaysApply: false\n---\nAPI body\n")},
			},
			path:      "rules/api.mdc",
			wantName:  "api",
			wantDesc:  "API rules",
			wantBody:  "API body\n",
			wantGlobs: []string{"src/api/**", "internal/api/**"},
		},
		{
			name: "name from frontmatter overrides filename",
			fs: fstest.MapFS{
				"rules/whatever.mdc": {Data: []byte("---\nname: security\ndescription: sec\n---\nSecure it\n")},
			},
			path:     "rules/whatever.mdc",
			wantName: "security",
			wantDesc: "sec",
			wantBody: "Secure it\n",
		},
		{
			name: "extra frontmatter preserved",
			fs: fstest.MapFS{
				"rules/x.mdc": {Data: []byte("---\ndescription: d\ncustom: val\n---\nBody\n")},
			},
			path:      "rules/x.mdc",
			wantName:  "x",
			wantDesc:  "d",
			wantBody:  "Body\n",
			wantExtra: map[string]any{"custom": "val"},
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../etc/rule.md",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid kebab name",
			fs: fstest.MapFS{
				"rules/bad.mdc": {Data: []byte("---\nname: bad_rule\n---\nBody\n")},
			},
			path:       "rules/bad.mdc",
			wantErr:    true,
			errContain: "kebab-case",
		},
		{
			name:       "missing file",
			fs:         fstest.MapFS{},
			path:       "rules/missing.md",
			wantErr:    true,
			errContain: "open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadRule(tt.fs, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContain)
				}
				if !containsString(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name() != tt.wantName {
				t.Errorf("name: got %q, want %q", got.Name(), tt.wantName)
			}
			if got.Description != tt.wantDesc {
				t.Errorf("description: got %q, want %q", got.Description, tt.wantDesc)
			}
			if got.Body != tt.wantBody {
				t.Errorf("body: got %q, want %q", got.Body, tt.wantBody)
			}
			if tt.wantGlobs != nil && !reflect.DeepEqual(got.Globs, tt.wantGlobs) {
				t.Errorf("globs: got %v, want %v", got.Globs, tt.wantGlobs)
			}
			if tt.wantGlobs == nil && len(got.Globs) != 0 {
				t.Errorf("globs: got %v, want empty", got.Globs)
			}
			if got.AlwaysApply != tt.wantAlwaysApply {
				t.Errorf("alwaysApply: got %v, want %v", got.AlwaysApply, tt.wantAlwaysApply)
			}
			for k, v := range tt.wantExtra {
				if got.Extra[k] != v {
					t.Errorf("extra[%q]: got %v, want %v", k, got.Extra[k], v)
				}
			}
		})
	}
}

// TestLoadRule_LenientUnquotedGlobs verifies that real-world Cursor .mdc rules
// with unquoted glob patterns (invalid strict YAML) still ingest via
// the lenient line-based fallback.
func TestLoadRule_LenientUnquotedGlobs(t *testing.T) {
	src := fstest.MapFS{
		"rules/frontend.mdc": {Data: []byte("---\ndescription: Frontend\nglobs: *.ts,*.tsx\nalwaysApply: true\n---\n# Frontend\nUse function components.\n")},
	}
	rule, err := LoadRule(src, "rules/frontend.mdc")
	if err != nil {
		t.Fatalf("LoadRule with unquoted globs should not error, got %v", err)
	}
	if rule.Name() != "frontend" {
		t.Errorf("name = %q, want frontend", rule.Name())
	}
	if rule.Description != "Frontend" {
		t.Errorf("description = %q, want Frontend", rule.Description)
	}
	if !reflect.DeepEqual(rule.Globs, []string{"*.ts", "*.tsx"}) {
		t.Errorf("globs = %v, want [*.ts *.tsx]", rule.Globs)
	}
	if !rule.AlwaysApply {
		t.Error("alwaysApply = false, want true")
	}
	if rule.Body != "# Frontend\nUse function components.\n" {
		t.Errorf("body = %q", rule.Body)
	}
}

func TestLoadRule_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"rules/x.md": {Data: []byte("Body\n")},
	}
	rule, err := LoadRule(src, "rules/x.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := ir.SourceRef{File: "rules/x.md", Line: 1}
	if rule.Source != want {
		t.Errorf("source: got %+v, want %+v", rule.Source, want)
	}
}
