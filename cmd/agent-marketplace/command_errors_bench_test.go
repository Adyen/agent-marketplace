package main

import (
	"errors"
	"testing"
)

// BenchmarkNewRootCmd measures building the command tree every invocation pays
// for, including the command error guard. No disk or network I/O is timed.
func BenchmarkNewRootCmd(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = newRootCmd()
	}
}

// BenchmarkTerminalSafeCommandError measures the failing-command path for an
// ordinary two-line joined configuration error.
func BenchmarkTerminalSafeCommandError(b *testing.B) {
	message := "v4.source-name: .agents/config.yaml:3: " + sourceNameRule
	err := errors.Join(errors.New(message), errors.New(message))
	b.ReportAllocs()
	for b.Loop() {
		_ = terminalSafeCommandError(err, "agent-marketplace sync")
	}
}
