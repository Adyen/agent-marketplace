package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hookInvariantActor is an independent teammate whose HOME, non-default XDG
// roots, absolute CODEX_HOME and checkout all sit under paths with spaces and
// single and double quotes.
type hookInvariantActor struct {
	home, state, config, cache, codexHome, checkout string
}

func newHookInvariantActor(t *testing.T, e *Env) hookInvariantActor {
	t.Helper()
	base := filepath.Join(filepath.Dir(e.home), `teammate's "relocated" root`)
	a := hookInvariantActor{
		home:      filepath.Join(base, `home with 'single' "double" quotes`),
		state:     filepath.Join(base, "xdg state"),
		config:    filepath.Join(base, "xdg config"),
		cache:     filepath.Join(base, "xdg cache"),
		codexHome: filepath.Join(base, `codex "home"`),
		checkout:  filepath.Join(base, `team checkout 'q' "x"`),
	}
	for _, dir := range []string{a.home, a.state, a.config, a.cache, a.codexHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func (a hookInvariantActor) run(e *Env, args ...string) ExecResult {
	e.t.Helper()
	env := subprocessEnvWithOverrides(e.subprocessEnv(), map[string]string{
		"HOME":            a.home,
		"XDG_STATE_HOME":  a.state,
		"XDG_CONFIG_HOME": a.config,
		"XDG_CACHE_HOME":  a.cache,
		"CODEX_HOME":      a.codexHome,
	})
	ctx, cancel := context.WithTimeout(e.t.Context(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, e.binary, args...)
	c.Dir, c.Env = a.checkout, env
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	result := ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), Combined: stdout.String() + stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		e.t.Fatalf("exec agent-marketplace %v as relocated teammate: %v", args, err)
	}
	return result
}

// execute runs a parsed handler the way the harness does: through a shell
// from the checkout (or the handler's Windsurf working directory), with every
// documented project-directory variable naming this actor's checkout.
func (a hookInvariantActor) execute(t *testing.T, handler hookInvariantHandler) (string, error) {
	t.Helper()
	cwd := a.checkout
	if handler.dir != "" {
		cwd = handler.dir
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(a.checkout, cwd)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "/bin/sh", "-c", handler.command)
	c.Dir = cwd
	c.Env = []string{"HOME=" + a.home, "PATH=/usr/bin:/bin", "CODEX_HOME=" + a.codexHome}
	for _, name := range []string{"CLAUDE_PROJECT_DIR", "FACTORY_PROJECT_DIR", "CURSOR_PROJECT_DIR", "GEMINI_PROJECT_DIR", "DEVIN_PROJECT_DIR", "AGENT_MARKETPLACE_PROJECT_DIR"} {
		c.Env = append(c.Env, name+"="+a.checkout)
	}
	output, err := c.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func hookInvariantWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// A hook whose command runs a support script must, after a teammate syncs
// from a relocated checkout with their own HOME, XDG roots and CODEX_HOME,
// all under paths with spaces and quotes, leave exactly one loaded handler
// that runs an executable copy of the script inside that teammate's own
// authorized scope: the checkout for repository scope, the harness root
// under HOME (or CODEX_HOME) for machine scope. It must never run a copy in
// another checkout, another HOME, or a cache.
// spec: hook-resources/repository-local-support-paths-survive-teammate-checkout
func TestHookInvariantSupportRunsInsideTheActorsOwnScope(t *testing.T) {
	for _, target := range hookInvariantNativeTargets {
		for _, scope := range []string{"repo", "user"} {
			if target == "opencode" && scope == "repo" {
				// A teammate cannot yet sync committed OpenCode output (#813);
				// see TestHookInvariantOpenCodeTeammateSyncKeepsCommittedHooks.
				continue
			}
			t.Run(target+"/"+scope, func(t *testing.T) {
				t.Parallel()
				e := newEnv(t)
				e.WriteFile("/workspace/vendor/reg/hooks/scripts/sentinel.sh", "#!/bin/sh\ncd \"$(dirname \"$0\")\" && pwd -P\n")
				if err := os.Chmod(filepath.Join(e.workspace, "vendor", "reg", "hooks", "scripts", "sentinel.sh"), 0o755); err != nil {
					t.Fatal(err)
				}
				e.WriteFile("/workspace/vendor/reg/hooks/hooks.json", hookInvariantDocument(hookInvariantGroup("Bash", `"${CLAUDE_PLUGIN_ROOT}/scripts/sentinel.sh"`)))
				selected := hookInvariantV4Config(target, "./vendor/reg", "hook/PreToolUse-command")
				base := "apiVersion: agent-marketplace/v4\nharnesses: [" + target + "]\n"
				if scope == "repo" {
					e.WriteRepoConfig(selected)
				} else {
					e.WriteRepoConfig(base)
				}
				e.InitGitRepo()
				if scope == "repo" {
					e.MustRunAM("sync")
				}
				hookInvariantCommitAll(e, "first teammate commits the registry and its output")

				a := newHookInvariantActor(t, e)
				e.MustRun("git clone -q --no-hardlinks " + shellQuote(e.workspace) + " " + shellQuote(a.checkout))
				root, machine := a.checkout, scope == "user"
				if machine {
					root = a.home
					if err := os.MkdirAll(filepath.Join(a.home, ".agents"), 0o755); err != nil {
						t.Fatal(err)
					}
					user := strings.Replace(selected, "'./vendor/reg'", shellQuoteYAML(filepath.Join(a.checkout, "vendor", "reg")), 1)
					if err := os.WriteFile(filepath.Join(a.home, ".agents", "config.yaml"), []byte(user), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				r := a.run(e, "sync")
				if r.ExitCode != 0 {
					t.Fatalf("relocated teammate's sync exited %d:\n%s", r.ExitCode, r.Combined)
				}
				handlers := hookInvariantLoadedHandlers(t, target, root, machine)
				// Codex machine output follows CODEX_HOME and OpenCode's follows
				// XDG_CONFIG_HOME; neither may fall back to HOME.
				for _, relocated := range []struct{ target, name, dir string }{{"codex", ".codex", a.codexHome}, {"opencode", ".config", a.config}} {
					if target != relocated.target || !machine {
						continue
					}
					if _, err := os.Lstat(filepath.Join(a.home, relocated.name)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("%s machine output ignored its relocated root: %v", target, err)
					}
					handlers = relocatedHandlers(t, target, relocated.name, relocated.dir)
				}
				if len(handlers) != 1 {
					t.Fatalf("loaded handlers = %+v, want exactly one", handlers)
				}
				output, err := a.execute(t, handlers[0])
				if err != nil {
					t.Fatalf("handler %q failed: %v\n%s", handlers[0].command, err, output)
				}
				allowed := a.checkout
				switch {
				case machine && target == "codex":
					allowed = a.codexHome
				case machine && target == "opencode":
					allowed = a.config
				case machine:
					allowed = a.home
				}
				allowed, err = filepath.EvalSymlinks(allowed)
				if err != nil {
					t.Fatal(err)
				}
				if !hookInvariantWithin(allowed, output) {
					t.Fatalf("handler %q ran support from %q, outside this teammate's scope %q", handlers[0].command, output, allowed)
				}
				for _, foreign := range []string{e.workspace, e.home, a.cache, filepath.Join(a.home, ".cache")} {
					if resolved, err := filepath.EvalSymlinks(foreign); err == nil && hookInvariantWithin(resolved, output) {
						t.Fatalf("handler %q ran support from %q inside %q", handlers[0].command, output, foreign)
					}
				}
				info, err := os.Stat(filepath.Join(output, "sentinel.sh"))
				if err != nil || info.Mode().Perm()&0o100 == 0 {
					t.Fatalf("support script at %s is not executable: %v %v", output, info, err)
				}
				if r := a.run(e, "sync", "--check"); r.ExitCode != 0 {
					t.Fatalf("relocated teammate's check exited %d:\n%s", r.ExitCode, r.Combined)
				}
			})
		}
	}
}

// relocatedHandlers reads a target's machine hooks from a relocated root,
// through a scope root whose name entry links to it.
func relocatedHandlers(t *testing.T, target, name, dir string) []hookInvariantHandler {
	t.Helper()
	root := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	return hookInvariantLoadedHandlers(t, target, root, true)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellQuoteYAML(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// A teammate's sync of a healthy committed OpenCode hook tree must succeed
// and leave the tracked adapter, declaration and support bytes unchanged.
func TestHookInvariantOpenCodeTeammateSyncKeepsCommittedHooks(t *testing.T) {
	t.Skip("known defect: https://github.com/Adyen/agent-marketplace/issues/813")
	e := newEnv(t)
	e.WriteFile("/workspace/vendor/reg/hooks/scripts/sentinel.sh", "#!/bin/sh\necho sentinel\n")
	if err := os.Chmod(filepath.Join(e.workspace, "vendor", "reg", "hooks", "scripts", "sentinel.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.WriteFile("/workspace/vendor/reg/hooks/hooks.json", hookInvariantDocument(hookInvariantGroup("Bash", `"${CLAUDE_PLUGIN_ROOT}/scripts/sentinel.sh"`)))
	e.WriteRepoConfig(hookInvariantV4Config("opencode", "./vendor/reg", "hook/PreToolUse-command"))
	e.InitGitRepo()
	e.MustRunAM("sync")
	hookInvariantCommitAll(e, "first teammate commits OpenCode hooks")
	home := filepath.Join(filepath.Dir(e.home), "teammate-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(filepath.Dir(e.workspace), "teammate-checkout")
	e.MustRun("git clone -q --no-hardlinks " + shellQuote(e.workspace) + " " + shellQuote(checkout))
	e.AssertExitCode(hookInvariantRunAs(e, home, checkout, "sync"), 0)
	if got := hookInvariantLoadedCount(t, "opencode", checkout, false, "scripts/sentinel.sh"); got != 1 {
		t.Errorf("teammate's sync left %d loaded handlers, want 1", got)
	}
	if status := e.Exec("git", "-C", checkout, "status", "--porcelain"); strings.TrimSpace(status.Stdout) != "" || status.ExitCode != 0 {
		t.Errorf("teammate's sync of a healthy committed tree changed tracked files:\n%s", status.Combined)
	}
}
