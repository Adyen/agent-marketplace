package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inProcessTestFiles hold runner infrastructure and pure unit tests that never
// touch process-global state, so their top-level tests run in the parent.
var inProcessTestFiles = map[string]bool{
	"command_integration_test.go": true,
	"docs_contract_test.go":       true,
	"process_runner_test.go":      true,
	"process_suite_test.go":       true,
	"warnings_test.go":            true,
	"version_test.go":             true,
}

// inProcessTests are individual tests in scenario files that intentionally
// run in the parent process.
var inProcessTests = map[string]bool{
	"TestMainIsolatesHome":                                true,
	"TestStartupTargetRegistrationsAreValid":              true,
	"TestRepositoryIgnoresDependencyTransactionLockFiles": true,
	"TestRapidProductionDependencyGuard":                  true,
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

// TestCommandIsolationGuard makes process isolation a suite invariant: every
// top-level test in a scenario file must open with
// `if isolateCommandTest(t) { return }` unless it is explicitly listed as an
// in-process test.
func TestCommandIsolationGuard(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("read test sources: %v", err)
	}
	for _, filename := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, violation := range commandIsolationViolations(filename, file) {
			t.Error(violation)
		}
	}
}

func commandIsolationViolations(filename string, file *ast.File) []string {
	var violations []string
	if inProcessTestFiles[filename] {
		return nil
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") || function.Name.Name == "TestMain" || inProcessTests[function.Name.Name] {
			continue
		}
		if !startsWithIsolationGuard(function) {
			violations = append(violations, fmt.Sprintf("%s: %s bypasses command isolation; open with `if isolateCommandTest(t) { return }`", filename, function.Name.Name))
		}
	}
	return violations
}

func startsWithIsolationGuard(function *ast.FuncDecl) bool {
	if function.Body == nil || len(function.Body.List) == 0 {
		return false
	}
	guard, ok := function.Body.List[0].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 {
		return false
	}
	call, ok := guard.Cond.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	if !ok || arg.Name != "t" {
		return false
	}
	callee, ok := call.Fun.(*ast.Ident)
	if !ok || callee.Name != "isolateCommandTest" {
		return false
	}
	ret, ok := guard.Body.List[0].(*ast.ReturnStmt)
	return ok && len(ret.Results) == 0
}
