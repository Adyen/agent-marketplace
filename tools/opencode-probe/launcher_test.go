package opencodeprobe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLauncherOfflineAndContainerOwnership(t *testing.T) {
	launcher, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"created", "collision", "cancel", "cancel creation", "cancel group", "cancel creation group"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "tools", "opencode-probe", "run.sh")
			writeFixture(t, script, string(launcher))
			bin := filepath.Join(root, "bin")
			writeFixture(t, filepath.Join(bin, "go"), `#!/bin/bash
if [[ $GOTOOLCHAIN != local || $GOPROXY != off || $GOSUMDB != off ]]; then
  echo 'Go execution was not forced offline' >&2
  exit 73
fi
case "$1" in
  test) exit 0 ;;
  tool) shift 5; exec "$@" ;;
  run) cat >/dev/null ;;
  *) exit 74 ;;
esac
`)
			writeFixture(t, filepath.Join(bin, "docker"), `#!/bin/bash
case "$1 $2" in
  'image inspect')
    if [[ $4 == '{{.Id}}' ]]; then echo sha256:fixture; else echo linux/arm64; fi ;;
  'create --rm')
    if [[ $PROBE_MODE == collision ]]; then
      echo collision > "$PROBE_COLLIDED"
      exit 41
    fi
    if [[ $PROBE_MODE == 'cancel creation'* ]]; then
      echo started > "$PROBE_STARTED"
      sleep 0.2
    fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == --cidfile ]]; then echo owned-container-id > "$2"; break; fi
      shift
    done ;;
  'start --attach')
    echo started > "$PROBE_START_INVOKED"
    if [[ $PROBE_MODE == cancel || $PROBE_MODE == 'cancel group' ]]; then
      echo started > "$PROBE_STARTED"
      sleep 10
    fi ;;
  'rm -f') echo "$3" >> "$PROBE_REMOVED" ;;
  *) exit 42 ;;
esac
`)
			for _, tool := range []string{"go", "docker"} {
				if err := os.Chmod(filepath.Join(bin, tool), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			native := filepath.Join(root, "native")
			writeFixture(t, native, "synthetic binary never executed")
			if err := os.Chmod(native, 0o700); err != nil {
				t.Fatal(err)
			}
			removed := filepath.Join(root, "removed")
			collided := filepath.Join(root, "collided")
			started := filepath.Join(root, "started")
			startInvoked := filepath.Join(root, "start-invoked")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/bash", script)
			cmd.Env = []string{
				"PATH=" + bin + ":/usr/bin:/bin", "TMPDIR=" + root,
				"OPENCODE_PROBE_BINARY=" + native, "PROBE_MODE=" + name,
				"PROBE_REMOVED=" + removed, "PROBE_COLLIDED=" + collided, "PROBE_STARTED=" + started,
				"PROBE_START_INVOKED=" + startInvoked,
			}
			if strings.HasPrefix(name, "cancel") {
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				waited := false
				defer func() {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // Bound failed-test descendants.
					if !waited {
						_ = cmd.Wait() // Reap on failed setup or signal paths too.
					}
				}()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for !fixtureExists(t, started) {
					select {
					case <-ctx.Done():
						t.Fatal("launcher did not reach the controlled running container")
					case <-ticker.C:
					}
				}
				began := time.Now()
				var signalErr error
				if strings.HasSuffix(name, "group") {
					signalErr = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				} else {
					signalErr = cmd.Process.Signal(syscall.SIGTERM)
				}
				if signalErr != nil {
					t.Fatal(signalErr)
				}
				err := cmd.Wait()
				waited = true
				exitErr, ok := errors.AsType[*exec.ExitError](err)
				if ctx.Err() != nil || time.Since(began) > 2*time.Second || !ok || exitErr.ExitCode() != 143 {
					t.Fatal("launcher did not promptly handle cancellation of its running container")
				}
				data, err := os.ReadFile(removed)
				if err != nil || string(data) != "owned-container-id\n" {
					t.Fatal("cancellation did not clean the owned container")
				}
				if strings.HasPrefix(name, "cancel creation") && fixtureExists(t, startInvoked) {
					t.Fatal("container execution started after creation cancellation")
				}
				return
			}
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("launcher exceeded its process bound")
			}
			if name == "collision" {
				exitErr, ok := errors.AsType[*exec.ExitError](err)
				if !ok || exitErr.ExitCode() != 41 || !fixtureExists(t, collided) {
					t.Fatal("launcher did not reach the intended container creation collision")
				}
				if fixtureExists(t, removed) {
					t.Fatal("launcher removed a container it did not create")
				}
			} else {
				if err != nil {
					t.Fatalf("launcher: %v: %s", err, strings.TrimSpace(string(output)))
				}
				data, err := os.ReadFile(removed)
				if err != nil || string(data) != "owned-container-id\n" {
					t.Fatal("cleanup must remove only the container ID created by this invocation")
				}
			}
		})
	}
}
