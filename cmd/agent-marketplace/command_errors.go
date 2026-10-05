package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/session"
)

// terminalSafeError carries a command error to Cobra's error printer with its
// text made terminal-safe. Unwrap keeps errors.Is/errors.As identity.
type terminalSafeError struct {
	err  error
	text string
}

func (e terminalSafeError) Error() string { return e.text }
func (e terminalSafeError) Unwrap() error { return e.err }

// terminalSafeCommandError returns err unchanged unless its text carries
// terminal controls. Line breaks between joined errors are kept; every other
// control inside a line becomes a visible escape.
func terminalSafeCommandError(err error) error {
	if err == nil {
		return nil
	}
	text := fmt.Sprint(err)
	if safe := terminalSafeLines(text); safe != text {
		return terminalSafeError{err: err, text: safe}
	}
	return err
}

func terminalSafeLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = session.TerminalSafeLine(line)
	}
	return strings.Join(lines, "\n")
}

// guardCommandErrors routes every error returned by the command tree's hooks
// through terminalSafeCommandError before Cobra prints it as "Error: ...".
func guardCommandErrors(cmd *cobra.Command) {
	for _, hook := range []*func(*cobra.Command, []string) error{&cmd.PersistentPreRunE, &cmd.PreRunE, &cmd.RunE, &cmd.PostRunE, &cmd.PersistentPostRunE} {
		if run := *hook; run != nil {
			*hook = func(c *cobra.Command, args []string) error { return terminalSafeCommandError(run(c, args)) }
		}
	}
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(c *cobra.Command, args []string) error { return terminalSafeCommandError(validate(c, args)) }
	}
	for _, child := range cmd.Commands() {
		guardCommandErrors(child)
	}
}
