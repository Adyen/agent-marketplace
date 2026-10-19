// Command specaudit enforces that OpenSpec specs stay the test-bound authority
// for repository behavior. See openspec/specs/spec-governance/spec.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

const usage = `usage: specaudit <command> [flags]

commands:
  trace     every known scenario has a // spec: test annotation and vice versa
  archived  no active change has every task checked
  gate      a behavior change carries a spec delta or a Spec-Impact trailer

flags:
  -root dir     repository root (default ".")
  -base sha     gate only: pull request base commit
  -head sha     gate only: pull request head commit
`

// finding is one violation, reported as "<kind>: <detail>". Kinds are stable
// so tests and CI logs can key on them.
type finding struct {
	kind   string
	detail string
}

const (
	kindDuplicateSlug      = "duplicate-slug"
	kindUnboundScenario    = "unbound-scenario"
	kindDanglingAnnotation = "dangling-annotation"
	kindUnarchivedChange   = "unarchived-change"
	kindMissingSpecDelta   = "missing-spec-delta"
	kindSpecImpactNoReason = "spec-impact-reason-required"
)

// errFindings signals that findings were already printed.
var errFindings = errors.New("findings reported")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errFindings) {
			fmt.Fprintln(os.Stderr, "specaudit:", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return errors.New("missing command")
	}
	cmd := args[0]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	base := fs.String("base", "", "pull request base commit")
	head := fs.String("head", "", "pull request head commit")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	var (
		findings []finding
		summary  string
		err      error
	)
	switch cmd {
	case "trace":
		findings, summary, err = trace(*root)
	case "archived":
		findings, summary, err = archived(*root)
	case "gate":
		if *base == "" || *head == "" {
			return errors.New("gate requires -base and -head")
		}
		findings, summary, err = gate(*root, *base, *head)
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	return report(cmd, findings, summary, stdout, stderr)
}

func report(cmd string, findings []finding, summary string, stdout, stderr io.Writer) error {
	if len(findings) == 0 {
		_, _ = fmt.Fprintf(stdout, "specaudit %s: ok (%s)\n", cmd, summary)
		return nil
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].kind != findings[j].kind {
			return findings[i].kind < findings[j].kind
		}
		return findings[i].detail < findings[j].detail
	})
	for _, f := range findings {
		_, _ = fmt.Fprintf(stderr, "specaudit %s: %s: %s\n", cmd, f.kind, f.detail)
	}
	_, _ = fmt.Fprintf(stderr, "specaudit %s: %d finding(s)\n", cmd, len(findings))
	return errFindings
}
