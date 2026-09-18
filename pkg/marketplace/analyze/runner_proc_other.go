//go:build !unix

package analyze

import "os/exec"

// bindRunnerProcessGroup is a no-op on platforms without Unix process groups;
// the analyzer runner contract targets macOS and Linux.
func bindRunnerProcessGroup(*exec.Cmd) {}
