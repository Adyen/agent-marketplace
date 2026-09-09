package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type commandTest struct {
	name string
	run  func(*testing.T)
}

func TestCommandIntegration(t *testing.T) {
	tests := slices.Clone(commandTests)
	if shuffle := flag.Lookup("test.shuffle").Value.String(); shuffle != "off" {
		seed, err := strconv.ParseInt(shuffle, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		rand.New(rand.NewSource(seed)).Shuffle(len(tests), func(i, j int) { tests[i], tests[j] = tests[j], tests[i] })
	}
	for _, scenario := range tests {
		t.Run(scenario.name, func(t *testing.T) {
			runIsolatedCommandTest(t, scenario.run)
		})
	}
}

func runIsolatedCommandTest(t *testing.T, run func(*testing.T)) {
	t.Helper()
	if !isolateCommandTest(t) {
		run(t)
	}
}

func commandScenarioPattern(selector, filter string) (string, error) {
	parts := strings.Split(selector, "/")
	var patterns []string
	for _, branch := range splitCommandTestFilter(filter) {
		matches := true
		for i, part := range branch {
			pattern, err := regexp.Compile(part)
			if err != nil {
				return "", err
			}
			if i < len(parts) && !pattern.MatchString(parts[i]) {
				matches = false
			}
		}
		if !matches {
			continue
		}
		pattern := exactCommandTestPattern(selector)
		if len(branch) > len(parts) {
			pattern += "/" + strings.Join(branch[len(parts):], "/")
		}
		patterns = append(patterns, pattern)
	}
	if len(patterns) == 0 {
		return "", fmt.Errorf("filter %q does not select scenario %q", filter, selector)
	}
	return strings.Join(patterns, "|"), nil
}

func splitCommandTestFilter(filter string) [][]string {
	branches := [][]string{{}}
	start, brackets, parentheses := 0, 0, 0
	for i := 0; i < len(filter); i++ {
		switch filter[i] {
		case '\\':
			i++
		case '[':
			brackets++
		case ']':
			brackets = max(0, brackets-1)
		case '(':
			if brackets == 0 {
				parentheses++
			}
		case ')':
			if brackets == 0 {
				parentheses--
			}
		case '/', '|':
			if brackets == 0 && parentheses == 0 {
				last := len(branches) - 1
				branches[last] = append(branches[last], filter[start:i])
				start = i + 1
				if filter[i] == '|' {
					branches = append(branches, []string{})
				}
			}
		}
	}
	last := len(branches) - 1
	branches[last] = append(branches[last], filter[start:])
	return branches
}

func TestCommandScenarioRegistration(t *testing.T) {
	registered := map[string]bool{}
	for _, scenario := range commandTests {
		if scenario.name == "" || strings.ContainsAny(scenario.name, "/ \t\n") || scenario.run == nil || registered[scenario.name] {
			t.Fatalf("invalid or duplicate scenario %q", scenario.name)
		}
		name := runtime.FuncForPC(reflect.ValueOf(scenario.run).Pointer()).Name()
		if !strings.HasSuffix(name, ".scenario"+scenario.name) {
			t.Errorf("scenario %q is bound to %s", scenario.name, name)
		}
		registered[scenario.name] = true
	}
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("read test sources: %v", err)
	}
	for _, filename := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, violation := range commandRegistrationViolations(filename, file, registered) {
			t.Error(violation)
		}
	}
}

func commandRegistrationViolations(filename string, file *ast.File, registered map[string]bool) []string {
	var violations []string
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}
		name := function.Name.Name
		if scenario, ok := strings.CutPrefix(name, "scenario"); ok && !registered[scenario] {
			violations = append(violations, fmt.Sprintf("%s: scenario %s is not registered", filename, scenario))
		}
		if !strings.HasPrefix(name, "Test") || filename == "process_runner_test.go" || filename == "process_suite_test.go" || filename == "warnings_test.go" || filename == "version_test.go" {
			continue
		}
		switch name {
		case "TestMain", "TestMainIsolatesHome", "TestStartupTargetRegistrationsAreValid", "TestCommandIntegration", "TestCommandScenarioRegistration", "TestRepositoryIgnoresDependencyTransactionLockFiles":
		default:
			violations = append(violations, fmt.Sprintf("%s: %s bypasses command isolation; add a named scenario to commandTests", filename, name))
		}
	}
	return violations
}
