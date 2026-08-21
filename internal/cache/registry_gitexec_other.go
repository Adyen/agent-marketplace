//go:build !unix

package cache

import (
	"os"
	"os/exec"
)

// The non-unix half of anchored git execution.
//
// Without fchdir(2) a child's working directory can only be expressed as a path,
// so containment here is only as strong as the checks that preceded it. PLATFORMS
// in the Makefile is darwin and linux only; this file exists so that
// `GOOS=windows go build ./...` keeps working for cross-platform editor
// analysis, not because the weaker guarantee is acceptable for a release.
const gitExecutionIsAnchored = false

func gitCommand(dir *os.File, args []string) (*exec.Cmd, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir.Name()
	return cmd, nil
}
