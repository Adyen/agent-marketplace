package bundle

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadSkill(t *testing.T) {
	tests := []struct {
		name         string
		fs           fstest.MapFS
		path         string
		wantName     string
		wantDesc     string
		wantBody     string
		wantTriggers []string
		wantExtra    map[string]any
		wantErr      bool
		errContain   string
	}{
		{
			name: "basic skill with all fields",
			fs: fstest.MapFS{
				"my-skill/SKILL.md": {
					Data: []byte("---\nname: my-skill\ndescription: A test skill\ntriggers:\n  - click\n  - mention\n---\nSkill body content\n"),
				},
			},
			path:         "my-skill",
			wantName:     "my-skill",
			wantDesc:     "A test skill",
			wantBody:     "Skill body content\n",
			wantTriggers: []string{"click", "mention"},
		},
		{
			name: "name from directory when not in frontmatter",
			fs: fstest.MapFS{
				"cool-skill/SKILL.md": {
					Data: []byte("---\ndescription: No name in FM\n---\nBody\n"),
				},
			},
			path:     "cool-skill",
			wantName: "cool-skill",
			wantDesc: "No name in FM",
			wantBody: "Body\n",
		},
		{
			name: "extra fields preserved",
			fs: fstest.MapFS{
				"my-skill/SKILL.md": {
					Data: []byte("---\nname: my-skill\ndescription: Skill\ncustomField: hello\nanotherExtra: 42\n---\nBody\n"),
				},
			},
			path:      "my-skill",
			wantName:  "my-skill",
			wantDesc:  "Skill",
			wantBody:  "Body\n",
			wantExtra: map[string]any{"customField": "hello", "anotherExtra": 42},
		},
		{
			name: "trigger as string instead of list",
			fs: fstest.MapFS{
				"my-skill/SKILL.md": {
					Data: []byte("---\nname: my-skill\ndescription: Skill\ntriggers: click\n---\nBody\n"),
				},
			},
			path:         "my-skill",
			wantName:     "my-skill",
			wantDesc:     "Skill",
			wantBody:     "Body\n",
			wantTriggers: []string{"click"},
		},
		{
			name: "missing SKILL.md",
			fs: fstest.MapFS{
				"my-skill/other.md": {
					Data: []byte("no skill md"),
				},
			},
			path:       "my-skill",
			wantErr:    true,
			errContain: "open",
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../etc/passwd",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid kebab name uppercase",
			fs: fstest.MapFS{
				"MySkill/SKILL.md": {
					Data: []byte("---\nname: MySkill\n---\nBody\n"),
				},
			},
			path:       "MySkill",
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name: "invalid kebab name with underscores",
			fs: fstest.MapFS{
				"my_skill/SKILL.md": {
					Data: []byte("---\nname: my_skill\n---\nBody\n"),
				},
			},
			path:       "my_skill",
			wantErr:    true,
			errContain: "kebab-case",
		},
		{
			name: "name starting with digit",
			fs: fstest.MapFS{
				"0bad/SKILL.md": {
					Data: []byte("---\nname: 0bad\n---\nBody\n"),
				},
			},
			path:       "0bad",
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name: "no frontmatter",
			fs: fstest.MapFS{
				"my-skill/SKILL.md": {
					Data: []byte("Just plain markdown\n"),
				},
			},
			path:     "my-skill",
			wantName: "my-skill",
			wantBody: "Just plain markdown\n",
		},
		{
			name: "empty body",
			fs: fstest.MapFS{
				"my-skill/SKILL.md": {
					Data: []byte("---\nname: my-skill\ndescription: Skill\n---\n"),
				},
			},
			path:     "my-skill",
			wantName: "my-skill",
			wantDesc: "Skill",
			wantBody: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadSkill(tt.fs, tt.path)
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
			if tt.wantTriggers != nil {
				if len(got.Triggers) != len(tt.wantTriggers) {
					t.Errorf("triggers: got %v, want %v", got.Triggers, tt.wantTriggers)
				} else {
					for i, v := range tt.wantTriggers {
						if got.Triggers[i] != v {
							t.Errorf("triggers[%d]: got %q, want %q", i, got.Triggers[i], v)
						}
					}
				}
			}
			if tt.wantExtra != nil {
				if got.Extra == nil {
					t.Errorf("extra: got nil, want %v", tt.wantExtra)
				} else {
					for k, v := range tt.wantExtra {
						gv, ok := got.Extra[k]
						if !ok {
							t.Errorf("extra key %q missing", k)
						} else if gv != v {
							t.Errorf("extra[%q]: got %v (%T), want %v (%T)", k, gv, gv, v, v)
						}
					}
					if len(got.Extra) != len(tt.wantExtra) {
						t.Errorf("extra: got %d keys, want %d", len(got.Extra), len(tt.wantExtra))
					}
				}
			} else if len(got.Extra) > 0 {
				t.Errorf("extra: got %v, want nil/empty", got.Extra)
			}
		})
	}
}

func TestLoadSkill_ExtraRoundTrip(t *testing.T) {
	// Verify that unknown frontmatter fields survive a load round-trip.
	src := fstest.MapFS{
		"test-skill/SKILL.md": {
			Data: []byte("---\nname: test-skill\ndescription: Test\ncustom: value\nnested:\n  key: inner\n---\nBody\n"),
		},
	}
	skill, err := LoadSkill(src, "test-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Extra == nil {
		t.Fatal("expected non-nil extra map")
	}
	if v, ok := skill.Extra["custom"]; !ok || v != "value" {
		t.Errorf("extra[custom]: got %v, want %q", v, "value")
	}
	if v, ok := skill.Extra["nested"]; !ok {
		t.Error("extra[nested]: missing")
	} else if m, ok := v.(map[string]any); !ok {
		t.Errorf("extra[nested]: got %T, want map[string]any", v)
	} else if m["key"] != "inner" {
		t.Errorf("extra[nested][key]: got %v, want %q", m["key"], "inner")
	}
}

func TestLoadSkill_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"my-skill/SKILL.md": {
			Data: []byte("---\nname: my-skill\n---\nBody\n"),
		},
	}
	skill, err := LoadSkill(src, "my-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRef := ir.SourceRef{File: "my-skill/SKILL.md", Line: 1}
	if skill.Source != wantRef {
		t.Errorf("source: got %+v, want %+v", skill.Source, wantRef)
	}
}

func TestLoadSkill_SupportFiles(t *testing.T) {
	src := fstest.MapFS{
		"my-skill/SKILL.md": {
			Data: []byte("---\nname: my-skill\n---\nBody\n"),
		},
		"my-skill/scripts/bootstrap.sh": {
			Data: []byte("#!/bin/sh\n"),
		},
		"my-skill/notes/example.txt": {
			Data: []byte("hello\n"),
		},
	}

	skill, err := LoadSkill(src, "my-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"notes/example.txt", "scripts/bootstrap.sh"}
	if len(skill.SupportFiles) != len(want) {
		t.Fatalf("support files: got %v, want %v", skill.SupportFiles, want)
	}
	for i := range want {
		if skill.SupportFiles[i] != want[i] {
			t.Fatalf("support files[%d] = %q, want %q", i, skill.SupportFiles[i], want[i])
		}
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || anySubstr(s, sub))
}

func anySubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLoadRootSkill(t *testing.T) {
	tests := []struct {
		name       string
		fs         fstest.MapFS
		wantName   string
		wantDesc   string
		wantBody   string
		wantErr    bool
		errContain string
	}{
		{
			name: "basic root skill with name",
			fs: fstest.MapFS{
				"SKILL.md": {
					Data: []byte("---\nname: firewall-rules\ndescription: Manage firewall rules\n---\nFirewall body\n"),
				},
			},
			wantName: "firewall-rules",
			wantDesc: "Manage firewall rules",
			wantBody: "Firewall body\n",
		},
		{
			name: "root skill without name field",
			fs: fstest.MapFS{
				"SKILL.md": {
					Data: []byte("---\ndescription: No name\n---\nBody\n"),
				},
			},
			wantErr:    true,
			errContain: "must declare a 'name' field",
		},
		{
			name: "root skill with invalid name",
			fs: fstest.MapFS{
				"SKILL.md": {
					Data: []byte("---\nname: BadName\ndescription: Bad\n---\nBody\n"),
				},
			},
			wantErr:    true,
			errContain: "lowercase letter",
		},
		{
			name: "root skill with extra fields",
			fs: fstest.MapFS{
				"SKILL.md": {
					Data: []byte("---\nname: my-skill\ndescription: Skill\ncustomField: hello\n---\nBody\n"),
				},
			},
			wantName: "my-skill",
			wantDesc: "Skill",
			wantBody: "Body\n",
		},
		{
			name: "missing SKILL.md",
			fs: fstest.MapFS{
				"README.md": {Data: []byte("# Hello")},
			},
			wantErr:    true,
			errContain: "open SKILL.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadRootSkill(tt.fs)
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
		})
	}
}

func TestLoadRootSkill_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"SKILL.md": {
			Data: []byte("---\nname: my-skill\n---\nBody\n"),
		},
	}
	skill, err := LoadRootSkill(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRef := ir.SourceRef{File: "SKILL.md", Line: 1}
	if skill.Source != wantRef {
		t.Errorf("source: got %+v, want %+v", skill.Source, wantRef)
	}
}

// TestSkillSupportFilesExcludeVCSInternals pins the discovery boundary for
// root skills: a repository that IS a skill carries its own files as support
// files, but VCS internals (.git) are never skill content — a marketplace
// clone keeps .git on disk, and publishing object databases into installable
// archives (or reading multi-megabyte pack files as bounded support reads)
// is both wrong and a hard failure waiting to happen.
func TestSkillSupportFilesExcludeVCSInternals(t *testing.T) {
	rootFS := fstest.MapFS{
		"SKILL.md":                {Data: []byte("---\nname: root-skill\ndescription: Root skill\n---\nBody")},
		"notes.md":                {Data: []byte("notes")},
		".git/config":             {Data: []byte("[core]")},
		".git/HEAD":               {Data: []byte("ref: refs/heads/main")},
		"vendor/lib/.git/config":  {Data: []byte("[core]")},
		"vendor/lib/reference.md": {Data: []byte("reference")},
	}

	root, err := LoadRootSkill(rootFS)
	if err != nil {
		t.Fatalf("LoadRootSkill: %v", err)
	}
	if want := []string{"notes.md", "vendor/lib/reference.md"}; !slices.Equal(root.SupportFiles, want) {
		t.Errorf("root support files = %#v, want %v", root.SupportFiles, want)
	}

	dirFS := fstest.MapFS{
		"review/SKILL.md":          {Data: []byte("---\nname: review\ndescription: Review\n---\nBody")},
		"review/notes.md":          {Data: []byte("notes")},
		"review/.git/config":       {Data: []byte("[core]")},
		"review/references/api.md": {Data: []byte("api")},
	}
	dir, err := LoadSkill(dirFS, "review")
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if want := []string{"notes.md", "references/api.md"}; !slices.Equal(dir.SupportFiles, want) {
		t.Errorf("directory support files = %#v, want %v", dir.SupportFiles, want)
	}
}
