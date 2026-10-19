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
	for ref, want := range map[string]bool{
		"0123456789abcdef0123456789abcdef01234567":                         true,
		"0123456789ABCDEF0123456789ABCDEF01234567":                         true,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": true,
		"0123456789abcdef0123456789abcdef0123456":                          false,
		"0123456789abcdef0123456789abcdef012345678":                        false,
		"0123456789abcdef0123456789abcdef9999999999":                       false,
		"0123456789abcdef0123456789abcdef0123456g":                         false,
		"v1.2.3":  false,
		"abc1234": false,
		"main":    false,
		"":        false,
	} {
		if got := IsFullObjectID(ref); got != want {
			t.Errorf("IsFullObjectID(%q) = %v, want %v", ref, got, want)
		}
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
