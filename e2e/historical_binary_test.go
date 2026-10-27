package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// historicalWorld runs pinned release executables and the candidate in
// offline, read-only containers that share only root with the host. Released
// binaries always read the fixed /etc organization path, so they must never
// run directly on a developer machine.
type historicalWorld struct {
	t     *testing.T
	root  string
	image string
}

// verifiedExecutable returns the absolute path and architecture of a pinned
// release asset named by env, failing unless its checksum is listed.
func verifiedExecutable(t *testing.T, env string, archByDigest map[string]string) (string, string) {
	t.Helper()
	// Docker bind mounts require an absolute source.
	path, err := filepath.Abs(os.Getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	arch, ok := archByDigest[hex.EncodeToString(sum[:])]
	if !ok {
		t.Fatalf("%s=%s is not a verified pinned release asset", env, path)
	}
	return path, arch
}

func newHistoricalWorld(t *testing.T, image string) *historicalWorld {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("historical-binary gate requires Docker: %v", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &historicalWorld{t: t, root: root, image: image}
}

func (w *historicalWorld) path(elem ...string) string {
	return filepath.Join(append([]string{w.root}, elem...)...)
}

func (w *historicalWorld) buildCandidate(arch string, tags ...string) string {
	w.t.Helper()
	candidate := w.path("candidate")
	args := []string{"build", "-o", candidate}
	if len(tags) > 0 {
		args = append(args, "-tags", tags[0])
	}
	build := exec.CommandContext(w.t.Context(), "go", append(args, "./cmd/agent-marketplace")...)
	build.Dir = ".."
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	if output, err := build.CombinedOutput(); err != nil {
		w.t.Fatalf("build Linux candidate: %v\n%s", err, output)
	}
	return candidate
}

// run executes executable in workdir with HOME set to home.
func (w *historicalWorld) run(home, workdir, executable string, args ...string) (string, error) {
	return w.runWithEnv(nil, home, workdir, executable, args...)
}

func (w *historicalWorld) runWithEnv(extraEnv []string, home, workdir, executable string, args ...string) (string, error) {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(w.t.Context(), 2*time.Minute)
	defer cancel()
	dockerArgs := []string{"run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges"}
	// Linux bind mounts keep host ownership, so git accepts the invoking
	// user. Docker Desktop reports root ownership inside the VM instead.
	if runtime.GOOS == "linux" {
		dockerArgs = append(dockerArgs, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()))
	}
	dockerArgs = append(dockerArgs,
		"--mount", "type=bind,src="+w.root+",dst="+w.root,
		"--mount", "type=bind,src="+executable+",dst=/bin/agent-marketplace,readonly",
		"--env", "HOME="+home, "--env", "TMPDIR="+w.path("tmp"),
		"--env", "AGENT_MARKETPLACE_LOG_ENABLED=false", "--env", "AGENT_MARKETPLACE_NO_TUI=1", "--env", "GIT_TERMINAL_PROMPT=0",
	)
	for _, value := range extraEnv {
		dockerArgs = append(dockerArgs, "--env", value)
	}
	dockerArgs = append(dockerArgs, "--workdir", workdir, "--entrypoint", "/bin/agent-marketplace", w.image)
	output, err := exec.CommandContext(ctx, "docker", append(dockerArgs, args...)...).CombinedOutput()
	return string(output), err
}

func (w *historicalWorld) must(home, workdir, executable string, args ...string) string {
	return w.mustWithEnv(nil, home, workdir, executable, args...)
}

func (w *historicalWorld) mustWithEnv(extraEnv []string, home, workdir, executable string, args ...string) string {
	w.t.Helper()
	output, err := w.runWithEnv(extraEnv, home, workdir, executable, args...)
	if err != nil {
		w.t.Fatalf("%s %v: %v\n%s", filepath.Base(executable), args, err, output)
	}
	return output
}

// gitRegistry is a working clone pushing to a bare registry under root.
type gitRegistry struct {
	t      *testing.T
	source string
	URL    string
}

func (w *historicalWorld) gitRegistry(skills map[string]string) *gitRegistry {
	w.t.Helper()
	r := &gitRegistry{t: w.t, source: w.path("source"), URL: "file://" + w.path("registry.git")}
	for name, body := range skills {
		r.writeSkill(name, body)
	}
	gitRun(w.t, "", "init", "-q", "-b", "main", r.source)
	r.commit("seed")
	gitRun(w.t, "", "clone", "-q", "--bare", r.source, w.path("registry.git"))
	gitRun(w.t, r.source, "remote", "add", "origin", w.path("registry.git"))
	return r
}

func (r *gitRegistry) writeSkill(name, body string) {
	r.t.Helper()
	path := filepath.Join(r.source, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(skillDocument(name, body)), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func skillDocument(name, body string) string {
	return "---\nname: " + name + "\ndescription: " + name + "\n---\n\n" + body + "\n"
}

func (r *gitRegistry) commit(message string) {
	r.t.Helper()
	gitRun(r.t, r.source, "add", "-A")
	gitRun(r.t, r.source, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", message)
}

func (r *gitRegistry) publish(name, body string) {
	r.t.Helper()
	r.writeSkill(name, body)
	r.commit("update " + name)
	gitRun(r.t, r.source, "push", "-q", "origin", "main")
}
