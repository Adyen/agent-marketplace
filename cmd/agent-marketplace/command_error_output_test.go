package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/remedy"
)

const (
	hostileSourceConfig  = "apiVersion: agent-marketplace/v4\nsources:\n  \"\\e]0;TITLE\\a\\e[2J\\u202EEVIL\\N\":\n    directory: ./missing\n"
	ordinarySourceConfig = "apiVersion: agent-marketplace/v4\nsources:\n  \"bad name — café\":\n    directory: ./missing\n"
	sourceNameRule       = "must contain only letters, numbers, dots, underscores, or hyphens"
	hostileSourceName    = `source name "\x1b]0;TITLE\a\x1b[2J\u202eEVIL\u0085" ` + sourceNameRule
	ordinarySourceName   = `source name "bad name — café" ` + sourceNameRule
	hostileSourcePath    = `sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85: ` + hostileSourceName
	ordinarySourcePath   = `sources.bad name — café: ` + ordinarySourceName
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
			if want := "Error: validate config "; !strings.Contains(output, want) {
				t.Fatalf("%s: command error does not name the authored file, want %q in:\n%q", command, want, output)
			}
			for _, tc := range []struct {
				filename, message string
			}{{"config.yaml", hostileSourcePath}, {"config.local.yaml", ordinarySourcePath}} {
				path := filepath.Join(repo, ".agents", tc.filename)
				want := `"` + path + `": v4.source-name: ` + path + `:3: ` + tc.message
				if !strings.Contains(output, want) {
					t.Fatalf("%s: command error does not name authored file and line, want %q in:\n%q", command, want, output)
				}
			}
		}

		output, err := executeCommand("doctor")
		if err != nil {
			t.Fatalf("doctor: %v\n%q", err, output)
		}
		assertNoTerminalControls(t, "doctor", output)
		for _, tc := range []struct {
			filename, message string
		}{{"config.yaml", hostileSourcePath}, {"config.local.yaml", ordinarySourcePath}} {
			path := filepath.Join(repo, ".agents", tc.filename)
			want := `"` + path + `": v4.source-name: ` + path + `:3: ` + tc.message
			if !strings.Contains(output, want) || !strings.Contains(output, "error: validate config ") {
				t.Fatalf("doctor: configuration error does not name authored file and line, want %q in:\n%q", want, output)
			}
		}
		if !strings.Contains(output, "hint: ") {
			t.Fatalf("doctor: configuration error has no hint:\n%q", output)
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
				got := output.String()
				if !strings.HasPrefix(got, "Error: HOOK\\x1B[31m\nhint: ") {
					t.Fatalf("output is missing terminal-safe error or hint lines: %q", got)
				}
				if tc.name == "args" && !strings.Contains(got, "root child --help") {
					t.Fatalf("usage error hint does not point to command help: %q", got)
				}
			})
		}
	})

	t.Run("ordinary errors", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), ordinarySourceConfig)
		withWorkingDir(t, repo)

		output, err := executeCommand("list")
		if err == nil || !strings.HasPrefix(err.Error(), "validate config ") ||
			!strings.Contains(err.Error(), `config.yaml": v4.source-name: `+filepath.Join(repo, ".agents", "config.yaml")+`:3: `+ordinarySourcePath) ||
			!strings.Contains(err.Error(), "hint: In "+filepath.Join(repo, ".agents", "config.yaml")+":3, rename the source") {
			t.Fatalf("list: unexpected error %v\n%q", err, output)
		}
		if !strings.Contains(output, "Error: "+err.Error()+"\n") {
			t.Fatalf("list: ordinary error text changed:\n%q", output)
		}
	})
}

// spec: command-error-output/coded-errors-print-code-specific-guidance
// spec: command-error-output/uncoded-errors-print-a-diagnostic-next-step
// spec: command-error-output/usage-errors-point-to-command-help
// spec: command-error-output/joined-errors-print-each-distinct-hint-once
// spec: command-error-output/hints-leave-machine-readable-output-and-identity-unchanged
// spec: command-error-output/hints-cannot-control-the-terminal
func TestCommandErrorsPrintActionableHints(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("coded errors use code guidance", func(t *testing.T) {
		identity := &commandCodedError{code: "lock.stale", text: "repository lock is stale"}
		root, stderr, stdout := commandErrorTree(identity)
		root.SetArgs([]string{"sync"})
		err := root.Execute()
		if !errors.Is(err, identity) {
			t.Fatalf("Execute error lost identity: %v", err)
		}
		if got := stderr.String(); !strings.Contains(got, "Error: repository lock is stale\nhint: ") ||
			!strings.Contains(got, "agent-marketplace lock") || strings.Contains(got, "--scope repo") ||
			!strings.Contains(got, "agent-marketplace sync --check") {
			t.Fatalf("coded error has no code-specific hint:\n%s", got)
		}
		if stdout.Len() != 0 {
			t.Fatalf("error hint leaked to stdout: %q", stdout.String())
		}
	})

	t.Run("uncoded errors use a command-aware fallback", func(t *testing.T) {
		root, stderr, _ := commandErrorTree(errors.New("network unavailable"))
		root.SetArgs([]string{"sync"})
		if err := root.Execute(); err == nil {
			t.Fatal("Execute succeeded")
		}
		if got := stderr.String(); !strings.Contains(got, "Error: network unavailable\nhint: ") ||
			!strings.Contains(got, "agent-marketplace sync --check") {
			t.Fatalf("uncoded error has no sync diagnostic hint:\n%s", got)
		}
	})

	t.Run("remediation wins and stays terminal safe", func(t *testing.T) {
		err := &commandRemediableError{
			commandCodedError: commandCodedError{code: "lock.stale", text: "stale"},
			hint:              "Inspect\x1b[31m\nthen retry",
		}
		root, stderr, _ := commandErrorTree(err)
		root.SetArgs([]string{"sync"})
		if gotErr := root.Execute(); !errors.Is(gotErr, err) {
			t.Fatalf("Execute error lost identity: %v", gotErr)
		}
		got := stderr.String()
		if strings.ContainsAny(got, "\x1b\a") || !strings.Contains(got, "hint: Inspect\\x1B[31m\\x0Athen retry") {
			t.Fatalf("remediation hint was not terminal-safe on its own line: %q", got)
		}
		if strings.Contains(got, "lock --scope repo") {
			t.Fatalf("catalog hint overrode remediation: %q", got)
		}
	})

	t.Run("joined errors retain messages and deduplicate the same hint", func(t *testing.T) {
		root, stderr, _ := commandErrorTree(errors.Join(errors.New("first failure"), errors.New("second failure")))
		root.SetArgs([]string{"sync"})
		if err := root.Execute(); err == nil {
			t.Fatal("Execute succeeded")
		}
		got := stderr.String()
		if !strings.Contains(got, "Error: first failure\nhint: ") || !strings.Contains(got, "second failure\n") {
			t.Fatalf("joined errors were not rendered separately:\n%s", got)
		}
		if count := strings.Count(got, "hint:"); count != 1 {
			t.Fatalf("identical joined-error hints = %d, want 1:\n%s", count, got)
		}
	})

	t.Run("unknown flags point to command help", func(t *testing.T) {
		root := newRootCmd()
		var stderr, stdout bytes.Buffer
		root.SetErr(&stderr)
		root.SetOut(&stdout)
		root.SetArgs([]string{"sync", "--not-a-real-flag"})
		if err := root.Execute(); err == nil {
			t.Fatal("Execute succeeded")
		}
		if got := stderr.String(); !strings.Contains(got, "hint: ") || !strings.Contains(got, "agent-marketplace sync --help") {
			t.Fatalf("usage error has no command-help hint:\n%s", got)
		}
	})

	t.Run("error hints do not alter machine-readable stdout", func(t *testing.T) {
		identity := &commandCodedError{code: "lock.stale", text: "repository lock is stale"}
		var stderr, stdout bytes.Buffer
		root := &cobra.Command{Use: "agent-marketplace", SilenceUsage: true}
		root.AddCommand(&cobra.Command{
			Use: "sync",
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), `{"error":"repository lock is stale"}`)
				return identity
			},
		})
		root.SetErr(&stderr)
		root.SetOut(&stdout)
		guardCommandErrors(root)
		root.SetArgs([]string{"sync"})
		err := root.Execute()
		if !errors.Is(err, identity) {
			t.Fatalf("Execute error lost identity: %v", err)
		}
		var coded *commandCodedError
		if !errors.As(err, &coded) || coded != identity {
			t.Fatalf("Execute error lost coded identity: %v", err)
		}
		if got := stdout.String(); got != "{\"error\":\"repository lock is stale\"}\n" || strings.Contains(got, "hint:") {
			t.Fatalf("machine-readable output changed: %q", got)
		}
		if !strings.Contains(stderr.String(), "hint: ") {
			t.Fatalf("command error hint missing from stderr: %q", stderr.String())
		}
	})
}

// spec: command-error-output/organization-failures-direct-users-to-their-administrator
// spec: command-error-output/doctor-configuration-errors-carry-hints
func TestDoctorErrorListsPrintActionableHints(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var output bytes.Buffer
	err := writeDoctorErrorList(&output, []error{
		&commandCodedError{code: "organization.trust.lock.read", text: "cannot read organization lock"},
		errors.New("configuration file could not be read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "error: cannot read organization lock\nhint: ") ||
		!strings.Contains(strings.ToLower(got), "ask your administrator") ||
		!strings.Contains(got, "configuration file could not be read\nhint: ") {
		t.Fatalf("doctor error list is missing actionable hints:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "edit /etc") || strings.Contains(strings.ToLower(got), "delete /etc") {
		t.Fatalf("organization hint suggests changing organization files:\n%s", got)
	}
}

func TestDoctorErrorListsDeduplicateIdenticalHints(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var output bytes.Buffer
	if err := writeDoctorErrorList(&output, []error{errors.New("first failure"), errors.New("second failure")}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "error: ") != 2 || strings.Count(got, "hint: ") != 1 {
		t.Fatalf("doctor repeated a shared hint or omitted an error:\n%s", got)
	}
}

// spec: command-error-output/every-consumer-diagnostic-code-has-guidance
func TestCatalogCommandReferencesResolveInCobra(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	inlineCode := regexp.MustCompile("`([^`]+)`")
	command := regexp.MustCompile(`agent-marketplace\s+([a-zA-Z][a-zA-Z0-9-]*)(.*)$`)
	flag := regexp.MustCompile(`--([a-zA-Z][a-zA-Z0-9-]*)`)
	root := newRootCmd()
	for code, hint := range remedy.SampleHints() {
		for _, segment := range inlineCode.FindAllStringSubmatch(hint, -1) {
			match := command.FindStringSubmatch(segment[1])
			if len(match) == 0 {
				continue
			}
			cmd, _, err := root.Find([]string{match[1]})
			if err != nil {
				t.Errorf("%s hint names unknown command %q: %s", code, match[1], hint)
				continue
			}
			if cmd == nil || cmd.Name() != match[1] {
				t.Errorf("%s hint did not resolve command %q: %s", code, match[1], hint)
				continue
			}
			cmd.InitDefaultHelpFlag()
			for _, flagMatch := range flag.FindAllStringSubmatch(match[2], -1) {
				name := flagMatch[1]
				if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
					t.Errorf("%s hint names unknown flag --%s for %s: %s", code, name, cmd.CommandPath(), hint)
				}
			}
		}
	}
}

type commandCodedError struct {
	code string
	text string
}

func (e *commandCodedError) Error() string          { return e.text }
func (e *commandCodedError) DiagnosticCode() string { return e.code }

type commandRemediableError struct {
	commandCodedError
	hint string
}

func (e *commandRemediableError) Remediation() string { return e.hint }

func commandErrorTree(err error) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	var stderr, stdout bytes.Buffer
	root := &cobra.Command{Use: "agent-marketplace", SilenceUsage: true}
	child := &cobra.Command{
		Use:  "sync",
		RunE: func(*cobra.Command, []string) error { return err },
	}
	root.AddCommand(child)
	root.SetErr(&stderr)
	root.SetOut(&stdout)
	guardCommandErrors(root)
	return root, &stderr, &stdout
}

func assertNoTerminalControls(t *testing.T, command, output string) {
	t.Helper()
	if i := strings.IndexAny(output, "\x1b\a\u202e\u0085"); i >= 0 {
		t.Fatalf("%s: raw terminal control at byte %d:\n%q", command, i, output)
	}
}
