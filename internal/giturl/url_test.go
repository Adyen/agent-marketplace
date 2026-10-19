package giturl

import (
	"strings"
	"testing"
)

func TestValidateRefRejectsUnsafeRawValuesAndComponents(t *testing.T) {
	for _, ref := range []string{
		" main",
		"main ",
		".hidden",
		"foo//bar",
		"foo.lock/bar",
		"foo./bar",
	} {
		t.Run(ref, func(t *testing.T) {
			if err := ValidateRef(ref); err == nil {
				t.Fatalf("ValidateRef(%q) accepted unsafe ref", ref)
			}
		})
	}
}

func TestValidateRefAllowsValidComponentRefs(t *testing.T) {
	for _, ref := range []string{
		"main",
		"feature/release-1.2",
		"refs/tags/v1.0.0",
		"origin/main",
	} {
		t.Run(ref, func(t *testing.T) {
			if err := ValidateRef(ref); err != nil {
				t.Fatalf("ValidateRef(%q) returned error: %v", ref, err)
			}
		})
	}
}

func TestIsFullObjectID(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
		want      bool
	}{
		{"sha1 lowercase", "0123456789abcdef0123456789abcdef01234567", true},
		{"sha1 uppercase", "0123456789ABCDEF0123456789ABCDEF01234567", true},
		{"sha256", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"sha1 non-hex", "0123456789abcdef0123456789abcdef0123456g", false},
		{"sha256 non-hex", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg", false},
		{"39 characters", "0123456789abcdef0123456789abcdef0123456", false},
		{"41 characters", "0123456789abcdef0123456789abcdef012345678", false},
		{"42 characters", "0123456789abcdef0123456789abcdef9999999999", false},
		{"version tag", "v1.2.3", false},
		{"abbreviated", "abc1234", false},
		{"branch", "main", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFullObjectID(tc.ref); got != tc.want {
				t.Errorf("IsFullObjectID(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestValidateURLRejectsWhitespaceAndBackslashes(t *testing.T) {
	for _, url := range []string{
		" https://example.com/repo.git",
		"https://example.com/repo.git ",
		`example.com:org\repo.git`,
		`git@example.com:org\repo.git`,
		`https://example.com/org\repo.git`,
		`file:///tmp/repo\.git`,
	} {
		t.Run(strings.ReplaceAll(url, "\\", "slash"), func(t *testing.T) {
			if err := ValidateURL(url); err == nil {
				t.Fatalf("ValidateURL(%q) accepted unsafe URL", url)
			}
		})
	}
}

func TestValidateURLAllowsSupportedGitOrigins(t *testing.T) {
	for _, url := range []string{
		"https://example.com/org/repo.git",
		"ssh://git@example.com/org/repo.git",
		"git@example.com:org/repo.git",
		"file:///tmp/repo.git",
	} {
		t.Run(url, func(t *testing.T) {
			if err := ValidateURL(url); err != nil {
				t.Fatalf("ValidateURL(%q) returned error: %v", url, err)
			}
		})
	}
}
