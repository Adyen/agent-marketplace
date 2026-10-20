package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/remedy"
	"github.com/Adyen/agent-marketplace/internal/tui"
)

// terminalSafeError carries a command error to Cobra's error printer with its
// text made terminal-safe. Unwrap keeps errors.Is/errors.As identity.
type terminalSafeError struct {
	err  error
	text string
}

func (e terminalSafeError) Error() string { return e.text }
func (e terminalSafeError) Unwrap() error { return e.err }

type terminalSafeJoinedError struct {
	err  interface{ Unwrap() []error }
	text string
}

func (e terminalSafeJoinedError) Error() string   { return e.text }
func (e terminalSafeJoinedError) Unwrap() []error { return e.err.Unwrap() }

// terminalSafeCommandError renders every error in the chain with its next step.
func terminalSafeCommandError(err error, commandPath string) error {
	if err == nil {
		return nil
	}
	var lines []string
	hints := make(map[string]struct{})
	for _, group := range commandErrorGroups(err) {
		hint := remedy.For(group.err, commandPath)
		errorLines := group.lines
		if hint == "" {
			lines = append(lines, errorLines...)
			continue
		}
		errorLines = withoutRenderedRemediation(errorLines, hint)
		lines = append(lines, errorLines...)
		if _, exists := hints[hint]; exists {
			continue
		}
		hints[hint] = struct{}{}
		lines = append(lines, "hint: "+tui.TerminalSafeLine(hint))
	}
	text := terminalSafeLines(strings.Join(lines, "\n"))
	if text == "" {
		text = terminalSafeLines(fmt.Sprint(err))
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return terminalSafeJoinedError{err: joined, text: text}
	}
	return terminalSafeError{err: err, text: text}
}

type commandErrorGroup struct {
	err   error
	lines []string
}

func commandErrorGroups(err error) []commandErrorGroup {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		childText := make([]string, 0, len(children))
		for _, child := range children {
			if child != nil {
				childText = append(childText, child.Error())
			}
		}
		if err.Error() != strings.Join(childText, "\n") {
			return []commandErrorGroup{{err: err, lines: strings.Split(err.Error(), "\n")}}
		}
		var groups []commandErrorGroup
		for _, child := range children {
			groups = append(groups, commandErrorGroups(child)...)
		}
		return groups
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		child := wrapped.Unwrap()
		groups := commandErrorGroups(child)
		outer, inner := err.Error(), child.Error()
		if prefix, ok := strings.CutSuffix(outer, inner); ok && len(groups) > 0 && len(groups[0].lines) > 0 {
			groups[0].lines[0] = prefix + groups[0].lines[0]
			if remediable, ok := err.(interface{ Remediation() string }); ok && strings.TrimSpace(remediable.Remediation()) != "" {
				groups[0].err = err
			} else if coded, ok := err.(interface{ DiagnosticCode() string }); ok && coded.DiagnosticCode() != "" {
				innerCoded, innerOK := groups[0].err.(interface{ DiagnosticCode() string })
				if !innerOK || innerCoded.DiagnosticCode() == "" {
					groups[0].err = err
				}
			}
			return groups
		}
	}
	return []commandErrorGroup{{err: err, lines: strings.Split(err.Error(), "\n")}}
}

func withoutRenderedRemediation(lines []string, hint string) []string {
	filtered := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) == "→ "+hint {
			continue
		}
		filtered = append(filtered, line)
	}
	return filtered
}

func terminalSafeUsageError(err error, command string) error {
	if err == nil {
		return nil
	}
	hint := fmt.Sprintf("Run `%s --help` to see this command's usage.", command)
	text := terminalSafeLines(err.Error()) + "\nhint: " + tui.TerminalSafeLine(hint)
	return terminalSafeError{err: err, text: text}
}

func terminalSafeLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = tui.TerminalSafeLine(line)
	}
	return strings.Join(lines, "\n")
}

// guardCommandErrors routes every command-hook error through the shared hint renderer.
func guardCommandErrors(cmd *cobra.Command) {
	for _, hook := range []*func(*cobra.Command, []string) error{&cmd.PersistentPreRunE, &cmd.PreRunE, &cmd.RunE, &cmd.PostRunE, &cmd.PersistentPostRunE} {
		if run := *hook; run != nil {
			*hook = func(c *cobra.Command, args []string) error {
				return terminalSafeCommandError(run(c, args), c.CommandPath())
			}
		}
	}
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			return terminalSafeUsageError(validate(c, args), c.CommandPath())
		}
	}
	for _, child := range cmd.Commands() {
		guardCommandErrors(child)
	}
}
