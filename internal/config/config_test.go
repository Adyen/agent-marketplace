package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ks is a shorthand for constructing KindSelector with includes only.
func ks(include ...string) KindSelector {
	if len(include) == 0 {
		return KindSelector{}
	}
	return KindSelector{Include: include}
}

// kse constructs a KindSelector with both include and exclude.
func kse(include, exclude []string) KindSelector {
	return KindSelector{Include: include, Exclude: exclude}
}

// parseRuntimeV4 exercises the supported authored parser and runtime adapter.
// Expected selections in callers remain independently authored.
func parseRuntimeV4(t *testing.T, input string) *Config {
	t.Helper()
	parsed, err := ParseV4Config([]byte(input), "", true)
	if err != nil {
		t.Fatalf("parse v4 config: %v", err)
	}
	cfg, err := parsed.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatalf("adapt v4 runtime config: %v", err)
	}
	return cfg
}

func TestParseRepoConfig(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
  other:
    git: git@gitlab.example.com:example-org/example-other-skills.git
    ref: v1.2.3
resources:
  infra:
    include: [skill/vm-lifecycle]
    exclude: [skill/firewall-rule-lifecycle]
    tags: [k8s]
`

	cfg := parseRuntimeV4(t, input)

	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}

	if cfg.Registries[0].URL != "git@gitlab.example.com:example-org/example-infra-skills.git" {
		t.Fatalf("expected first registry URL to be parsed, got %q", cfg.Registries[0].URL)
	}

	if cfg.Registries[0].Ref != "main" {
		t.Fatalf("expected first registry ref to be parsed, got %q", cfg.Registries[0].Ref)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected first registry skills include to be parsed, got %#v", cfg.Registries[0].Skills.Include)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Tags, []string{"k8s"}) {
		t.Fatalf("expected first registry tags to be parsed, got %#v", cfg.Registries[0].Tags)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Exclude, []string{"firewall-rule-lifecycle"}) {
		t.Fatalf("expected first registry skills exclude to be parsed, got %#v", cfg.Registries[0].Skills.Exclude)
	}

	if cfg.Registries[0].Required {
		t.Fatalf("expected required to default to false for repo config entries")
	}

	if !cfg.Registries[1].Skills.IsZero() {
		t.Fatalf("expected absent per-entry skills to remain zero, got %#v", cfg.Registries[1].Skills)
	}

	if cfg.Registries[1].Tags != nil {
		t.Fatalf("expected absent per-entry tags to remain nil, got %#v", cfg.Registries[1].Tags)
	}

	if cfg.Tags != nil {
		t.Fatalf("source-local tags must not become top-level defaults, got %#v", cfg.Tags)
	}
}

func TestParseResourceListForm(t *testing.T) {
	cfg := parseRuntimeV4(t, `
apiVersion: agent-marketplace/v4
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
resources:
  infra: [skill/entry-skill]
`)
	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"entry-skill"}) {
		t.Fatalf("resource shorthand did not map to runtime includes: %#v", cfg.Registries[0].Skills)
	}
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nskills: [entry-skill]\n"))
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.legacy-key" {
		t.Fatalf("legacy selector key error = %v, want v4.legacy-key", err)
	}
}

func TestParseRequiredConfig(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  a-security:
    git: git@gitlab.example.com:example-org/example-security-skills.git
    ref: main
  b-infra:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: stable
resources:
  a-security: {all: true, required: true}
`

	cfg := parseRuntimeV4(t, input)

	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}

	if !cfg.Registries[0].Required {
		t.Fatalf("expected required registry to parse as true")
	}

	if cfg.Registries[1].Required {
		t.Fatalf("expected missing required field to default to false")
	}
}

func TestConfigValidateRequiresRegistryURLAndRef(t *testing.T) {
	testCases := []struct {
		name        string
		config      Config
		wantMessage string
	}{
		{
			name: "missing url",
			config: Config{
				APIVersion: APIVersionV4,
				Harnesses:  []string{"factory"},
				Registries: []RegistryEntry{{Ref: "main"}},
			},
			wantMessage: "registries[0].url is required",
		},
		{
			name: "missing ref",
			config: Config{
				APIVersion: APIVersionV4,
				Harnesses:  []string{"factory"},
				Registries: []RegistryEntry{{URL: "git@gitlab.example.com:example-org/example-infra-skills.git"}},
			},
			wantMessage: "registries[0].ref is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if err == nil {
				t.Fatalf("expected validation error")
			}

			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantMessage, err.Error())
			}
		})
	}
}

func TestConfigValidateAcceptsValidConfig(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@gitlab.example.com:example-org/example-security-skills.git", Ref: "main", Required: true},
			{URL: "git@gitlab.example.com:example-org/example-infra-skills.git", Ref: "v1.0.0", Skills: ks("vm-lifecycle")},
			{URL: "/local/skills", Skills: ks("local-skill")},
		},
		Tags: []string{"network"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass validation: %v", err)
	}
}

func TestConfigValidateRejectsUnsupportedVersions(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3", "agent-marketplace/v1", "agent-marketplace/v5"} {
		t.Run(version, func(t *testing.T) {
			cfg := Config{
				APIVersion: version,
				Harnesses:  []string{"factory"},
				Registries: []RegistryEntry{{URL: "git@example.com:repo.git", Ref: "main"}},
			}
			for _, validate := range []func() error{cfg.Validate, cfg.ValidateWithoutHarnesses} {
				err := validate()
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != "apiVersion" {
					t.Fatalf("version %q error = %v, want v4.api-version for apiVersion", version, err)
				}
			}
			cfg.APIVersion = APIVersionV4
			if err := cfg.Validate(); err != nil {
				t.Fatalf("otherwise-identical v4 control: %v", err)
			}
		})
	}
}

func TestConfigValidateRejectsPathTraversal(t *testing.T) {
	base := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"absolute path", "/tmp/evil", "path must be relative"},
		{"parent traversal", "../escape", "path must not escape the registry root"},
		{"deep traversal", "a/../../escape", "path must not escape the registry root"},
		{"valid relative path", ".factory", ""},
		{"valid nested path", "teams/dev", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Registries = []RegistryEntry{{
				URL:  "git@example.com:repo.git",
				Ref:  "main",
				Path: tc.path,
			}}
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestConfigValidateRejectsLocalURLTraversal(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"bare parent", "..", "must not contain '..'"},
		{"parent traversal", "../escape", "must not contain '..'"},
		{"deep traversal", "foo/../..", "must not contain '..'"},
		{"home parent", "~/..", "must not contain '..'"},
		{"home nested parent", "~/foo/../..", "must not contain '..'"},
		{"valid relative", "./skills", ""},
		{"valid home", "~/my-skills", ""},
		{"valid absolute", "/opt/skills", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				APIVersion: APIVersionV4,
				Harnesses:  []string{"factory"},
				Registries: []RegistryEntry{{URL: tc.url}},
			}
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateBasicRejectsPathTraversal(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"absolute path", "/tmp/evil", "path must be relative"},
		{"parent traversal", "../escape", "path must not escape the registry root"},
		{"valid relative path", ".factory", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Registries: []RegistryEntry{{
					URL:  "git@example.com:repo.git",
					Ref:  "main",
					Path: tc.path,
				}},
			}
			err := cfg.ValidateBasic()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateBasicRejectsLocalURLTraversal(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"bare parent", "..", "must not contain '..'"},
		{"parent traversal", "../escape", "must not contain '..'"},
		{"home parent", "~/..", "must not contain '..'"},
		{"valid relative", "./skills", ""},
		{"valid absolute", "/opt/skills", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Registries: []RegistryEntry{{URL: tc.url}},
			}
			err := cfg.ValidateBasic()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestIsLocal(t *testing.T) {
	testCases := []struct {
		url  string
		ref  string
		want bool
	}{
		{"/absolute/path", "", true},
		{"file:///absolute/path", "", true},
		{"file:///absolute/path.git", "main", false},
		{"file:///absolute/path%2Egit", "main", false},
		{"/absolute/path.git", "", false},
		{"/absolute/path.git", "main", false},
		{"/absolute/path", "main", true},
		{"~/home/path", "", true},
		{"~/home/path", "main", true},
		{"~/home/path.git", "", false},
		{"./relative/path", "", true},
		{"./relative/path", "main", true},
		{"./relative/path.git", "", false},
		{".", "", true},
		{".", "main", true},
		{"..", "", true},
		{"..", "main", true},
		{"../parent/path", "", true},
		{"../parent/path", "main", true},
		{"  /trimmed  ", "", true},
		{"  /trimmed.git  ", "", false},
		{"git@host:repo.git", "main", false},
		{"https://example.com/repo.git", "main", false},
		{"", "", false},
		{"  ", "", false},
		{"relative-no-prefix", "", false},
	}

	for _, tc := range testCases {
		name := tc.url + ":" + tc.ref
		t.Run(name, func(t *testing.T) {
			entry := RegistryEntry{URL: tc.url, Ref: tc.ref}
			if got := entry.IsLocal(); got != tc.want {
				t.Fatalf("IsLocal(URL=%q, Ref=%q) = %v, want %v", tc.url, tc.ref, got, tc.want)
			}
		})
	}
}

func TestIsRepoRelativeLocal(t *testing.T) {
	testCases := []struct {
		url  string
		ref  string
		want bool
	}{
		{"/absolute/path", "", false},
		{"~/home/path", "", false},
		{"./relative/path", "", true},
		{"../parent/path", "", true},
		{".", "", true},
		{"..", "", true},
		{"./relative.git", "", true},
		{"not-relative", "", false},
		{"https://example.com/repo.git", "", false},
		{"", "", false},
		{"   ", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			entry := RegistryEntry{URL: tc.url, Ref: tc.ref}
			if got := entry.IsRepoRelativeLocal(); got != tc.want {
				t.Fatalf("IsRepoRelativeLocal(URL=%q, Ref=%q) = %v, want %v", tc.url, tc.ref, got, tc.want)
			}
		})
	}
}

func TestGlobalRelativeRegistryError(t *testing.T) {
	err := &GlobalRelativeRegistryError{URL: "./plugins/gitlab-mr"}
	msg := err.Error()
	if !strings.Contains(msg, "./plugins/gitlab-mr") {
		t.Fatalf("expected error to contain URL, got %q", msg)
	}
	if !strings.Contains(msg, "global config") {
		t.Fatalf("expected error to mention global config, got %q", msg)
	}
}

func TestValidateGlobalRegistryWrite(t *testing.T) {
	if err := ValidateGlobalRegistryWrite(
		RegistryEntry{URL: "https://example.com/remote.git"},
		RegistryEntry{URL: "/absolute/local"},
	); err != nil {
		t.Fatalf("expected portable registries to pass: %v", err)
	}

	err := ValidateGlobalRegistryWrite(
		RegistryEntry{URL: "https://example.com/remote.git"},
		RegistryEntry{URL: "./relative.git"},
	)
	if _, ok := err.(*GlobalRelativeRegistryError); !ok {
		t.Fatalf("expected GlobalRelativeRegistryError, got %T: %v", err, err)
	}
}

func TestResolveLocalPath(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	testCases := []struct {
		name    string
		entry   RegistryEntry
		baseDir string
		want    string
		wantErr bool
	}{
		{
			name:    "absolute path",
			entry:   RegistryEntry{URL: "/local/skills"},
			baseDir: "/repo",
			want:    "/local/skills",
		},
		{
			name:    "file URL path",
			entry:   RegistryEntry{URL: "file:///local/skills"},
			baseDir: "/repo",
			want:    "/local/skills",
		},
		{
			name:    "file URL bare git repo is remote",
			entry:   RegistryEntry{URL: "file:///local/registry.git", Ref: "main"},
			baseDir: "/repo",
			want:    "",
		},
		{
			name:    "relative path resolved against baseDir",
			entry:   RegistryEntry{URL: "./skills"},
			baseDir: "/repo",
			want:    "/repo/skills",
		},
		{
			name:    "bare dot resolves to baseDir",
			entry:   RegistryEntry{URL: "."},
			baseDir: "/repo",
			want:    "/repo",
		},
		{
			name:    "bare dot-dot rejected",
			entry:   RegistryEntry{URL: ".."},
			baseDir: "/repo/sub",
			wantErr: true,
		},
		{
			name:    "parent relative path rejected",
			entry:   RegistryEntry{URL: "../skills"},
			baseDir: "/repo/sub",
			wantErr: true,
		},
		{
			name:    "non-local entry returns empty",
			entry:   RegistryEntry{URL: "git@host:repo.git"},
			baseDir: "/repo",
			want:    "",
		},
		{
			name:    "home-relative path resolved",
			entry:   RegistryEntry{URL: "~/my-skills"},
			baseDir: "/repo",
			want:    "/home/test/my-skills",
		},
		{
			name:    "home-relative parent traversal rejected",
			entry:   RegistryEntry{URL: "~/.."},
			baseDir: "/repo",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.entry.ResolveLocalPath(tc.baseDir)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ResolveLocalPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigValidateLocalRegistryDoesNotRequireRef(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "/local/skills", Skills: ks("my-skill")},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected local registry without ref to pass validation: %v", err)
	}
}

func TestConfigValidateLocalRegistryWithRefAccepted(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "/local/skills", Ref: "main", Skills: ks("my-skill")},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected local registry with ref to pass validation: %v", err)
	}
}

func TestConfigValidateFileURLRegistryClassification(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		ref     string
		wantErr string
	}{
		{name: "plain file URL does not require ref", url: "file:///local/skills"},
		{name: "bare git file URL requires ref", url: "file:///local/registry.git", wantErr: "registries[0].ref is required"},
		{name: "bare git file URL with ref accepted", url: "file:///local/registry.git", ref: "main"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				APIVersion: APIVersionV4,
				Harnesses:  []string{"factory"},
				Registries: []RegistryEntry{{URL: tc.url, Ref: tc.ref, Skills: ks("my-skill")}},
			}
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no validation error, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected validation error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestConfigValidateHomeLocalRegistryWithoutRefAccepted(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "~/skills", Skills: ks("my-skill")},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected home-relative local registry without ref to pass validation: %v", err)
	}
}

func TestConfigValidateRelativeLocalRegistryWithRef(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: ".", Ref: "main", Path: "skills", Skills: ks("my-skill")},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected relative local registry with ref to pass validation: %v", err)
	}
}

func TestParseLocalRegistryConfig(t *testing.T) {
	const input = `
apiVersion: agent-marketplace/v4
sources:
  a-local: {directory: /home/user/my-skills}
  b-remote:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
resources:
  a-local: [skill/local-skill]
`

	cfg := parseRuntimeV4(t, input)

	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}

	if !cfg.Registries[0].IsLocal() {
		t.Fatalf("expected first registry to be local, got URL %q", cfg.Registries[0].URL)
	}

	if cfg.Registries[0].Ref != "" {
		t.Fatalf("expected local registry ref to be empty, got %q", cfg.Registries[0].Ref)
	}

	if cfg.Registries[1].IsLocal() {
		t.Fatalf("expected second registry to NOT be local, got URL %q", cfg.Registries[1].URL)
	}
}

func TestBundlePath(t *testing.T) {
	tests := []struct {
		name     string
		entry    RegistryEntry
		basePath string
		want     string
	}{
		{
			name:     "empty Path returns basePath unchanged",
			entry:    RegistryEntry{},
			basePath: "/cache/abc/main",
			want:     "/cache/abc/main",
		},
		{
			name:     "populated Path is joined",
			entry:    RegistryEntry{Path: "skills"},
			basePath: "/cache/abc/main",
			want:     "/cache/abc/main/skills",
		},
		{
			name:     "whitespace-only Path treated as empty",
			entry:    RegistryEntry{Path: "   "},
			basePath: "/cache/abc/main",
			want:     "/cache/abc/main",
		},
		{
			name:     "nested Path is joined",
			entry:    RegistryEntry{Path: "sub/skills"},
			basePath: "/root",
			want:     "/root/sub/skills",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.BundlePath(tc.basePath); got != tc.want {
				t.Fatalf("BundlePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSourceDiscriminatorsNormalizeURLAndPath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantURL   string
		wantPath  string
		wantRef   string
		wantLocal bool
	}{
		{
			name: "directory discriminator supplies local URL",
			input: `
apiVersion: agent-marketplace/v4
sources:
  local: {directory: /local/skills}
`,
			wantURL:   "/local/skills",
			wantPath:  "",
			wantLocal: true,
		},
		{
			name: "git discriminator retains ref",
			input: `
apiVersion: agent-marketplace/v4
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
`,
			wantURL:  "git@example.com:infra.git",
			wantPath: "",
			wantRef:  "main",
		},
		{
			name: "git subpath normalizes independently of URL",
			input: `
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: git@example.com:infra.git
    ref: main
    path: " skills "
`,
			wantURL:  "git@example.com:infra.git",
			wantPath: "skills",
			wantRef:  "main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseRuntimeV4(t, tc.input)
			if len(cfg.Registries) != 1 {
				t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
			}
			entry := cfg.Registries[0]
			if entry.URL != tc.wantURL {
				t.Fatalf("expected URL %q, got %q", tc.wantURL, entry.URL)
			}
			if entry.Path != tc.wantPath {
				t.Fatalf("expected Path %q, got %q", tc.wantPath, entry.Path)
			}
			if tc.wantRef != "" && entry.Ref != tc.wantRef {
				t.Fatalf("expected Ref %q, got %q", tc.wantRef, entry.Ref)
			}
			if tc.wantLocal && !entry.IsLocal() {
				t.Fatalf("expected entry to be local, got URL %q", entry.URL)
			}
		})
	}
}

func TestSourcePathDoesNotAliasOrigin(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {path: /local/skills}\n"))
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.source-discriminator" || validation.Path != "sources.local" {
		t.Fatalf("path-only source error = %v, want v4.source-discriminator", err)
	}
	cfg := parseRuntimeV4(t, "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: /local/skills}\n")
	if len(cfg.Registries) != 1 || cfg.Registries[0].URL != "/local/skills" || cfg.Registries[0].Path != "" || !cfg.Registries[0].IsLocal() {
		t.Fatalf("explicit directory control = %#v", cfg.Registries)
	}
}

func TestRuntimeLocalPathValidatesCorrectly(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "/local/skills", Skills: ks("my-skill")},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected local path registry to pass validation: %v", err)
	}
}

func TestEditorRoundTripPreservesConfigStructureAndPath(t *testing.T) {
	const input = `apiVersion: agent-marketplace/v4
harnesses: [factory, cursor]
sources:
  security:
    git: git@gitlab.example.com:example-org/example-security-skills.git
    ref: main
    path: .factory
  infra:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: v2.0.0
resources:
  security:
    required: true
    include: [skill/no-curl-bash]
    exclude: [skill/legacy-skill]
    tags: [security]
`
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(input), 0o640); err != nil {
		t.Fatal(err)
	}
	editor, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: configPath, DeclaringPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || string(candidate.Bytes) != input || candidate.Mode != 0o640 {
		t.Fatalf("no-op render changed bytes/mode: %+v", candidate)
	}
	binding, found, err := editor.LookupRegistry(RegistryEntry{URL: "git@gitlab.example.com:example-org/example-security-skills.git", Ref: "main", Path: ".factory"})
	if err != nil || !found {
		t.Fatalf("lookup source: found=%v, err=%v", found, err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{{Kind: ir.KindCommand, Name: "audit"}}}); err != nil {
		t.Fatal(err)
	}
	candidate, err = editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed || candidate.Mode != 0o640 {
		t.Fatalf("real edit did not render with existing mode: %+v", candidate)
	}
	cfg := parseRuntimeV4(t, string(candidate.Bytes))
	if cfg.APIVersion != APIVersionV4 || !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "cursor"}) || len(cfg.Registries) != 2 {
		t.Fatalf("config settings/structure changed: %#v", cfg)
	}
	infra, security := cfg.Registries[0], cfg.Registries[1]
	if infra.URL != "git@gitlab.example.com:example-org/example-infra-skills.git" || infra.Ref != "v2.0.0" || !infra.Skills.IsZero() {
		t.Fatalf("unrelated source changed: %#v", infra)
	}
	if security.URL != "git@gitlab.example.com:example-org/example-security-skills.git" || security.Ref != "main" || security.Path != ".factory" || !security.Required || !reflect.DeepEqual(security.Skills, kse([]string{"no-curl-bash"}, []string{"legacy-skill"})) || !reflect.DeepEqual(security.Tags, []string{"security"}) || !reflect.DeepEqual(security.Commands.Include, []string{"audit"}) {
		t.Fatalf("selection edit lost existing structure/path: %#v", security)
	}
	stored, err := os.ReadFile(configPath)
	if err != nil || string(stored) != input {
		t.Fatalf("render must not publish: bytes=%q, err=%v", stored, err)
	}
}

func TestResolveMaterializationModesScopePrecedence(t *testing.T) {
	global := &Config{Materialization: &MaterializationConfig{Mode: "file"}}
	repo := &Config{Materialization: &MaterializationConfig{Mode: "symlink"}}
	local := &LocalConfig{Materialization: &MaterializationConfig{Mode: "file"}}

	modes, err := ResolveMaterializationModes(global, repo, local)
	if err != nil {
		t.Fatalf("ResolveMaterializationModes: %v", err)
	}
	if modes.Machine != MaterializationModeFile {
		t.Fatalf("machine mode = %q, want global file mode", modes.Machine)
	}
	if modes.Project != MaterializationModeFile {
		t.Fatalf("project mode = %q, want local file override", modes.Project)
	}

	modes, err = ResolveMaterializationModes(global, repo, nil)
	if err != nil {
		t.Fatalf("ResolveMaterializationModes without local: %v", err)
	}
	if modes.Machine != MaterializationModeFile || modes.Project != MaterializationModeSymlink {
		t.Fatalf("repo must override project only, got %+v", modes)
	}

	modes, err = ResolveMaterializationModes(global, nil, nil)
	if err != nil {
		t.Fatalf("ResolveMaterializationModes global only: %v", err)
	}
	if modes.Machine != MaterializationModeFile || modes.Project != MaterializationModeFile {
		t.Fatalf("global must seed both scopes when no project layer sets a mode, got %+v", modes)
	}
}

func TestMaterializationTargetsComposeAcrossRuntimeLayersAndV4LocalIncludes(t *testing.T) {
	global := &Config{Materialization: &MaterializationConfig{
		Mode:    "file",
		Targets: map[string]string{"factory": "file", "devin": "symlink"},
	}}
	repo := &Config{Materialization: &MaterializationConfig{
		Targets: map[string]string{"factory": "symlink"},
	}}
	local := &LocalConfig{Materialization: &MaterializationConfig{
		Targets: map[string]string{"claude": "file"},
	}}
	merged := MergeLayers([]Layer{
		{Source: UserPersonal, Config: global},
		{Source: RepoCommitted, Config: repo},
		*LocalConfigAsLayer(local),
	})
	if merged.Materialization == nil || len(merged.Materialization.Targets) != 3 || merged.Materialization.Targets["factory"] != "symlink" || merged.Materialization.Targets["devin"] != "symlink" || merged.Materialization.Targets["claude"] != "file" {
		t.Fatalf("runtime layer target map = %#v", merged.Materialization)
	}
	dir := t.TempDir()
	fragment := filepath.Join(dir, "materialization.yaml")
	root := filepath.Join(dir, "config.local.yaml")
	if err := os.WriteFile(fragment, []byte("materialization:\n  targets: {devin: file, claude: file}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\ninclude: materialization.yaml\nmaterialization:\n  targets: {factory: symlink, devin: symlink}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	included, err := LoadLocalConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if included.Materialization == nil || len(included.Materialization.Targets) != 3 || included.Materialization.Targets["devin"] != "symlink" || included.Materialization.Targets["factory"] != "symlink" || included.Materialization.Targets["claude"] != "file" {
		t.Fatalf("v4 include target map = %#v", included.Materialization)
	}
	modes, err := ResolveMaterializationModes(global, repo, local)
	if err != nil {
		t.Fatal(err)
	}
	if modes.For("factory", false) != MaterializationModeSymlink || modes.For("devin", false) != MaterializationModeSymlink || modes.For("claude", false) != MaterializationModeFile {
		t.Fatalf("resolved runtime target modes = %+v", modes)
	}
}

func TestResolveMaterializationModesDefaultsToSymlink(t *testing.T) {
	modes, err := ResolveMaterializationModes(nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveMaterializationModes: %v", err)
	}
	if modes.Machine != MaterializationModeSymlink || modes.Project != MaterializationModeSymlink {
		t.Fatalf("default modes = %+v, want symlink for both scopes", modes)
	}
}

func TestMaterializationModeValidation(t *testing.T) {
	cfg := Config{Materialization: &MaterializationConfig{Mode: "copy"}}
	if err := cfg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "materialization.mode") {
		t.Fatalf("invalid materialization mode error = %v, want materialization.mode validation", err)
	}
	local := LocalConfig{Materialization: &MaterializationConfig{Mode: "copy"}}
	if err := local.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "materialization.mode") {
		t.Fatalf("invalid local materialization mode error = %v, want materialization.mode validation", err)
	}
	cfg.Materialization = &MaterializationConfig{Targets: map[string]string{"unknown": "symlink"}}
	if err := cfg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "materialization.targets") {
		t.Fatalf("invalid materialization target error = %v, want materialization.targets validation", err)
	}
	cfg.Materialization = &MaterializationConfig{Targets: map[string]string{"devin": "copy"}}
	if err := cfg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "materialization.targets.devin") {
		t.Fatalf("invalid target mode error = %v, want target-specific validation", err)
	}
}

func TestResolveMaterializationModesSupportsTargetOverrides(t *testing.T) {
	global := &Config{Materialization: &MaterializationConfig{
		Mode:    "file",
		Targets: map[string]string{"devin": "symlink"},
	}}
	modes, err := ResolveMaterializationModes(global, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := modes.For("factory", false); got != MaterializationModeFile {
		t.Fatalf("factory project mode = %q, want file", got)
	}
	if got := modes.For("devin", false); got != MaterializationModeSymlink {
		t.Fatalf("devin project mode = %q, want symlink", got)
	}
	if got := modes.For("devin", true); got != MaterializationModeSymlink {
		t.Fatalf("devin machine mode = %q, want symlink", got)
	}

	repo := &Config{Materialization: &MaterializationConfig{Targets: map[string]string{"devin": "file"}}}
	modes, err = ResolveMaterializationModes(global, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := modes.For("devin", false); got != MaterializationModeFile {
		t.Fatalf("repo override project mode = %q, want file", got)
	}
	if got := modes.For("devin", true); got != MaterializationModeSymlink {
		t.Fatalf("repo override changed machine mode to %q, want symlink", got)
	}
}
