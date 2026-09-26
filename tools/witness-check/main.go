// Command witness-check reads `go test -json` output on stdin and fails unless
// every package finished and passed and every behavior witness declared by the
// CLI surface guard actually ran and passed. A mapped witness that skipped or
// never ran is not coverage. With -require, the named top-level tests are the
// witnesses instead, so a dedicated gate cannot pass after running nothing.
// Package summaries and the output of failing tests are relayed so the log
// reads like a plain `go test` run.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

const (
	guardPackage  = "github.com/Adyen/agent-marketplace/cmd/agent-marketplace"
	guardTest     = "TestPublicCommandPathsHaveBehaviorTests"
	witnessPrefix = "behavior-witness: "
)

type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

func main() {
	require := flag.String("require", "", "comma-separated top-level tests that must pass, instead of the CLI guard's witnesses")
	flag.Parse()
	var required []string
	for _, name := range strings.Split(*require, ",") {
		if name = strings.TrimSpace(name); name != "" {
			required = append(required, name)
		}
	}
	if err := check(os.Stdin, os.Stdout, required); err != nil {
		fmt.Fprintln(os.Stderr, "witness-check:", err)
		os.Exit(1)
	}
}

func check(in io.Reader, out io.Writer, required []string) error {
	results := make(map[string]string)
	buffered := make(map[string][]string)
	// A package that started but never reported a result was killed or its
	// stream was truncated; its exit status would otherwise be lost.
	unfinished := make(map[string]bool)
	// Skips are routine across the root suite, so their reasons are kept for
	// guard-package tests and reported only when the skipped test is a witness.
	skipReasons := make(map[string]string)
	witnesses := slices.Clone(required)
	var failedPackages []string
	events := 0
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var e event
		if line := scanner.Bytes(); len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil || e.Action == "" {
			_, _ = fmt.Fprintf(out, "%s\n", line)
			continue
		}
		events++
		key := e.Package + "\x00" + e.Test
		switch {
		case e.Action == "build-output" || (e.Action == "output" && e.Test == ""):
			// -json implies verbose package output; drop the bare PASS line a
			// plain `go test` run does not print.
			if e.Output != "PASS\n" {
				_, _ = io.WriteString(out, e.Output)
			}
		case e.Action == "start" && e.Test == "":
			unfinished[e.Package] = true
		case e.Action == "output":
			buffered[key] = append(buffered[key], e.Output)
			if required == nil && e.Package == guardPackage && e.Test == guardTest {
				if _, name, ok := strings.Cut(e.Output, witnessPrefix); ok {
					witnesses = append(witnesses, strings.TrimSpace(name))
				}
			}
		case (e.Action == "pass" || e.Action == "skip") && e.Test == "":
			delete(unfinished, e.Package)
		case e.Action == "fail" && e.Test == "":
			delete(unfinished, e.Package)
			failedPackages = append(failedPackages, e.Package)
		case e.Action == "pass" || e.Action == "fail" || e.Action == "skip":
			if e.Action == "fail" {
				_, _ = io.WriteString(out, strings.Join(buffered[key], ""))
			}
			if (required != nil || e.Package == guardPackage) && !strings.Contains(e.Test, "/") {
				results[e.Test] = e.Action
				if e.Action == "skip" {
					skipReasons[e.Test] = strings.TrimSpace(strings.Join(buffered[key], ""))
				}
			}
			delete(buffered, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read go test -json: %w", err)
	}
	if events == 0 {
		return fmt.Errorf("no go test -json events; run tests with -json")
	}
	if len(failedPackages) > 0 {
		return fmt.Errorf("test packages failed: %s", strings.Join(failedPackages, ", "))
	}
	if len(unfinished) > 0 {
		packages := make([]string, 0, len(unfinished))
		for pkg := range unfinished {
			packages = append(packages, pkg)
		}
		slices.Sort(packages)
		return fmt.Errorf("test packages started but never reported a result: %s", strings.Join(packages, ", "))
	}
	if required == nil {
		if got := results[guardTest]; got != "pass" {
			return fmt.Errorf("%s in %s did not pass (%s); witness coverage is unknown", guardTest, guardPackage, stateLabel(got))
		}
		if len(witnesses) == 0 {
			return fmt.Errorf("%s declared no behavior witnesses", guardTest)
		}
	}
	slices.Sort(witnesses)
	witnesses = slices.Compact(witnesses)
	var problems []string
	for _, name := range witnesses {
		if got := results[name]; got != "pass" {
			problem := name + ": " + stateLabel(got)
			if reason := skipReasons[name]; reason != "" {
				problem += "\n    " + strings.ReplaceAll(reason, "\n", "\n    ")
			}
			problems = append(problems, problem)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("behavior witnesses without a passing run:\n  %s", strings.Join(problems, "\n  "))
	}
	label := "behavior witnesses"
	if required != nil {
		label = "required tests"
	}
	_, _ = fmt.Fprintf(out, "witness-check: %d %s ran and passed\n", len(witnesses), label)
	return nil
}

func stateLabel(action string) string {
	if action == "" {
		return "never ran"
	}
	return action
}
