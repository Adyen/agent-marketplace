//go:build unix

package cache

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func processSessionID(pid int) (int, error) {
	return unix.Getsid(pid)
}

// spec: git-authentication/unverified-ssh-host-key-is-explained
func TestRemoteDefaultBranchClassifiesTransportFailures(t *testing.T) {
	t.Run("SSH host key", func(t *testing.T) {
		isolatedGitAuth(t)
		ssh := authFile(t, "fake-ssh", "#!/bin/sh\nprintf 'Host key verification failed.\\n' >&2\nexit 255\n", 0o700)
		t.Setenv("GIT_SSH", ssh)
		_, _, err := RemoteDefaultBranch("ssh://git@example.invalid/owner/repo.git")
		if !isGitTransportFailure(err) || !strings.Contains(err.Error(), "the SSH host key for example.invalid is not verified") || !strings.Contains(err.Error(), "~/.ssh/known_hosts") {
			t.Fatalf("RemoteDefaultBranch() error = %v, want host-key diagnostic", err)
		}
		if strings.Contains(err.Error(), "Host key verification failed.") {
			t.Fatalf("RemoteDefaultBranch() exposed Git stderr: %v", err)
		}
	})
	t.Run("HTTPS authentication", func(t *testing.T) {
		isolatedGitAuth(t)
		installGitFailureWrapper(t, "fatal: Authentication failed for https://example.invalid/owner/repo.git")
		_, _, err := RemoteDefaultBranch("https://example.invalid/owner/repo.git")
		if !isGitTransportFailure(err) || !strings.Contains(err.Error(), "repository not found or requires credentials") || strings.Contains(err.Error(), "Authentication failed") {
			t.Fatalf("RemoteDefaultBranch() error = %v, want sanitized repository/credentials diagnostic", err)
		}
	})
	t.Run("unmatched stderr keeps first line", func(t *testing.T) {
		isolatedGitAuth(t)
		installGitFailureWrapper(t, "fatal: synthetic first line\nsynthetic second line")
		_, _, err := RemoteDefaultBranch("ssh://git@example.invalid/owner/repo.git")
		if err == nil || !strings.Contains(err.Error(), "git ls-remote --symref --") || !strings.Contains(err.Error(), ": exit status 128: fatal: synthetic first line") || strings.Contains(err.Error(), "synthetic second line") {
			t.Fatalf("RemoteDefaultBranch() error = %v, want existing ls-remote first-line format", err)
		}
	})
}

// spec: git-authentication/cli-acquisition-does-not-prompt
// spec: git-authentication/unknown-host-keys-are-not-trusted-automatically
func TestGitAcquisitionDoesNotPromptOnTTY(t *testing.T) {
	isolatedGitAuth(t)
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")

	home := os.Getenv("HOME")
	knownHosts := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.Mkdir(filepath.Dir(knownHosts), 0o700); err != nil {
		t.Fatalf("create SSH directory: %v", err)
	}
	knownHostsBefore := []byte("existing known_hosts fixture\n")
	if err := os.WriteFile(knownHosts, knownHostsBefore, 0o600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}
	parentSession, err := processSessionID(os.Getpid())
	if err != nil {
		t.Fatalf("read parent session ID: %v", err)
	}
	record := filepath.Join(t.TempDir(), "ssh-record")
	release := filepath.Join(t.TempDir(), "release-ssh")
	script := authFile(t, "fake-ssh", fmt.Sprintf(`#!/bin/sh
{
	printf 'pid=%%s\n' "$$"
} > %q
while [ ! -e %q ]; do sleep 0.01; done
tty_open=no
if : </dev/tty 2>/dev/null; then tty_open=yes; fi
{
	printf 'askpass=%%s\n' "${SSH_ASKPASS_REQUIRE-}"
	printf 'tty=%%s\n' "$tty_open"
} >> %q
printf 'Host key verification failed.\n' >&2
exit 255
`, record, release, record), 0o700)
	t.Setenv("GIT_SSH", script)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type gitResult struct {
		output []byte
		err    error
	}
	done := make(chan gitResult, 1)
	go func() {
		output, err := execGitContext(ctx, "", []string{
			"clone", "--", "ssh://git@example.invalid/owner/repo.git", filepath.Join(t.TempDir(), "clone"),
		})
		done <- gitResult{output: output, err: err}
	}()
	finished := false
	defer func() {
		_ = os.WriteFile(release, nil, 0o600)
		if !finished {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				cancel()
				<-done
			}
		}
	}()

	var recorded []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorded, err = os.ReadFile(record)
		if err == nil && strings.HasPrefix(string(recorded), "pid=") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || !strings.HasPrefix(string(recorded), "pid=") {
		t.Fatalf("fake SSH did not record its PID: data=%q err=%v", recorded, err)
	}
	firstLine, _, _ := strings.Cut(strings.TrimSpace(string(recorded)), "\n")
	key, value, valid := strings.Cut(firstLine, "=")
	if !valid || key != "pid" {
		t.Fatalf("invalid fake SSH record %q", recorded)
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("invalid fake SSH PID %q: %v", value, err)
	}
	childSession, err := processSessionID(pid)
	if err != nil {
		t.Fatalf("read fake SSH session ID for PID %d: %v", pid, err)
	}
	if childSession == parentSession {
		t.Fatalf("Git SSH session ID = %d, parent session ID = %d; want a separate session", childSession, parentSession)
	}

	_ = os.WriteFile(release, nil, 0o600)
	result := <-done
	finished = true
	recorded, err = os.ReadFile(record)
	if err != nil {
		t.Fatalf("read fake SSH record: %v", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n")[1:] {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid fake SSH record line %q", line)
		}
		values[key] = value
	}
	if values["askpass"] != "never" {
		t.Fatalf("SSH_ASKPASS_REQUIRE = %q, want never", values["askpass"])
	}
	if values["tty"] != "no" {
		t.Fatalf("fake SSH opened /dev/tty: record %q", recorded)
	}
	const want = "git.auth-transport-failed: the SSH host key for example.invalid is not verified. Verify the host key (for example with ssh -T git@example.invalid) and add it to ~/.ssh/known_hosts"
	if !isGitTransportFailure(result.err) || result.err.Error() != want {
		t.Fatalf("execGitContext() error = %v, want %q", result.err, want)
	}
	if strings.Contains(string(result.output), "Host key verification failed.") || strings.Contains(result.err.Error(), "Host key verification failed.") {
		t.Fatalf("transport stderr was exposed: output=%q error=%q", result.output, result.err)
	}
	knownHostsAfter, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("read known_hosts after acquisition: %v", err)
	}
	if !bytes.Equal(knownHostsAfter, knownHostsBefore) {
		t.Fatalf("known_hosts after acquisition = %q, want unchanged bytes %q", knownHostsAfter, knownHostsBefore)
	}
}
