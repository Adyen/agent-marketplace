package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// releasedClientArch maps the checksum of each pinned latest-release Linux
// asset to its architecture. Bump these, the CI download, and the version
// label after every stable release.
const releasedClientVersion = "v4.0.8"

var releasedClientArch = map[string]string{
	"02a34b1786ffbe2cf2d19f5163a9383ab5712c61db44636abc803771654665bb": "amd64",
	"51829f84d90da1035b983f24cf7f02a9c93c9d3aae32e2846ae4fcd10768370b": "arm64",
}

// A teammate on the candidate must not break teammates or CI that still run
// the latest release against the same committed repository. Each actor has
// its own HOME; only the repository is shared.
func TestReleasedClientReadsCandidateCommittedState(t *testing.T) {
	released := os.Getenv("AM_E2E_RELEASED_BINARY")
	image := os.Getenv("AM_E2E_RELEASED_IMAGE")
	if released == "" || image == "" {
		t.Skip("released executable and git-capable image are supplied by the dedicated released-client gate")
	}
	data, err := os.ReadFile(released)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	arch, ok := releasedClientArch[hex.EncodeToString(sum[:])]
	if !ok {
		t.Fatalf("%s is not a verified %s Linux release asset", released, releasedClientVersion)
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "candidate")
	build := exec.Command("go", "build", "-o", candidate, "./cmd/agent-marketplace")
	build.Dir = ".."
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Linux candidate: %v\n%s", err, output)
	}

	workspace := filepath.Join(root, "workspace")
	source := filepath.Join(root, "source")
	for _, dir := range []string{filepath.Join(workspace, ".agents"), filepath.Join(root, "tmp"), filepath.Join(root, "home-released"), filepath.Join(root, "home-candidate")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill := func(name, body string) {
		t.Helper()
		path := filepath.Join(source, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) {
		t.Helper()
		gitRun(t, source, "add", "-A")
		gitRun(t, source, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-q", "-m", message)
	}
	writeSkill("review", "Review body")
	writeSkill("second", "Second body")
	gitRun(t, "", "init", "-q", "-b", "main", source)
	commit("seed")
	registry := filepath.Join(root, "registry.git")
	gitRun(t, "", "clone", "-q", "--bare", source, registry)
	gitRun(t, source, "remote", "add", "origin", registry)
	gitRun(t, "", "init", "-q", "-b", "main", workspace)

	run := func(actor, executable string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		dockerArgs := []string{"run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges"}
		// Linux bind mounts keep host ownership, so git accepts the invoking
		// user. Docker Desktop reports root ownership inside the VM instead.
		if runtime.GOOS == "linux" {
			dockerArgs = append(dockerArgs, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()))
		}
		dockerArgs = append(dockerArgs,
			"--mount", "type=bind,src="+root+",dst="+root,
			"--mount", "type=bind,src="+executable+",dst=/bin/agent-marketplace,readonly",
			"--env", "HOME="+filepath.Join(root, "home-"+actor), "--env", "TMPDIR="+filepath.Join(root, "tmp"),
			"--env", "AGENT_MARKETPLACE_LOG_ENABLED=false", "--env", "AGENT_MARKETPLACE_NO_TUI=1", "--env", "GIT_TERMINAL_PROMPT=0",
			"--workdir", workspace, "--entrypoint", "/bin/agent-marketplace", image)
		cmd := exec.CommandContext(ctx, "docker", append(dockerArgs, args...)...)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
	must := func(actor, executable string, args ...string) {
		t.Helper()
		if output, err := run(actor, executable, args...); err != nil {
			t.Fatalf("%s %v: %v\n%s", actor, args, err, output)
		}
	}
	// Teammates and CI fetch the committed exact snapshot with --locked; the
	// check-only mode is deliberately offline and runs afterwards.
	releasedReads := func(after string) {
		t.Helper()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			if output, err := run("released", released, args...); err != nil {
				t.Fatalf("%s %v cannot use state committed after candidate %s: %v\n%s", releasedClientVersion, args, after, err, output)
			}
		}
	}
	lockPath := filepath.Join(workspace, ".agents", "lock.yaml")
	readLock := func() []byte {
		t.Helper()
		lock, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		return lock
	}

	must("released", released, "init", "--harnesses", "factory")
	must("candidate", candidate, "init", "--harnesses", "factory")
	repoConfig := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: file://%s\n    ref: main\n    skills:\n      include: [review]\n", registry)
	if err := os.WriteFile(filepath.Join(workspace, ".agents", "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	must("released", released, "sync")
	committed := readLock()

	must("candidate", candidate, "sync")
	if got := readLock(); !bytes.Equal(got, committed) {
		t.Fatalf("candidate plain sync rewrote the healthy committed lock:\nbefore:\n%s\nafter:\n%s", committed, got)
	}
	releasedReads("sync")

	must("candidate", candidate, "install", "second", "--scope", "repo")
	releasedReads("install")
	must("released", released, "sync")
	if _, err := os.Stat(filepath.Join(workspace, ".factory", "skills", "second", "SKILL.md")); err != nil {
		t.Fatalf("%s did not materialize the candidate's committed selection: %v", releasedClientVersion, err)
	}

	writeSkill("review", "Review body, revised")
	commit("revise review")
	gitRun(t, source, "push", "-q", "origin", "main")
	must("candidate", candidate, "update", "--scope", "repo")
	releasedReads("update")

	must("candidate", candidate, "remove", "second", "--scope", "repo")
	releasedReads("remove")
}
