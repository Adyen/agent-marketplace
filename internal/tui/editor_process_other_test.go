//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris && !aix

package tui

// ignoreEditorHelperSignals is a no-op on platforms without POSIX signal
// semantics; the fake editor blocks until the parent kills it.
func ignoreEditorHelperSignals() {}

// editorHelperMarkerContent carries no process-group identity on platforms
// without POSIX process groups.
func editorHelperMarkerContent() []byte { return []byte("ready") }
