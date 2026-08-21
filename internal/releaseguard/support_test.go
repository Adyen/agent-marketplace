package releaseguard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// validateScriptRelPath is the shared admission gate under test. The workflow
// and the Makefile must both invoke exactly this script.
const validateScriptRelPath = "scripts/validate-release-tag.sh"

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}
		dir = parent
	}
}

type cmdResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// baseEnv strips variables that would otherwise leak from the invoking `make
// test` run: an inherited VERSION would defeat tests that assert the default,
// and inherited MAKELEVEL/MAKEFLAGS make a nested make print directory banners
// and inherit flags such as -j.
func baseEnv(extra ...string) []string {
	stripped := map[string]bool{
		"VERSION":      true,
		"MAKELEVEL":    true,
		"MAKEFLAGS":    true,
		"MFLAGS":       true,
		"MAKE_TERMOUT": true,
		"MAKE_TERMERR": true,
	}

	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if stripped[key] {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func run(t *testing.T, dir string, env []string, name string, args ...string) cmdResult {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := cmdResult{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s %v: %v", name, args, err)
		}
		res.exitCode = exitErr.ExitCode()
	}
	return res
}

// runValidateScript invokes the admission gate with a single argument.
func runValidateScript(t *testing.T, tag string) cmdResult {
	t.Helper()

	return runValidateScriptEnv(t, nil, tag)
}

// runValidateScriptEnv invokes the admission gate with extra environment
// entries (e.g. a requested locale) layered over the sanitized base set. For
// duplicate keys os/exec keeps the last value, so extras override inherited
// variables.
func runValidateScriptEnv(t *testing.T, extraEnv []string, tag string) cmdResult {
	t.Helper()

	root := repoRoot(t)
	return run(t, root, baseEnv(extraEnv...), filepath.Join(root, validateScriptRelPath), tag)
}

// makeBin resolves make, skipping the test when it is unavailable rather than
// failing on machines without it.
func makeBin(t *testing.T) string {
	t.Helper()

	bin, err := exec.LookPath("make")
	if err != nil {
		t.Skipf("make not available: %v", err)
	}
	return bin
}

// runMake invokes make in the repository root with a sanitized environment.
func runMake(t *testing.T, env []string, args ...string) cmdResult {
	t.Helper()

	root := repoRoot(t)
	full := append([]string{"--no-print-directory"}, args...)
	return run(t, root, env, makeBin(t), full...)
}

// sentinel returns a path inside a per-test temporary directory that no command
// under test may create. Its existence proves that injected syntax executed.
func sentinel(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func assertNotCreated(t *testing.T, path string, res cmdResult) {
	t.Helper()

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("injected command executed: %q was created\nstdout: %s\nstderr: %s", path, res.stdout, res.stderr)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %q: %v", path, err)
	}
}
