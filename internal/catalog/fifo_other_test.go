//go:build !unix

package catalog

import "testing"

func mkfifo(t *testing.T, _ string) {
	t.Helper()
	t.Skip("FIFOs are not reachable through a filesystem path on this platform")
}
