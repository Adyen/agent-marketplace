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
	err := errors.Join(errors.New(ordinaryError), errors.New(ordinaryError))
	b.ReportAllocs()
	for b.Loop() {
		_ = terminalSafeCommandError(err)
	}
}
