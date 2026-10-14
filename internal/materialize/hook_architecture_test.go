package materialize

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	hookArchitectureTargetComparison = "target-comparison"
	hookArchitectureTargetCase       = "target-case"
	hookArchitectureForbiddenString  = "forbidden-string-literal"
)

type hookArchitectureViolation struct {
	File       string
	Function   string
	Rule       string
	Line       int
	Expression string
}

type hookArchitectureAllowance struct {
	File       string
	Function   string
	Rule       string
	Expression string
}

var hookArchitectureTargetSelectors = map[string]struct{}{
	"TargetClaude":   {},
	"TargetFactory":  {},
	"TargetCursor":   {},
	"TargetGemini":   {},
	"TargetWindsurf": {},
	"TargetDevin":    {},
	"TargetCodex":    {},
}

var hookArchitectureForbiddenStrings = []string{
	"CLAUDE_PLUGIN_ROOT",
	"CLAUDE_PLUGIN_DATA",
	"DROID_PLUGIN_ROOT",
	"CURSOR_PLUGIN_ROOT",
	"PLUGIN_ROOT",
	"PLUGIN_DATA",
	"extensionPath",
	"CLAUDE_PROJECT_DIR",
	"FACTORY_PROJECT_DIR",
	"CURSOR_PROJECT_DIR",
	"GEMINI_PROJECT_DIR",
	"DEVIN_PROJECT_DIR",
	"ROOT_WORKSPACE_PATH",
}

func scanHookArchitecturePackage(dir string) ([]hookArchitectureViolation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var violations []hookArchitectureViolation
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		found, err := scanHookArchitectureSource(name, string(source))
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		violations = append(violations, found...)
	}
	sortHookArchitectureViolations(violations)
	return violations, nil
}

func scanHookArchitectureSource(filename, source string) ([]hookArchitectureViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(filename)
	scanAll := hookArchitectureScansAllFunctions(base)
	var violations []hookArchitectureViolation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || (!scanAll && !hookArchitectureScansFunction(function.Name.Name)) {
			continue
		}
		var scanErr error
		add := func(rule string, node ast.Node) {
			expression, err := hookArchitecturePrintNode(fset, node)
			if err != nil {
				scanErr = err
				return
			}
			violations = append(violations, hookArchitectureViolation{
				File:       base,
				Function:   function.Name.Name,
				Rule:       rule,
				Line:       fset.Position(node.Pos()).Line,
				Expression: expression,
			})
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if node == nil {
				return true
			}
			switch typed := node.(type) {
			case *ast.BinaryExpr:
				if (typed.Op == token.EQL || typed.Op == token.NEQ) && hookArchitectureContainsTargetSelector(typed) {
					add(hookArchitectureTargetComparison, typed)
				}
			case *ast.CaseClause:
				for _, expression := range typed.List {
					if hookArchitectureContainsTargetSelector(expression) {
						add(hookArchitectureTargetCase, expression)
					}
				}
			case *ast.BasicLit:
				if typed.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(typed.Value)
				if err != nil {
					scanErr = err
					return false
				}
				for _, forbidden := range hookArchitectureForbiddenStrings {
					if strings.Contains(value, forbidden) {
						add(hookArchitectureForbiddenString, typed)
						break
					}
				}
			}
			return scanErr == nil
		})
		if scanErr != nil {
			return nil, fmt.Errorf("%s: %w", function.Name.Name, scanErr)
		}
	}
	sortHookArchitectureViolations(violations)
	return violations, nil
}

func hookArchitectureScansAllFunctions(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	if strings.Contains(base, "hook") {
		return true
	}
	switch base {
	case "owned_json_reconcile.go", "owned_aggregate.go", "gemini_native.go":
		return true
	default:
		return false
	}
}

func hookArchitectureScansFunction(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "hook") || name == "compileWorkItems" || name == "executeMaterializePlans"
}

func hookArchitectureContainsTargetSelector(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		selector, ok := current.(*ast.SelectorExpr)
		if ok {
			if _, target := hookArchitectureTargetSelectors[selector.Sel.Name]; target {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func hookArchitecturePrintNode(fset *token.FileSet, node ast.Node) (string, error) {
	var output bytes.Buffer
	if err := format.Node(&output, fset, node); err != nil {
		return "", err
	}
	return output.String(), nil
}

func sortHookArchitectureViolations(violations []hookArchitectureViolation) {
	sort.Slice(violations, func(i, j int) bool {
		left, right := violations[i], violations[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Function != right.Function {
			return left.Function < right.Function
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Expression < right.Expression
	})
}

func TestHookArchitectureSyntheticViolations(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		source     string
		function   string
		rule       string
		line       int
		expression string
	}{
		{
			name:       "target comparison",
			file:       "materialize.go",
			source:     "package materialize\nfunc branchHook() {\n if selected == hcc.TargetFactory {}\n}\n",
			function:   "branchHook",
			rule:       hookArchitectureTargetComparison,
			line:       3,
			expression: "selected == hcc.TargetFactory",
		},
		{
			name:       "reversed target comparison",
			file:       "materialize.go",
			source:     "package materialize\nfunc reverseHook() {\n if hcc.TargetCursor != selected {}\n}\n",
			function:   "reverseHook",
			rule:       hookArchitectureTargetComparison,
			line:       3,
			expression: "hcc.TargetCursor != selected",
		},
		{
			name:       "renamed target import alias",
			file:       "materialize.go",
			source:     "package materialize\nimport q \"example.com/target\"\nfunc aliasHook() {\n if selected == q.TargetClaude {}\n}\n",
			function:   "aliasHook",
			rule:       hookArchitectureTargetComparison,
			line:       4,
			expression: "selected == q.TargetClaude",
		},
		{
			name:       "switch target case",
			file:       "materialize.go",
			source:     "package materialize\nfunc switchHook() {\n switch selected {\n case hcc.TargetWindsurf:\n }\n}\n",
			function:   "switchHook",
			rule:       hookArchitectureTargetCase,
			line:       4,
			expression: "hcc.TargetWindsurf",
		},
		{
			name:       "nested target conversion",
			file:       "materialize.go",
			source:     "package materialize\nimport (\n hcc \"example.com/hcc\"\n targetmeta \"example.com/target\"\n)\nfunc nestedHook() {\n if selected == hcc.Target(targetmeta.TargetCodex) {}\n}\n",
			function:   "nestedHook",
			rule:       hookArchitectureTargetComparison,
			line:       7,
			expression: "selected == hcc.Target(targetmeta.TargetCodex)",
		},
		{
			name:       "plugin root literal without dollar",
			file:       "materialize.go",
			source:     "package materialize\nfunc pluginHook() {\n _ = \"CLAUDE_PLUGIN_ROOT\"\n}\n",
			function:   "pluginHook",
			rule:       hookArchitectureForbiddenString,
			line:       3,
			expression: `"CLAUDE_PLUGIN_ROOT"`,
		},
		{
			name:       "project directory literal without dollar",
			file:       "materialize.go",
			source:     "package materialize\nfunc projectHook() {\n _ = \"FACTORY_PROJECT_DIR\"\n}\n",
			function:   "projectHook",
			rule:       hookArchitectureForbiddenString,
			line:       3,
			expression: `"FACTORY_PROJECT_DIR"`,
		},
		{
			name:       "future callback in watched hook file",
			file:       "native_hook_activation.go",
			source:     "package materialize\nfunc futureCallback() {\n if selected != harnesscompile.TargetDevin {}\n}\n",
			function:   "futureCallback",
			rule:       hookArchitectureTargetComparison,
			line:       3,
			expression: "selected != harnesscompile.TargetDevin",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := scanHookArchitectureSource(test.file, test.source)
			if err != nil {
				t.Fatal(err)
			}
			want := []hookArchitectureViolation{{
				File:       test.file,
				Function:   test.function,
				Rule:       test.rule,
				Line:       test.line,
				Expression: test.expression,
			}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("AST findings = %#v, want %#v", got, want)
			}
		})
	}
}

func TestHookArchitectureFindingsArePerNodeAndSorted(t *testing.T) {
	source := `package materialize
func zHook() {
 if selected == hcc.TargetClaude {}
}
func aHook() {
 if selected != hcc.TargetCursor {}
 if selected == hcc.TargetDevin {}
}`
	got, err := scanHookArchitectureSource("native_hook_activation.go", source)
	if err != nil {
		t.Fatal(err)
	}
	want := []hookArchitectureViolation{
		{File: "native_hook_activation.go", Function: "aHook", Rule: hookArchitectureTargetComparison, Line: 6, Expression: "selected != hcc.TargetCursor"},
		{File: "native_hook_activation.go", Function: "aHook", Rule: hookArchitectureTargetComparison, Line: 7, Expression: "selected == hcc.TargetDevin"},
		{File: "native_hook_activation.go", Function: "zHook", Rule: hookArchitectureTargetComparison, Line: 3, Expression: "selected == hcc.TargetClaude"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AST findings = %#v, want %#v", got, want)
	}
}
func TestHookArchitectureDataDrivenPolicyFieldsPass(t *testing.T) {
	source := `package materialize
func hookPolicyFields(loader, loaded string, loadsHooksOf []string, machineScope bool, member string, envelope any) bool {
 return machineScope && member != "" && len(loadsHooksOf) > 0 && envelope != nil && loader != loaded
}`
	got, err := scanHookArchitectureSource("materialize.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("data-driven policy fields produced AST findings: %#v", got)
	}
}

func hookArchitectureReaderAllowances() []hookArchitectureAllowance {
	return []hookArchitectureAllowance{
		{File: "native_hook_inspect.go", Function: "nativeHookLayoutFor", Rule: hookArchitectureTargetCase, Expression: "hcc.TargetClaude"},
		{File: "native_hook_inspect.go", Function: "nativeHookLayoutFor", Rule: hookArchitectureTargetCase, Expression: "hcc.TargetCursor"},
		{File: "native_hook_inspect.go", Function: "nativeHookLayoutFor", Rule: hookArchitectureTargetCase, Expression: "hcc.TargetFactory"},
		{File: "native_hook_inspect.go", Function: "nativeHookLayoutFor", Rule: hookArchitectureTargetCase, Expression: "hcc.TargetWindsurf"},
	}
}

func filterHookArchitecture(findings []hookArchitectureViolation, allowances []hookArchitectureAllowance) ([]hookArchitectureViolation, []int) {
	used := make([]int, len(allowances))
	var unexpected []hookArchitectureViolation
	for _, finding := range findings {
		matched := false
		for i, allowance := range allowances {
			if finding.File == allowance.File && finding.Function == allowance.Function && finding.Rule == allowance.Rule && finding.Expression == allowance.Expression {
				used[i]++
				matched = true
				break
			}
		}
		if !matched {
			unexpected = append(unexpected, finding)
		}
	}
	return unexpected, used
}

func TestHookArchitectureReaderAllowancesAreExact(t *testing.T) {
	source := `package materialize
func nativeHookLayoutFor(target hcc.Target) {
 switch target {
 case hcc.TargetClaude:
 case hcc.TargetCursor:
 case hcc.TargetFactory:
 case hcc.TargetWindsurf:
 case hcc.TargetCodex:
 }
}
func futureLayoutHelper(target hcc.Target) bool {
 return target == hcc.TargetFactory
}`
	findings, err := scanHookArchitectureSource("native_hook_inspect.go", source)
	if err != nil {
		t.Fatal(err)
	}
	unexpected, _ := filterHookArchitecture(findings, hookArchitectureReaderAllowances())
	want := []hookArchitectureViolation{
		{File: "native_hook_inspect.go", Function: "futureLayoutHelper", Rule: hookArchitectureTargetComparison, Line: 12, Expression: "target == hcc.TargetFactory"},
		{File: "native_hook_inspect.go", Function: "nativeHookLayoutFor", Rule: hookArchitectureTargetCase, Line: 8, Expression: "hcc.TargetCodex"},
	}
	if !reflect.DeepEqual(unexpected, want) {
		t.Fatalf("unexpected synthetic findings = %#v, want %#v", unexpected, want)
	}
}

func TestHookArchitectureProductionTree(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test source path")
	}
	findings, err := scanHookArchitecturePackage(filepath.Dir(sourceFile))
	if err != nil {
		t.Fatal(err)
	}
	allowances := hookArchitectureReaderAllowances()
	unexpected, used := filterHookArchitecture(findings, allowances)
	if len(unexpected) != 0 {
		t.Errorf("unexpected hook architecture findings: %#v", unexpected)
	}
	for i, allowance := range allowances {
		if used[i] != 1 {
			t.Errorf("frozen reader allowance matched %d findings: %#v", used[i], allowance)
		}
	}
}
