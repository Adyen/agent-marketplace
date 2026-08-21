package releaseguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseTagAcceptsStrictSemVer(t *testing.T) {
	t.Parallel()

	tags := []string{
		"v0.0.0",
		"v1.2.3",
		"v10.20.30",
		"v1.0.0-rc.1",
		"v1.0.0-alpha",
		"v1.0.0-alpha.beta.1",
		"v1.0.0-0.3.7",
		"v0.0.0-dev",
		"v1.0.0-x-y-z.-",
	}

	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()

			res := runValidateScript(t, tag)
			if res.exitCode != 0 {
				t.Fatalf("tag %q rejected (exit %d)\nstderr: %s", tag, res.exitCode, res.stderr)
			}
			if !strings.Contains(res.stdout, "accepted release tag: "+tag) {
				t.Fatalf("tag %q: unexpected stdout %q", tag, res.stdout)
			}
		})
	}
}

// TestValidateReleaseTagRejectsShellBearingTags covers the reproduced exploit:
// Git accepts these as tag names, and before the guard existed each one reached
// Make expansion or a shell word in the privileged release job.
func TestValidateReleaseTagRejectsShellBearingTags(t *testing.T) {
	t.Parallel()

	tags := map[string]string{
		"command substitution":      "v1.0.0$(id)",
		"backtick substitution":     "v1.0.0`id`",
		"single quote break out":    "v1.0.0'; id; '",
		"double quote break out":    "v1.0.0\"; id; \"",
		"command separator":         "v1.0.0; id",
		"logical and":               "v1.0.0 && id",
		"pipeline":                  "v1.0.0 | id",
		"redirect":                  "v1.0.0 > /tmp/pwned",
		"make shell function":       "$(shell id)",
		"make variable reference":   "$(VERSION)",
		"newline smuggled tag":      "v1.0.0\nv9.9.9",
		"carriage return":           "v1.0.0\r",
		"leading dash":              "-v1.0.0",
		"path traversal":            "../../etc/passwd",
		"absolute path":             "/v1.0.0",
		"nul adjacent control byte": "v1.0.0\x07",
		"tab":                       "v1.0.0\t",
		"space":                     "v1.0.0 ",
		"glob":                      "v1.0.*",
		"env expansion":             "v1.0.0$HOME",
	}

	for name, tag := range tags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := runValidateScript(t, tag)
			if res.exitCode != 1 {
				t.Fatalf("tag %q: got exit %d, want 1\nstdout: %s\nstderr: %s", tag, res.exitCode, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "rejected release tag") {
				t.Fatalf("tag %q: unexpected stderr %q", tag, res.stderr)
			}
			if strings.Contains(res.stdout, "accepted") {
				t.Fatalf("tag %q: reported acceptance on stdout: %q", tag, res.stdout)
			}
		})
	}
}

func TestValidateReleaseTagRejectsNonReleaseVersions(t *testing.T) {
	t.Parallel()

	tags := map[string]string{
		"empty":                 "",
		"missing v prefix":      "1.2.3",
		"leading zero major":    "v01.2.3",
		"leading zero patch":    "v1.2.03",
		"missing patch":         "v1.2",
		"four components":       "v1.2.3.4",
		"build metadata":        "v1.2.3+build.1",
		"empty prerelease":      "v1.2.3-",
		"leading zero numeric":  "v1.2.3-01",
		"empty identifier":      "v1.2.3-alpha..1",
		"git describe fallback": "a58e897",
		"dirty describe":        "a58e897-dirty",
		"uppercase v":           "V1.2.3",
		"latest alias":          "latest",
		"release branch name":   "release/1.2.3",
	}

	for name, tag := range tags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := runValidateScript(t, tag)
			if res.exitCode != 1 {
				t.Fatalf("tag %q: got exit %d, want 1\nstdout: %s\nstderr: %s", tag, res.exitCode, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "rejected release tag") {
				t.Fatalf("tag %q: unexpected stderr %q", tag, res.stderr)
			}
		})
	}
}

// TestValidateReleaseTagRedactsNonPrintableBytesInDiagnostics keeps a crafted
// tag from forging CI log output with terminal escape sequences.
func TestValidateReleaseTagRedactsNonPrintableBytesInDiagnostics(t *testing.T) {
	t.Parallel()

	res := runValidateScript(t, "v1.0.0\x1b[31mRED")
	if res.exitCode != 1 {
		t.Fatalf("got exit %d, want 1\nstderr: %s", res.exitCode, res.stderr)
	}
	if strings.Contains(res.stderr, "\x1b") {
		t.Fatalf("escape byte echoed back verbatim: %q", res.stderr)
	}
}

// TestValidateReleaseTagRejectsInvalidUTF8 covers byte sequences that are not
// valid UTF-8 text. Before the script pinned LC_ALL=C, such input made tr
// abort with "Illegal byte sequence" under a UTF-8 locale: the script died via
// set -e with no rejection diagnostic, and the exit path depended on the
// caller's locale. The gate must instead reject cleanly and redact the bytes.
func TestValidateReleaseTagRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	tags := map[string]string{
		"lone continuation byte": "v1.0.0\x80",
		"truncated 2-byte lead":  "v1.0.0\xc3",
		"invalid byte pair":      "v1.0.0\xff\xfe",
		"overlong NUL encoding":  "v1.0.0\xc0\x80",
	}

	for name, tag := range tags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := runValidateScript(t, tag)
			if res.exitCode != 1 {
				t.Fatalf("tag %q: got exit %d, want 1\nstdout: %s\nstderr: %s", tag, res.exitCode, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "rejected release tag") {
				t.Fatalf("tag %q: rejected without the guard diagnostic (set -e exit?)\nstderr: %q", tag, res.stderr)
			}
			if strings.Contains(res.stdout, "accepted") {
				t.Fatalf("tag %q: reported acceptance on stdout: %q", tag, res.stdout)
			}
			for _, b := range []string{"\x80", "\xc3", "\xff", "\xfe", "\xc0"} {
				if strings.Contains(res.stderr, b) {
					t.Fatalf("tag %q: non-printable byte echoed back verbatim: %q", tag, res.stderr)
				}
			}
		})
	}
}

// TestValidateReleaseTagDecisionIsLocaleIndependent pins the script's
// `export LC_ALL=C` from the caller's side: accept/reject decisions and
// diagnostic redaction must not depend on the locale the invoking environment
// requests, so a runner defaulting to a UTF-8 locale cannot change what the
// gate admits or crash redaction. Locales that are not installed fall back to
// C in libc, which still exercises the override on hosts where they are.
func TestValidateReleaseTagDecisionIsLocaleIndependent(t *testing.T) {
	t.Parallel()

	locales := []string{"C", "POSIX", "C.UTF-8", "en_US.UTF-8", "de_DE.UTF-8", "tr_TR.UTF-8"}

	cases := []struct {
		tag  string
		want int
	}{
		{"v1.2.3", 0},
		{"v1.2.3-rc.1", 0},
		{"v1.2.3$(id)", 1},
		{"v1.2.3\xff\xfe", 1},
		{"v1.2.3-ü", 1}, // printable non-ASCII: the contract is ASCII-only
	}

	for _, locale := range locales {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			env := []string{"LC_ALL=" + locale, "LANG=" + locale, "LC_CTYPE=" + locale}
			for _, tc := range cases {
				res := runValidateScriptEnv(t, env, tc.tag)
				if res.exitCode != tc.want {
					t.Fatalf("locale %s, tag %q: got exit %d, want %d\nstdout: %s\nstderr: %s",
						locale, tc.tag, res.exitCode, tc.want, res.stdout, res.stderr)
				}
				if tc.want == 1 && !strings.Contains(res.stderr, "rejected release tag") {
					t.Fatalf("locale %s, tag %q: rejected without the guard diagnostic\nstderr: %q",
						locale, tc.tag, res.stderr)
				}
				if tc.want == 0 && !strings.Contains(res.stdout, "accepted release tag: "+tc.tag) {
					t.Fatalf("locale %s, tag %q: unexpected stdout %q", locale, tc.tag, res.stdout)
				}
			}
		})
	}
}

// TestValidateScriptPinsByteLocale asserts the locale override structurally:
// the behavioral tests above cannot distinguish an uninstalled locale (which
// libc silently falls back from to C) from a missing override, so the override
// itself is pinned here.
func TestValidateScriptPinsByteLocale(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), validateScriptRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", validateScriptRelPath, err)
	}
	if !strings.Contains(string(data), "export LC_ALL=C") {
		t.Errorf("%s does not pin LC_ALL=C; tag acceptance becomes locale-dependent", validateScriptRelPath)
	}
}

func TestValidateReleaseTagRequiresExactlyOneArgument(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	script := root + "/" + validateScriptRelPath

	for name, args := range map[string][]string{
		"no arguments":  {},
		"two arguments": {"v1.0.0", "v1.0.1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := run(t, root, baseEnv(), script, args...)
			if res.exitCode != 2 {
				t.Fatalf("got exit %d, want 2 (usage)\nstdout: %s\nstderr: %s", res.exitCode, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "usage:") {
				t.Fatalf("unexpected stderr %q", res.stderr)
			}
		})
	}
}
