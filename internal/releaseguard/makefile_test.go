package releaseguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMakeTreatsVersionAsDataNotMakeSyntax covers the reproduced exploit where a
// tag name such as `$(shell id)` was expanded by Make while parsing the file, so
// it executed for every target regardless of any later validation.
func TestMakeTreatsVersionAsDataNotMakeSyntax(t *testing.T) {
	for name, viaCommandLine := range map[string]bool{
		"environment override":  false,
		"command line override": true,
	} {
		t.Run(name, func(t *testing.T) {
			marker := sentinel(t, "make-expansion")
			injected := "$(shell touch " + marker + ")"

			var res cmdResult
			if viaCommandLine {
				res = runMake(t, baseEnv(), "version", "VERSION="+injected)
			} else {
				res = runMake(t, baseEnv("VERSION="+injected), "version")
			}

			assertNotCreated(t, marker, res)
			if res.exitCode != 0 {
				t.Fatalf("make version failed (exit %d)\nstderr: %s", res.exitCode, res.stderr)
			}
			if got := strings.TrimSpace(res.stdout); got != injected {
				t.Fatalf("VERSION was rewritten by Make: got %q, want the raw text %q", got, injected)
			}
		})
	}
}

// TestMakeReleaseRefusesShellBearingTag asserts the privileged path fails closed:
// the guard runs before any artifact is produced, and the quoting keeps injected
// shell syntax from executing while it is being rejected.
func TestMakeReleaseRefusesShellBearingTag(t *testing.T) {
	tags := map[string]string{
		"single quote break out": "v1.0.0'; touch %s; '",
		"double quote break out": "v1.0.0\"; touch %s; \"",
		"command substitution":   "v1.0.0$(touch %s)",
		"backtick substitution":  "v1.0.0`touch %s`",
		"command separator":      "v1.0.0; touch %s",
	}

	for name, template := range tags {
		t.Run(name, func(t *testing.T) {
			marker := sentinel(t, "release-shell")
			tag := strings.ReplaceAll(template, "%s", marker)
			distDir := filepath.Join(filepath.Dir(marker), "dist")

			res := runMake(t, baseEnv("VERSION="+tag), "release", "DIST_DIR="+distDir)

			assertNotCreated(t, marker, res)
			assertNotCreated(t, distDir, res)
			if res.exitCode == 0 {
				t.Fatalf("make release accepted %q\nstdout: %s", tag, res.stdout)
			}
			if !strings.Contains(res.stderr, "rejected release tag") {
				t.Fatalf("make release failed without the tag guard diagnostic\nstderr: %s", res.stderr)
			}
		})
	}
}

func TestMakeCheckReleaseTagAcceptsReleaseTag(t *testing.T) {
	res := runMake(t, baseEnv("VERSION=v1.2.3-rc.1"), "check-release-tag")
	if res.exitCode != 0 {
		t.Fatalf("check-release-tag rejected a valid tag (exit %d)\nstderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, "accepted release tag: v1.2.3-rc.1") {
		t.Fatalf("unexpected stdout %q", res.stdout)
	}
}

// TestMakeCheckReleaseTagRejectsUntaggedDescribeOutput documents that an
// untagged or dirty checkout cannot produce release artifacts: the
// `git describe --always --dirty` fallback (a bare commit hash, optionally
// suffixed) is fine for `make build` but fails the release tag form check.
// Describe output is not categorically rejected — on an exact tag it is the
// tag itself, and `--long` output such as v1.4.0-2-gdeadbee is strict SemVer
// shaped, so both pass the gate by design.
func TestMakeCheckReleaseTagRejectsUntaggedDescribeOutput(t *testing.T) {
	res := runMake(t, baseEnv("VERSION=a58e897-dirty"), "check-release-tag")
	if res.exitCode == 0 {
		t.Fatalf("check-release-tag accepted describe output\nstdout: %s", res.stdout)
	}
}

// TestMakeBuildQuotesVersionInLinkerFlags proves the ldflags recipe hands the
// value to the shell as one quoted parameter instead of splicing it into the
// command line. It uses a dry run: expansion is what is under test, and the
// recipe Make prints is the expanded text it would have executed.
func TestMakeBuildQuotesVersionInLinkerFlags(t *testing.T) {
	t.Parallel()

	marker := sentinel(t, "ldflags-shell")
	tag := "v1.0.0\"; touch " + marker + "; \""
	binary := filepath.Join(filepath.Dir(marker), "agent-marketplace")

	res := runMake(t, baseEnv("VERSION="+tag), "--dry-run", "build", "BINARY="+binary)

	assertNotCreated(t, marker, res)
	if res.exitCode != 0 {
		t.Fatalf("make --dry-run build failed (exit %d)\nstderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, `-ldflags "-X main.version=$VERSION"`) {
		t.Fatalf("build recipe does not reference the exported variable\nstdout: %s", res.stdout)
	}
	if strings.Contains(res.stdout, marker) {
		t.Fatalf("build recipe expanded the hostile version into the command line\nstdout: %s", res.stdout)
	}
}

// TestMakeBuildStampsVersionVerbatim shows the value survives Make and the shell
// as inert text: this tag is a Make function and a shell command substitution,
// yet it is stamped into the binary unchanged.
func TestMakeBuildStampsVersionVerbatim(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the CLI; skipped in -short mode")
	}

	tag := "v1.0.0$(id)"
	binary := filepath.Join(t.TempDir(), "agent-marketplace")

	res := runMake(t, baseEnv("VERSION="+tag), "build", "BINARY="+binary)
	if res.exitCode != 0 {
		t.Fatalf("make build failed (exit %d)\nstdout: %s\nstderr: %s", res.exitCode, res.stdout, res.stderr)
	}

	version := run(t, filepath.Dir(binary), baseEnv(), binary, "--version")
	if version.exitCode != 0 {
		t.Fatalf("%s --version failed (exit %d)\nstderr: %s", binary, version.exitCode, version.stderr)
	}
	if got := strings.TrimSpace(version.stdout); !strings.Contains(got, tag) {
		t.Fatalf("stamped version %q does not contain the raw tag %q", got, tag)
	}
}

// TestMakefileNeverInterpolatesVersionIntoRecipes guards the structural rule
// behind the behavioural tests above: Make-level interpolation of $(VERSION)
// into a recipe cannot be made shell-safe, so recipes must reference the
// exported "$$VERSION" instead.
func TestMakefileNeverInterpolatesVersionIntoRecipes(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.Contains(line, "$(VERSION)") {
			t.Errorf("Makefile:%d interpolates $(VERSION) into a recipe: %s", i+1, line)
		}
	}
}

// TestMakefileGatesReleaseArtifactsOnTagGuard keeps every artifact-producing
// target behind the admission gate, including when a single platform target is
// invoked directly.
func TestMakefileGatesReleaseArtifactsOnTagGuard(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)

	for _, target := range []string{"release", "darwin-arm64", "darwin-amd64", "linux-amd64"} {
		prerequisites, ok := targetPrerequisites(makefile, target)
		if !ok {
			t.Fatalf("target %q not found in Makefile", target)
		}
		if !strings.Contains(prerequisites, "check-release-tag") {
			t.Errorf("target %q does not depend on check-release-tag (prerequisites: %q)", target, prerequisites)
		}
	}

	if !strings.Contains(makefile, validateScriptRelPath) {
		t.Errorf("Makefile does not invoke %s", validateScriptRelPath)
	}
}

func targetPrerequisites(makefile, target string) (string, bool) {
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) != target {
			continue
		}
		if strings.HasPrefix(rest, "=") { // target-specific variable assignment
			continue
		}
		return rest, true
	}
	return "", false
}
