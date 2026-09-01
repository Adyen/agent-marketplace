//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris && !aix

package tui

import "os"

// editorForwardedSignals on platforms without POSIX signal semantics
// forwards the portable interrupt only.
func editorForwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
