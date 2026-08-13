package config

import (
	"fmt"
	"strings"
	"testing"
)

// DefaultAllowedRegistries is a test-only fixture. It is not consulted at
// runtime; the effective configuration layers supply the registry allowlist.
var DefaultAllowedRegistries = []string{
	`^[^/]*\.example\.com/`,
}

func TestValidateRegistryURLs_AllowedURLPasses(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "gitlab.example.com/team/repo.git", Ref: "main"},
		{URL: "registry.gitlab.example.com/team/repo.git", Ref: "main"},
		{URL: "git@gitlab.example.com:team/repo.git", Ref: "main"},
		{URL: "https://gitlab.example.com/team/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(entries, DefaultAllowedRegistries); err != nil {
		t.Fatalf("expected allowed URLs to pass, got: %v", err)
	}
}

func TestValidateRegistryURLs_BlockedURLFails(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "gitlab.com/someone/repo.git", Ref: "main"},
	}
	err := ValidateRegistryURLs(entries, DefaultAllowedRegistries)
	if err == nil {
		t.Fatal("expected blocked URL to fail")
	}
	if !strings.Contains(err.Error(), "gitlab.com/someone/repo.git") {
		t.Errorf("error should mention blocked URL, got: %v", err)
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Errorf("error should mention policy, got: %v", err)
	}
}

func TestValidateRegistryURLs_MixedAllowedAndBlocked(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "gitlab.example.com/team/repo.git", Ref: "main"},
		{URL: "gitlab.com/evil/repo.git", Ref: "main"},
		{URL: "registry.gitlab.example.com/team/repo.git", Ref: "main"},
	}
	err := ValidateRegistryURLs(entries, DefaultAllowedRegistries)
	if err == nil {
		t.Fatal("expected error because one URL is blocked")
	}
	if !strings.Contains(err.Error(), "gitlab.com/evil/repo.git") {
		t.Errorf("error should mention blocked URL, got: %v", err)
	}
}

func TestValidateRegistryURLs_LocalPathsExempt(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "./local/skills"},
		{URL: "./shared/skills"},
		{URL: "/opt/skills"},
		{URL: "~/my-skills"},
	}
	if err := ValidateRegistryURLs(entries, []string{`^never-match$`}); err != nil {
		t.Fatalf("local paths should be exempt from allowlist, got: %v", err)
	}
}

func TestValidateRegistryURLs_NilPatternsAllowAll(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "github.com/any/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(entries, nil); err != nil {
		t.Fatalf("nil patterns should allow all, got: %v", err)
	}
}

func TestValidateRegistryURLs_EmptyPatternsDenyAll(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "github.com/any/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(entries, []string{}); err == nil {
		t.Fatal("empty patterns should deny all non-local registries")
	}
}

func TestValidateRegistryURLs_GitSuffixLocalPathIsValidated(t *testing.T) {
	// A local path ending in .git is treated as a git bare repo and is NOT
	// exempt from the allowlist. An explicit empty allowlist should block it.
	entries := []RegistryEntry{
		{URL: "/tmp/repo.git", Ref: "main"},
	}
	err := ValidateRegistryURLs(entries, []string{})
	if err == nil {
		t.Fatal("expected .git local path to be blocked by empty allowlist")
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Errorf("error should mention allowed_registries, got: %v", err)
	}
}

func TestValidateRegistryURLs_CustomPatternsOverride(t *testing.T) {
	// Patterns can scope to specific repos, not just whole hosts.
	patterns := []string{`github\.com/myorg/`, `gitlab\.com/`}
	entries := []RegistryEntry{
		{URL: "github.com/myorg/repo.git", Ref: "main"},
		{URL: "gitlab.com/myorg/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(entries, patterns); err != nil {
		t.Fatalf("custom patterns should allow matching URLs, got: %v", err)
	}

	// Other org on github should be blocked.
	blocked := []RegistryEntry{
		{URL: "github.com/other-org/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(blocked, patterns); err == nil {
		t.Fatal("URL not matching custom patterns should be blocked")
	}

	// Example domain should be blocked by these custom patterns.
	blockedExample := []RegistryEntry{
		{URL: "gitlab.example.com/team/repo.git", Ref: "main"},
	}
	if err := ValidateRegistryURLs(blockedExample, patterns); err == nil {
		t.Fatal("URL not matching custom patterns should be blocked")
	}
}

func TestValidateRegistryURLs_InvalidPattern(t *testing.T) {
	patterns := []string{`[invalid(`}
	err := ValidateRegistryURLs(nil, patterns)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
	if !strings.Contains(err.Error(), "invalid allowed_registries pattern") {
		t.Errorf("error should mention invalid pattern, got: %v", err)
	}
}

func TestDefaultAllowedRegistries_MatchExampleDomains(t *testing.T) {
	testCases := []struct {
		url   string
		allow bool
	}{
		// Example domains are allowed by default.
		{"https://gitlab.example.com/team/repo.git", true},
		{"git@gitlab.example.com:team/repo.git", true},
		{"https://registry.gitlab.example.com/team/repo", true},
		{"https://gitlab.example.com/team/repo.git", true},
		{"gitlab.example.com/team/repo.git", true},
		// Non-example domains are blocked by default (no github.com in defaults).
		{"https://github.com/team/repo.git", false},
		{"git@github.com:team/repo.git", false},
		{"github.com/team/repo.git", false},
		{"https://gitlab.com/team/repo.git", false},
		// Subdomain spoofing: evil.example.com.evil.com host is NOT .example.com.
		{"https://evil.example.com.evil.com/repo.git", false},
		{"https://evil.github.com.evil.com/repo.git", false},
		// Path-component bypass: example.com in the path, not the host.
		{"https://attacker.com/x.example.com/registry.git", false},
	}
	for _, tc := range testCases {
		entry := RegistryEntry{URL: tc.url, Ref: "main"}
		err := ValidateRegistryURLs([]RegistryEntry{entry}, DefaultAllowedRegistries)
		if tc.allow && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tc.url, err)
		}
		if !tc.allow && err == nil {
			t.Errorf("expected %q to be blocked", tc.url)
		}
	}
}

func TestValidateRegistryURLs_ReturnsTypedBlockedError(t *testing.T) {
	entries := []RegistryEntry{
		{URL: "gitlab.com/someone/repo.git", Ref: "main"},
	}
	err := ValidateRegistryURLs(entries, DefaultAllowedRegistries)
	if err == nil {
		t.Fatal("expected blocked URL to fail")
	}
	if !IsRegistryURLBlockedError(err) {
		t.Fatalf("expected RegistryURLBlockedError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "gitlab.com/someone/repo.git") {
		t.Errorf("error should mention blocked URL, got: %v", err)
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Errorf("error should mention policy, got: %v", err)
	}
}

func TestOnlyRegistryURLBlockedErrors(t *testing.T) {
	blockedErr := &RegistryURLBlockedError{BlockedURLs: []string{"evil.com"}, AllowedPatterns: []string{".*"}}
	otherErr := fmt.Errorf("some other error")

	if !OnlyRegistryURLBlockedErrors([]error{blockedErr}) {
		t.Fatal("expected single blocked error to be reported as only blocked")
	}
	if OnlyRegistryURLBlockedErrors([]error{blockedErr, otherErr}) {
		t.Fatal("expected mixed errors not to be only blocked")
	}
	if OnlyRegistryURLBlockedErrors(nil) {
		t.Fatal("expected nil errors not to be only blocked")
	}
	if OnlyRegistryURLBlockedErrors([]error{}) {
		t.Fatal("expected empty errors not to be only blocked")
	}
}

func TestNormalizeRegistryURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://gitlab.example.com/team/repo.git", "gitlab.example.com/team/repo"},
		{"http://gitlab.example.com/team/repo.git", "gitlab.example.com/team/repo"},
		{"git://gitlab.example.com/team/repo.git", "gitlab.example.com/team/repo"},
		{"git@gitlab.example.com:team/repo.git", "gitlab.example.com/team/repo"},
		{"ssh://git@gitlab.example.com/team/repo.git", "gitlab.example.com/team/repo"},
		{"gitlab.example.com/team/repo.git", "gitlab.example.com/team/repo"},
		{"gitlab.example.com/team/repo", "gitlab.example.com/team/repo"},
		{"gitlab.example.com/team/repo.git/", "gitlab.example.com/team/repo"},
	}
	for _, tc := range tests {
		got := normalizeRegistryURL(tc.input)
		if got != tc.want {
			t.Errorf("normalizeRegistryURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
