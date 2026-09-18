//go:build unix

package analyze

import (
	"os/exec"
	"syscall"
)

// bindRunnerProcessGroup places the runner command in its own process group
// and replaces the context-cancel kill with a group kill. Descendants that
// inherited the runner's stdout/stderr pipes die with it, so the pipes reach
// EOF and Wait returns within the configured timeout.
func bindRunnerProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
