package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "agent-marketplace-gui:", err)
		os.Exit(1)
	}
}
