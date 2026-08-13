package devinprobe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/devincli"
)

// EnableVar opts the suite in. Without it every probe skips, so `go test ./...`
// and CI never depend on a third-party binary.
const EnableVar = "AGENT_MARKETPLACE_DEVIN_PROBES"

// RequireVar turns a skip into a failure.
//
// This exists because of acceptance criterion 15 of the plan: "Current-version
// probes pass for project/user skill-directory and flat-subagent symlinks;
// failure blocks release." A suite that skips because no Devin binary is
// installed has not passed that criterion — it has said nothing about it — and
// on a release machine the difference must be a red build, not a green one with
// SKIP lines nobody reads.
//
//	make devin-probe                        # developer run; skips when absent
//	DEVIN_PROBE_REQUIRE=1 make devin-probe  # release gate; absence fails
const RequireVar = "DEVIN_PROBE_REQUIRE"

// skipOrFail is the single place a probe declines to run. With RequireVar set
// it fails instead, so a release gate cannot be satisfied by a skip.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv(RequireVar) == "1" {
		t.Fatalf("RELEASE GATE UNSATISFIED (%s=1): "+format, append([]any{RequireVar}, args...)...)
	}
	t.Skipf(format, args...)
}

// commandTimeout bounds one probe invocation. Every command in this suite is a
// local, non-networked, read-only command; anything slower is a hung binary and
// must fail the probe rather than hang the suite.
const commandTimeout = 30 * time.Second

// inheritedEnv is the allowlist of variables a probe inherits from the
// developer's shell. Everything else — in particular every DEVIN_* variable and
// every XDG_* variable — is dropped so ambient configuration cannot change what
// a probe observes.
var inheritedEnv = []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "SHELL", "TERM"}

// requireDevin skips unless the suite is enabled and a Devin CLI at or above the
// compatibility floor is installed. The skip reason always says exactly why, as
// the evidence snapshot requires: "The suite skips with a clear reason when
// Devin is absent or older than the floor."
func requireDevin(t *testing.T) devincli.Probe {
	t.Helper()
	if os.Getenv(EnableVar) != "1" {
		// Deliberately NOT skipOrFail: not opting in is a statement about how
		// the suite was invoked, not about the machine. `make devin-probe`
		// always sets it.
		t.Skipf("devin runtime probes are local-only and opt-in; run `make devin-probe` (set %s=1) to enable them", EnableVar)
	}
	if runtime.GOOS == "windows" {
		t.Skip("the canonical user root ~/.config/devin is macOS/Linux only; Windows %APPDATA% roots are an explicit plan non-goal")
	}
	probe := devincli.Detect(context.Background())
	switch probe.Status {
	case devincli.StatusNotInstalled:
		skipOrFail(t, "no %q binary on PATH; install Devin %s+ to run the runtime probes", devincli.Binary, devincli.FloorString)
	case devincli.StatusUnknown:
		skipOrFail(t, "a %q binary exists at %s but its version could not be established (%v); the %s floor cannot be confirmed",
			devincli.Binary, probe.Path, probe.Err, devincli.FloorString)
	case devincli.StatusUnsupported:
		skipOrFail(t, "installed Devin %s is below the %s compatibility floor; these probes describe %s+ only",
			probe.Version, devincli.FloorString, devincli.FloorString)
	}
	return probe
}

// sandbox is one hermetic Devin environment: a private HOME, a private XDG
// tree, and a throwaway git repository to act as the project root.
type sandbox struct {
	t *testing.T
	// Root is the temp directory that contains everything.
	Root string
	// Home is the sandbox HOME. The canonical user root is Home/.config/devin.
	Home string
	// UserRoot is Home/.config/devin, the canonical user extensibility root.
	UserRoot string
	// Repo is the throwaway git repository Devin resolves as the project root.
	Repo string
	// ProjectRoot is Repo/.devin, the canonical project extensibility root.
	ProjectRoot string
	// binary is the resolved devin executable.
	binary string
	// version is the detected version of binary, used in probe failure messages.
	version devincli.Version
}

// newSandbox builds an isolated sandbox and verifies the isolation invariants
// before any command can run.
func newSandbox(t *testing.T, probe devincli.Probe) *sandbox {
	t.Helper()
	root := t.TempDir()
	s := &sandbox{
		t:           t,
		Root:        root,
		Home:        filepath.Join(root, "home"),
		UserRoot:    filepath.Join(root, "home", ".config", "devin"),
		Repo:        filepath.Join(root, "repo"),
		ProjectRoot: filepath.Join(root, "repo", ".devin"),
		binary:      probe.Path,
		version:     probe.Version,
	}
	for _, dir := range []string{s.UserRoot, s.ProjectRoot, filepath.Join(root, "empty-git-template")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create sandbox dir %s: %v", dir, err)
		}
	}
	// Devin resolves the project root by walking up for .git or .jj, so the
	// sandbox repo must actually be a repository or Devin would walk out of the
	// sandbox and find the developer's real checkout.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	gitInit := exec.CommandContext(ctx, "git", "init", "-q", s.Repo)
	// Same allowlist discipline as sandbox.env: an ambient GIT_DIR, GIT_WORK_TREE,
	// GIT_INDEX_FILE or GIT_TEMPLATE_DIR must not reach this exec.
	gitInit.Env = append(s.env(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TEMPLATE_DIR="+filepath.Join(root, "empty-git-template"),
	)
	gitInit.Stdin = nil
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init sandbox repo: %v\n%s", err, out)
	}
	s.assertIsolated()
	return s
}

// assertIsolated fails the test unless the sandbox cannot reach real user
// configuration. It runs before every exec, not once at construction, so a
// probe that rewrites the sandbox paths still cannot escape.
func (s *sandbox) assertIsolated() {
	s.t.Helper()
	realHome, err := os.UserHomeDir()
	if err != nil {
		s.t.Fatalf("resolve real home to prove isolation: %v", err)
	}
	realDevin := filepath.Join(realHome, ".config", "devin")
	if s.Home == realHome {
		s.t.Fatalf("REFUSING TO PROBE: sandbox HOME %s is the real user home", s.Home)
	}
	if strings.HasPrefix(realDevin+string(filepath.Separator), s.Root+string(filepath.Separator)) {
		s.t.Fatalf("REFUSING TO PROBE: the real Devin config root %s is inside the sandbox %s", realDevin, s.Root)
	}
	if !strings.HasPrefix(s.UserRoot+string(filepath.Separator), s.Root+string(filepath.Separator)) {
		s.t.Fatalf("REFUSING TO PROBE: sandbox user root %s escaped the sandbox %s", s.UserRoot, s.Root)
	}
	if _, err := os.Stat(filepath.Join(s.Repo, ".git")); err != nil {
		s.t.Fatalf("REFUSING TO PROBE: sandbox repo %s is not a git repository: %v", s.Repo, err)
	}
}

// env builds the allowlisted environment for one invocation.
func (s *sandbox) env() []string {
	env := []string{
		"HOME=" + s.Home,
		"XDG_CONFIG_HOME=" + filepath.Join(s.Home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(s.Home, ".local", "state"),
		"XDG_DATA_HOME=" + filepath.Join(s.Home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(s.Home, ".cache"),
		// Devin is a TTY app; force plain output so probe assertions are not
		// matching against ANSI escape sequences.
		"NO_COLOR=1",
		"CLICOLOR=0",
	}
	for _, key := range inheritedEnv {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// run executes one read-only devin command inside the sandbox repository and
// returns its combined output. A non-zero exit is returned as output plus an
// error; callers decide whether that is a failure.
func (s *sandbox) run(args ...string) (string, error) {
	s.t.Helper()
	s.assertIsolated()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary, args...)
	cmd.Dir = s.Repo
	cmd.Env = s.env()
	// A probe must never block on input from the developer's terminal.
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	s.t.Logf("devin %s\n%s", strings.Join(args, " "), out)
	return string(out), err
}

// mustRun executes a command that is expected to succeed.
func (s *sandbox) mustRun(args ...string) string {
	s.t.Helper()
	out, err := s.run(args...)
	if err != nil {
		s.t.Fatalf("devin %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// writeSkill writes a canonical Devin skill at a root.
func (s *sandbox) writeSkill(root, name, frontmatter, body string) string {
	s.t.Helper()
	dir := filepath.Join(root, "skills", name)
	if !strings.HasPrefix(dir+string(filepath.Separator), s.Root+string(filepath.Separator)) {
		s.t.Fatalf("REFUSING TO WRITE: skill dir %s is outside the sandbox %s", dir, s.Root)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatalf("create skill dir: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: " + name + "\n" + frontmatter + "---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatalf("write skill: %v", err)
	}
	return dir
}

// writeFile writes an arbitrary file inside the sandbox.
func (s *sandbox) writeFile(path, content string) {
	s.t.Helper()
	if !strings.HasPrefix(path, s.Root+string(filepath.Separator)) {
		s.t.Fatalf("REFUSING TO WRITE: %s is outside the sandbox %s", path, s.Root)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatalf("write %s: %v", path, err)
	}
}

// symlink creates a symlink inside the sandbox.
func (s *sandbox) symlink(target, link string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		s.t.Fatalf("create dir for symlink %s: %v", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		s.t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

// releaseBlocker fails a probe the plan declares a release gate.
//
//	"Both shapes are release requirements: 3000.3 probes must pass for project
//	and user scopes before the target ships. There is no real-file fallback in
//	this increment."
//
// The RELEASE BLOCKER prefix exists so this failure is never triaged as an
// ordinary flaky assertion.
func releaseBlocker(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf("RELEASE BLOCKER (symlink probe): "+format, args...)
}
