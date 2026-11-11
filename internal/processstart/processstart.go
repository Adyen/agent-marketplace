package processstart

import (
	"os/exec"
)

// Command constructs a context-bound subprocess command.
var Command = exec.CommandContext
