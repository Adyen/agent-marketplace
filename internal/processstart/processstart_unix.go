//go:build unix

package processstart

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// detachedWaitDelay bounds how long Wait blocks on inherited output pipes after
// cancellation when a descendant escapes the process group.
const detachedWaitDelay = 5 * time.Second

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = detachedWaitDelay
}
