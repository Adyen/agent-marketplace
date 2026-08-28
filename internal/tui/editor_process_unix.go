//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || solaris || aix

package tui

import (
	"os"
	"syscall"
)

// editorForwardedSignals lists the external signals forwarded to the active
// editor: the same set Bubble Tea's own handler treats as hard exits.
func editorForwardedSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}
