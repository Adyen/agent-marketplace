package pipeline

import (
	"testing"
)

func TestPathBase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"skills/helm-chart-mirroring", "helm-chart-mirroring"},
		{"skills", "skills"},
		{".factory/skills/openspec-apply-change", "openspec-apply-change"},
		{"skills/", "skills"},
		{"single", "single"},
		{"", ""},
	}
	for _, tt := range tests {
		got := pathBase(tt.input)
		if got != tt.want {
			t.Errorf("pathBase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseGitWebURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  ParsedWebURL
	}{
		// GitLab tree URLs
		{
			name:  "gitlab tree with path to skill dir",
			input: "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills/helm-chart-mirroring",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "skills/helm-chart-mirroring",
				Ok:     true,
			},
		},
		{
			name:  "gitlab tree with path to parent dir",
			input: "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "skills",
				Ok:     true,
			},
		},
		{
			name:  "gitlab tree root with trailing slash",
			input: "https://gitlab.example.com/example-org/infra-skills/-/tree/main/",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "",
				Ok:     true,
			},
		},
		{
			name:  "gitlab tree root without trailing slash",
			input: "https://gitlab.example.com/example-org/infra-skills/-/tree/main",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "",
				Ok:     true,
			},
		},
		{
			name:  "gitlab tree with nested path",
			input: "https://gitlab.example.com/example-org/example-resources/-/tree/main/.factory/skills/openspec-apply-change",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/example-resources",
				Ref:    "main",
				Path:   ".factory/skills/openspec-apply-change",
				Ok:     true,
			},
		},
		// GitLab blob URLs — path becomes the *directory* containing the file
		{
			name:  "gitlab blob to SKILL.md returns parent dir",
			input: "https://gitlab.example.com/example-org/infra-skills/-/blob/main/skills/helm-chart-mirroring/SKILL.md",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "skills/helm-chart-mirroring",
				Ok:     true,
			},
		},
		{
			name:  "gitlab blob at repo root returns empty path",
			input: "https://gitlab.example.com/example-org/infra-skills/-/blob/main/README.md",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "main",
				Path:   "",
				Ok:     true,
			},
		},
		// GitHub URLs
		{
			name:  "github tree with path",
			input: "https://github.com/owner/repo/tree/main/skills/my-skill",
			want: ParsedWebURL{
				GitURL: "git@github.com:owner/repo",
				Ref:    "main",
				Path:   "skills/my-skill",
				Ok:     true,
			},
		},
		{
			name:  "github tree root",
			input: "https://github.com/owner/repo/tree/main",
			want: ParsedWebURL{
				GitURL: "git@github.com:owner/repo",
				Ref:    "main",
				Path:   "",
				Ok:     true,
			},
		},
		{
			name:  "github blob returns parent dir",
			input: "https://github.com/owner/repo/blob/main/skills/my-skill/SKILL.md",
			want: ParsedWebURL{
				GitURL: "git@github.com:owner/repo",
				Ref:    "main",
				Path:   "skills/my-skill",
				Ok:     true,
			},
		},
		// Bare project URLs (no tree/blob)
		{
			name:  "gitlab bare project URL",
			input: "https://gitlab.example.com/example-org/infra-skills",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "",
				Path:   "",
				Ok:     true,
			},
		},
		{
			name:  "github bare project URL",
			input: "https://github.com/owner/repo",
			want: ParsedWebURL{
				GitURL: "git@github.com:owner/repo",
				Ref:    "",
				Path:   "",
				Ok:     true,
			},
		},
		// Non-web URLs are not parsed (returned as-is via ResolveFromURL)
		{
			name:  "git ssh URL not parsed",
			input: "git@gitlab.example.com:example-org/infra-skills",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "https URL ending in .git not parsed",
			input: "https://gitlab.example.com/example-org/infra-skills.git",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "local path not parsed",
			input: "/opt/skills",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "empty string not parsed",
			input: "",
			want:  ParsedWebURL{Ok: false},
		},
		// GitLab with tag ref (not branch)
		{
			name:  "gitlab tree with tag ref",
			input: "https://gitlab.example.com/example-org/infra-skills/-/tree/v2.1.0/skills",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:example-org/infra-skills",
				Ref:    "v2.1.0",
				Path:   "skills",
				Ok:     true,
			},
		},
		// Deeply nested group
		{
			name:  "gitlab nested group",
			input: "https://gitlab.example.com/team/platform/infra/skills/-/tree/main/skills",
			want: ParsedWebURL{
				GitURL: "git@gitlab.example.com:team/platform/infra/skills",
				Ref:    "main",
				Path:   "skills",
				Ok:     true,
			},
		},
		// Negative: GitLab non-tree/blob verbs must not be parsed
		{
			name:  "gitlab commits verb not parsed",
			input: "https://gitlab.example.com/example-org/infra-skills/-/commits/main",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "gitlab merge_requests verb not parsed",
			input: "https://gitlab.example.com/example-org/infra-skills/-/merge_requests/42",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "gitlab pipelines verb not parsed",
			input: "https://gitlab.example.com/example-org/infra-skills/-/pipelines",
			want:  ParsedWebURL{Ok: false},
		},
		// Negative: GitHub subpages must not be parsed as bare project URLs
		{
			name:  "github issues subpage not parsed",
			input: "https://github.com/owner/repo/issues",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "github pulls subpage not parsed",
			input: "https://github.com/owner/repo/pulls",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "github actions subpage not parsed",
			input: "https://github.com/owner/repo/actions",
			want:  ParsedWebURL{Ok: false},
		},
		{
			name:  "github settings subpage not parsed",
			input: "https://github.com/owner/repo/settings",
			want:  ParsedWebURL{Ok: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseGitWebURL(tt.input)
			if got.Ok != tt.want.Ok {
				t.Fatalf("Ok = %v, want %v", got.Ok, tt.want.Ok)
			}
			if got.Ok {
				if got.GitURL != tt.want.GitURL {
					t.Errorf("GitURL = %q, want %q", got.GitURL, tt.want.GitURL)
				}
				if got.Ref != tt.want.Ref {
					t.Errorf("Ref = %q, want %q", got.Ref, tt.want.Ref)
				}
				if got.Path != tt.want.Path {
					t.Errorf("Path = %q, want %q", got.Path, tt.want.Path)
				}
			}
		})
	}
}

func TestResolveFromURL(t *testing.T) {
	tests := []struct {
		name         string
		rawURL       string
		explicitRef  string
		explicitPath string
		wantGitURL   string
		wantRef      string
		wantPath     string
	}{
		{
			name:       "web URL with no explicit flags",
			rawURL:     "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills",
			wantGitURL: "git@gitlab.example.com:example-org/infra-skills",
			wantRef:    "main",
			wantPath:   "skills",
		},
		{
			name:        "web URL with explicit ref overriding parsed ref",
			rawURL:      "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills",
			explicitRef: "stable",
			wantGitURL:  "git@gitlab.example.com:example-org/infra-skills",
			wantRef:     "stable",
			wantPath:    "skills",
		},
		{
			name:         "web URL with explicit path overriding parsed path",
			rawURL:       "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills",
			explicitPath: ".factory",
			wantGitURL:   "git@gitlab.example.com:example-org/infra-skills",
			wantRef:      "main",
			wantPath:     ".factory",
		},
		{
			name:        "git SSH URL passed through unchanged",
			rawURL:      "git@gitlab.example.com:example-org/infra-skills",
			explicitRef: "main",
			wantGitURL:  "git@gitlab.example.com:example-org/infra-skills",
			wantRef:     "main",
			wantPath:    "",
		},
		{
			name:        "bare project URL with explicit ref",
			rawURL:      "https://gitlab.example.com/example-org/infra-skills",
			explicitRef: "main",
			wantGitURL:  "git@gitlab.example.com:example-org/infra-skills",
			wantRef:     "main",
			wantPath:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotRef, gotPath, gotOk := ResolveFromURL(tt.rawURL, tt.explicitRef, tt.explicitPath)
			if !gotOk {
				t.Fatal("expected ok=true")
			}
			if gotURL != tt.wantGitURL {
				t.Errorf("gitURL = %q, want %q", gotURL, tt.wantGitURL)
			}
			if gotRef != tt.wantRef {
				t.Errorf("ref = %q, want %q", gotRef, tt.wantRef)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}
