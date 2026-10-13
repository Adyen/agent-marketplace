package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
)

const (
	hostileSourceConfig  = "apiVersion: agent-marketplace/v4\nsources:\n  \"\\e]0;TITLE\\a\\e[2J\\u202EEVIL\\N\":\n    directory: ./missing\n"
	ordinarySourceConfig = "apiVersion: agent-marketplace/v4\nsources:\n  \"bad name — café\":\n    directory: ./missing\n"
	sourceNameRule       = "must match [a-zA-Z0-9][a-zA-Z0-9._-]*"
	escapedHostileError  = `v4.source-name: sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85 (line 3): ` + sourceNameRule
	ordinaryError        = "v4.source-name: sources.bad name — café (line 3): " + sourceNameRule
)

// spec: command-error-output/configuration-errors-cannot-control-the-terminal
// spec: command-error-output/ordinary-command-errors-are-printed-unchanged
func TestCommandErrorsAreTerminalSafe(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	t.Run("repository configuration controls", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), hostileSourceConfig)
		writeFile(t, filepath.Join(repo, ".agents", "config.local.yaml"), ordinarySourceConfig)
		withWorkingDir(t, repo)

		for _, command := range []string{"status", "list", "sync"} {
			output, err := executeCommand(command)
			if validation, ok := errors.AsType[*config.V4ValidationError](err); !ok || validation.Code != "v4.source-name" {
				t.Fatalf("%s: error identity lost: %v\n%q", command, err, output)
			}
			assertNoTerminalControls(t, command, output)
			if want := "Error: " + escapedHostileError + "\n" + ordinaryError + "\n"; !strings.Contains(output, want) {
				t.Fatalf("%s: command error not escaped line by line, want %q in:\n%q", command, want, output)
			}
		}

		output, err := executeCommand("doctor")
		if err != nil {
			t.Fatalf("doctor: %v\n%q", err, output)
		}
		assertNoTerminalControls(t, "doctor", output)
		if want := "error: " + escapedHostileError + "\nerror: " + ordinaryError + "\n"; !strings.Contains(output, want) {
			t.Fatalf("doctor: configuration errors not escaped, want %q in:\n%q", want, output)
		}
	})

	t.Run("every command hook", func(t *testing.T) {
		hostile := errors.New("HOOK\x1b[31m")
		fail := func(*cobra.Command, []string) error { return hostile }
		for _, tc := range []struct {
			name      string
			configure func(*cobra.Command)
		}{
			{"args", func(c *cobra.Command) { c.Args = fail }},
			{"persistent pre-run", func(c *cobra.Command) { c.PersistentPreRunE = fail }},
			{"pre-run", func(c *cobra.Command) { c.PreRunE = fail }},
			{"run", func(c *cobra.Command) { c.RunE = fail }},
			{"post-run", func(c *cobra.Command) { c.PostRunE = fail }},
			{"persistent post-run", func(c *cobra.Command) { c.PersistentPostRunE = fail }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := &cobra.Command{Use: "root", SilenceUsage: true}
				child := &cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }}
				tc.configure(child)
				root.AddCommand(child)
				guardCommandErrors(root)
				var output bytes.Buffer
				root.SetOut(&output)
				root.SetErr(&output)
				root.SetArgs([]string{"child"})
				if err := root.Execute(); !errors.Is(err, hostile) {
					t.Fatalf("error identity lost: %v", err)
				}
				if got, want := output.String(), "Error: HOOK\\x1B[31m\n"; got != want {
					t.Fatalf("output = %q, want %q", got, want)
				}
			})
		}
	})

	t.Run("ordinary errors", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), ordinarySourceConfig)
		withWorkingDir(t, repo)

		output, err := executeCommand("list")
		if err == nil || err.Error() != ordinaryError {
			t.Fatalf("list: unexpected error %v\n%q", err, output)
		}
		if !strings.Contains(output, "Error: "+ordinaryError+"\n") {
			t.Fatalf("list: ordinary error text changed:\n%q", output)
		}
	})
}

func assertNoTerminalControls(t *testing.T, command, output string) {
	t.Helper()
	if i := strings.IndexAny(output, "\x1b\a\u202e\u0085"); i >= 0 {
		t.Fatalf("%s: raw terminal control at byte %d:\n%q", command, i, output)
	}
}
