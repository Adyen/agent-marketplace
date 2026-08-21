//go:build !unix

package registryfs

import "testing"

func mkfifo(t *testing.T, _ string) {
	t.Helper()
	t.Skip("FIFOs are not reachable through a filesystem path on this platform")
}
