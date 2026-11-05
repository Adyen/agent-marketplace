package bundle

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadSubagent(t *testing.T) {
	tests := []struct {
		name           string
		fs             fstest.MapFS
		path           string
		wantName       string
		wantDesc       string
		wantBody       string
		wantModel      string
		wantTools      []string
		wantEffort     string
		wantDisallowed []string
		wantIsolation  string
		wantExtra      map[string]any
		wantErr        bool
		errContain     string
	}{
		{
			name: "full subagent",
			fs: fstest.MapFS{
				"agents/my-agent.md": {
					Data: []byte("---\nname: my-agent\ndescription: An agent\nmodel: sonnet\ntools:\n  - Read\n  - Write\neffort: high\ndisallowedTools:\n  - Bash\nisolation: worktree\n---\nSystem prompt body\n"),
				},
			},
			path:           "agents/my-agent.md",
			wantName:       "my-agent",
			wantDesc:       "An agent",
			wantBody:       "System prompt body\n",
			wantModel:      "sonnet",
			wantTools:      []string{"Read", "Write"},
			wantEffort:     "high",
			wantDisallowed: []string{"Bash"},
			wantIsolation:  "worktree",
		},
		{
			name: "tools as string",
			fs: fstest.MapFS{
				"agents/simple.md": {
					Data: []byte("---\nname: simple\ntools: Read\n---\nBody\n"),
				},
			},
			path:      "agents/simple.md",
			wantName:  "simple",
			wantBody:  "Body\n",
			wantTools: []string{"Read"},
		},
		{
			name: "name from filename",
			fs: fstest.MapFS{
				"agents/fallback-name.md": {
					Data: []byte("---\ndescription: Auto\n---\nBody\n"),
				},
			},
			path:     "agents/fallback-name.md",
			wantName: "fallback-name",
			wantDesc: "Auto",
			wantBody: "Body\n",
		},
		{
			name: "extra fields preserved",
			fs: fstest.MapFS{
				"agents/my-agent.md": {
					Data: []byte("---\nname: my-agent\ndescription: Agent\nmodel: opus\ntools:\n  - Read\neffort: low\ndisallowedTools: []\nisolation: \"\"\ncustomFlag: true\n---\nBody\n"),
				},
			},
			path:       "agents/my-agent.md",
			wantName:   "my-agent",
			wantDesc:   "Agent",
			wantBody:   "Body\n",
			wantModel:  "opus",
			wantTools:  []string{"Read"},
			wantEffort: "low",
			wantExtra:  map[string]any{"customFlag": true},
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../agents/evil.md",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid kebab name uppercase start",
			fs: fstest.MapFS{
				"agents/BadAgent.md": {
					Data: []byte("---\nname: BadAgent\n---\nBody\n"),
				},
			},
			path:       "agents/BadAgent.md",
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name: "invalid kebab name with underscores",
			fs: fstest.MapFS{
				"agents/bad_agent.md": {
					Data: []byte("---\nname: bad_agent\n---\nBody\n"),
				},
			},
			path:       "agents/bad_agent.md",
			wantErr:    true,
			errContain: "kebab-case",
		},
		{
			name:       "missing file",
			fs:         fstest.MapFS{},
			path:       "agents/missing.md",
			wantErr:    true,
			errContain: "open",
		},
		{
			name: "minimal subagent no extra fields",
			fs: fstest.MapFS{
				"agents/minimal.md": {
					Data: []byte("---\nname: minimal\n---\nBody\n"),
				},
			},
			path:     "agents/minimal.md",
			wantName: "minimal",
			wantBody: "Body\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadSubagent(tt.fs, tt.path)
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
			if got.Model != tt.wantModel {
				t.Errorf("model: got %q, want %q", got.Model, tt.wantModel)
			}
			if tt.wantTools != nil {
				if len(got.Tools) != len(tt.wantTools) {
					t.Errorf("tools: got %v, want %v", got.Tools, tt.wantTools)
				} else {
					for i, v := range tt.wantTools {
						if got.Tools[i] != v {
							t.Errorf("tools[%d]: got %q, want %q", i, got.Tools[i], v)
						}
					}
				}
			}
			if got.Effort != tt.wantEffort {
				t.Errorf("effort: got %q, want %q", got.Effort, tt.wantEffort)
			}
			if tt.wantDisallowed != nil {
				if len(got.DisallowedTools) != len(tt.wantDisallowed) {
					t.Errorf("disallowedTools: got %v, want %v", got.DisallowedTools, tt.wantDisallowed)
				}
			}
			if got.Isolation != tt.wantIsolation {
				t.Errorf("isolation: got %q, want %q", got.Isolation, tt.wantIsolation)
			}
			if tt.wantExtra != nil {
				for k, v := range tt.wantExtra {
					gv, ok := got.Extra[k]
					if !ok {
						t.Errorf("extra key %q missing", k)
					} else if gv != v {
						t.Errorf("extra[%q]: got %v, want %v", k, gv, v)
					}
				}
			} else if len(got.Extra) > 0 {
				t.Errorf("extra: got %v, want nil/empty", got.Extra)
			}
		})
	}
}

func TestLoadSubagent_RejectsMalformedIsolation(t *testing.T) {
	src := fstest.MapFS{
		"agents/malformed.md": {Data: []byte("---\nname: malformed\nisolation: true\n---\nBody\n")},
	}
	if _, err := LoadSubagent(src, "agents/malformed.md"); err == nil {
		t.Fatal("LoadSubagent accepted non-string isolation")
	}
}

func TestLoadSubagent_RejectsConflictingMaxNestingAliases(t *testing.T) {
	src := fstest.MapFS{
		"agents/conflicting.md": {Data: []byte("---\nname: conflicting\nmax-nesting: 3\nmaxNesting: 5\n---\nBody\n")},
	}
	if _, err := LoadSubagent(src, "agents/conflicting.md"); err == nil {
		t.Fatal("LoadSubagent accepted both max-nesting and maxNesting")
	} else if !containsString(err.Error(), "max-nesting") {
		t.Fatalf("LoadSubagent error = %q, want max-nesting alias conflict", err)
	}
}

func TestLoadSubagent_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"agents/my-agent.md": {
			Data: []byte("---\nname: my-agent\n---\nBody\n"),
		},
	}
	sa, err := LoadSubagent(src, "agents/my-agent.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRef := ir.SourceRef{File: "agents/my-agent.md", Line: 1}
	if sa.Source != wantRef {
		t.Errorf("source: got %+v, want %+v", sa.Source, wantRef)
	}
}

// Claude documents a subagent's tools and disallowedTools as either a YAML list
// or a comma-separated string such as "Read, Grep, Bash". A value that is not a
// well-formed list keeps the single entry earlier versions produced, so the
// subagent that loaded before still loads.
func TestLoadSubagent_ToolLists(t *testing.T) {
	tests := []struct {
		name  string
		value string // the YAML that follows "<key>:"
		want  []string
	}{
		{"comma-separated string", " Read, Grep, Glob", []string{"Read", "Grep", "Glob"}},
		{"single name", " Read", []string{"Read"}},
		{"single name with surrounding spaces", ` "  Read  "`, []string{"Read"}},
		{"surrounding and missing spaces", ` "  Read ,Grep "`, []string{"Read", "Grep"}},
		{"blank entries are dropped", " Read,, Grep, ", []string{"Read", "Grep"}},
		{"comma inside parentheses", " Agent(worker, researcher), Read", []string{"Agent(worker, researcher)", "Read"}},
		{"nested parentheses", " Bash(f(a, b), c), Read", []string{"Bash(f(a, b), c)", "Read"}},
		{"escaped parenthesis does not close", ` Bash(echo \)), Read`, []string{`Bash(echo \))`, "Read"}},
		{"escaped parenthesis does not split", ` Read, Bash(a\), b)`, []string{"Read", `Bash(a\), b)`}},
		{"escaped comma does not split", ` Read\, Grep`, []string{`Read\, Grep`}},
		{"escaped backslash at the end is not dangling", ` Read, Grep\\`, []string{"Read", `Grep\\`}},
		{"escaped backslash does not escape the comma", ` Read\\, Grep`, []string{`Read\\`, "Grep"}},
		{"flow list", " [Read, Grep]", []string{"Read", "Grep"}},
		{"block list", "\n  - Read\n  - Grep", []string{"Read", "Grep"}},
		{"list entry with parentheses stays whole", "\n  - Agent(worker, researcher)\n  - Read", []string{"Agent(worker, researcher)", "Read"}},
		{"list entries are taken as written", "\n  - \"Read, Grep\"", []string{"Read, Grep"}},
		{"explicit empty list", " []", []string{}},
		// Not well-formed lists: the string stays one entry, exactly as written.
		{"empty string", ` ""`, []string{""}},
		{"only spaces", ` "   "`, []string{"   "}},
		{"only commas", ` ","`, []string{","}},
		{"only separators", ` " , "`, []string{" , "}},
		{"unclosed parenthesis", " Read, Bash(git diff", []string{"Read, Bash(git diff"}},
		{"unopened parenthesis", " Read), Grep", []string{"Read), Grep"}},
		{"extra closing parenthesis", " Read, Bash(git diff))", []string{"Read, Bash(git diff))"}},
		{"escaped closing parenthesis leaves one open", ` Bash(echo \), Read`, []string{`Bash(echo \), Read`}},
		{"dangling backslash", ` Read, Grep\`, []string{`Read, Grep\`}},
		{"dangling backslash on a single name", ` Read\`, []string{`Read\`}},
	}
	for _, key := range []string{"tools", "disallowedTools"} {
		for _, tt := range tests {
			t.Run(key+"/"+tt.name, func(t *testing.T) {
				src := fstest.MapFS{"agents/a.md": {Data: []byte("---\nname: a\n" + key + ":" + tt.value + "\n---\nBody\n")}}
				got, err := LoadSubagent(src, "agents/a.md")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				list, other := got.Tools, got.DisallowedTools
				if key == "disallowedTools" {
					list, other = other, list
				}
				// DeepEqual keeps nil (absent) apart from an empty, explicit list.
				if !reflect.DeepEqual(list, tt.want) {
					t.Errorf("%s: got %#v, want %#v", key, list, tt.want)
				}
				if other != nil {
					t.Errorf("the other tool list gained %#v", other)
				}
			})
		}
	}
}

func TestLoadSubagent_RejectsUnsupportedToolListTypes(t *testing.T) {
	for _, key := range []string{"tools", "disallowedTools"} {
		for _, value := range []string{"5", "true", "~"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				src := fstest.MapFS{"agents/a.md": {Data: []byte("---\nname: a\n" + key + ": " + value + "\n---\nBody\n")}}
				if got, err := LoadSubagent(src, "agents/a.md"); err == nil {
					t.Fatalf("LoadSubagent accepted %s %q: %#v", key, value, got)
				}
			})
		}
	}
}
