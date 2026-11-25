package processstart

import (
	"context"
	"os/exec"
)

// Command constructs a context-bound subprocess command.
var Command = exec.CommandContext

// Detached constructs a context-bound subprocess detached from the controlling terminal on Unix.
func Detached(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := Command(ctx, name, args...)
	detach(cmd)
	return cmd
}
