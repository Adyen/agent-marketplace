//go:build !unix

package processstart

import "os/exec"

func detach(*exec.Cmd) {}
