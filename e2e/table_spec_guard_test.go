package e2e_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type tableSpecCase struct {
	name   string
	tagged bool
}

func checkTableSpecTags(sources map[string]string, baselineText string) error {
	cases, err := tableSpecCases(sources)
	if err != nil {
		return err
	}
	baseline, err := parseTableSpecBaseline(baselineText)
	if err != nil {
		return err
	}

	var failures []string
	seen := make(map[string]bool, len(cases))
	for _, testCase := range cases {
		seen[testCase.name] = true
		if testCase.tagged && baseline[testCase.name] {
			failures = append(failures, tableSpecFailure(testCase.name, "is tagged but remains in the untagged baseline; remove the baseline entry"))
		} else if !testCase.tagged && !baseline[testCase.name] {
			failures = append(failures, tableSpecFailure(testCase.name, "is untagged and missing from the frozen baseline"))
		}
	}
	for name := range baseline {
		if !seen[name] {
			failures = append(failures, tableSpecFailure(name, "is in the baseline but no longer matches a table case; remove the stale baseline entry"))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	sort.Strings(failures)
	return fmt.Errorf("%s", strings.Join(failures, "\n"))
}

func tableSpecFailure(name, detail string) string {
	return fmt.Sprintf("case %q %s; add a `// spec:` line bound to an existing OpenSpec scenario for untagged cases, and remove tagged or unmatched cases from the baseline", name, detail)
}

func tableSpecCases(sources map[string]string) ([]tableSpecCase, error) {
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var cases []tableSpecCase
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					if name.Name != "testTable" || index >= len(value.Values) {
						continue
					}
					table, ok := value.Values[index].(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, element := range table.Elts {
						literal, ok := element.(*ast.CompositeLit)
						if !ok {
							continue
						}
						name, err := tableSpecCaseName(literal)
						if err != nil {
							return nil, fmt.Errorf("%s:%d: %w", path, fset.Position(literal.Pos()).Line, err)
						}
						cases = append(cases, tableSpecCase{
							name:   name,
							tagged: hasTableSpecTagBefore(file, fset, literal),
						})
					}
				}
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "init" || fn.Body == nil {
				continue
			}
			appended, err := appendedTableSpecCases(file, fset, fn)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			cases = append(cases, appended...)
		}
	}
	return cases, nil
}

func tableSpecCaseName(literal *ast.CompositeLit) (string, error) {
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			continue
		}
		prefix, dynamic, err := leadingTableNameString(field.Value)
		if err != nil {
			return "", err
		}
		if dynamic {
			return prefix + "*", nil
		}
		return prefix, nil
	}
	return "", fmt.Errorf("testTable case has no Name field")
}

func leadingTableNameString(expr ast.Expr) (string, bool, error) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false, fmt.Errorf("testTable case Name is not a string literal or concatenation")
		}
		name, err := strconv.Unquote(value.Value)
		return name, false, err
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false, fmt.Errorf("testTable case Name is not a string concatenation")
		}
		prefix, _, err := leadingTableNameString(value.X)
		return prefix, true, err
	case *ast.ParenExpr:
		return leadingTableNameString(value.X)
	default:
		return "", false, fmt.Errorf("testTable case Name has no leading string literal")
	}
}

func appendedTableSpecCases(file *ast.File, fset *token.FileSet, fn *ast.FuncDecl) ([]tableSpecCase, error) {
	literals := make(map[string][]*ast.CompositeLit)
	var loops []ast.Node
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for index, lhs := range value.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || index >= len(value.Rhs) {
					continue
				}
				literal, ok := value.Rhs[index].(*ast.CompositeLit)
				if ok && isTestCaseLiteral(literal) {
					literals[ident.Name] = append(literals[ident.Name], literal)
				}
			}
		case *ast.RangeStmt:
			loops = append(loops, value)
		case *ast.ForStmt:
			loops = append(loops, value)
		}
		return true
	})

	var cases []tableSpecCase
	var caseErr error
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.AssignStmt)
		if !ok || !assignsTestTable(statement) {
			return true
		}
		call, ok := statement.Rhs[0].(*ast.CallExpr)
		if !ok || !isAppendCall(call) {
			return true
		}
		tagged := hasTableSpecTagBefore(file, fset, statement)
		var outermost ast.Node
		for _, loop := range loops {
			if loop.Pos() > statement.Pos() || loop.End() < statement.End() {
				continue
			}
			if outermost == nil || loop.End()-loop.Pos() > outermost.End()-outermost.Pos() {
				outermost = loop
			}
		}
		if outermost != nil && hasTableSpecTagBefore(file, fset, outermost) {
			tagged = true
		}
		for _, argument := range call.Args[1:] {
			var literal *ast.CompositeLit
			switch value := argument.(type) {
			case *ast.CompositeLit:
				literal = value
			case *ast.Ident:
				for _, candidate := range literals[value.Name] {
					if candidate.Pos() < statement.Pos() && (literal == nil || candidate.Pos() > literal.Pos()) {
						literal = candidate
					}
				}
			}
			if literal == nil {
				caseErr = fmt.Errorf("%s:%d: cannot resolve appended TestCase literal", fset.Position(statement.Pos()).Filename, fset.Position(statement.Pos()).Line)
				continue
			}
			name, err := tableSpecCaseName(literal)
			if err != nil {
				caseErr = fmt.Errorf("%s:%d: %w", fset.Position(literal.Pos()).Filename, fset.Position(literal.Pos()).Line, err)
				continue
			}
			cases = append(cases, tableSpecCase{name: name, tagged: tagged})
		}
		return true
	})
	return cases, caseErr
}

func assignsTestTable(statement *ast.AssignStmt) bool {
	for _, lhs := range statement.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "testTable" {
			return true
		}
	}
	return false
}

func isAppendCall(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "append" || len(call.Args) == 0 {
		return false
	}
	table, ok := call.Args[0].(*ast.Ident)
	return ok && table.Name == "testTable"
}

func isTestCaseLiteral(literal *ast.CompositeLit) bool {
	ident, ok := literal.Type.(*ast.Ident)
	return ok && ident.Name == "TestCase"
}

func hasTableSpecTagBefore(file *ast.File, fset *token.FileSet, node ast.Node) bool {
	line := fset.Position(node.Pos()).Line
	for _, group := range file.Comments {
		if fset.Position(group.End()).Line+1 != line {
			continue
		}
		for _, comment := range group.List {
			if strings.HasPrefix(strings.TrimSpace(comment.Text), "// spec:") {
				return true
			}
		}
	}
	return false
}

func parseTableSpecBaseline(text string) (map[string]bool, error) {
	baseline := make(map[string]bool)
	previous := ""
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if baseline[name] {
			return nil, fmt.Errorf("%s", tableSpecFailure(name, "is duplicated in the baseline"))
		}
		if previous != "" && name < previous {
			return nil, fmt.Errorf("%s", tableSpecFailure(name, fmt.Sprintf("is unsorted after baseline case %q", previous)))
		}
		baseline[name] = true
		previous = name
	}
	return baseline, nil
}

func TestTableSpecTagsCheckRejectsUntaggedCase(t *testing.T) {
	err := checkTableSpecTags(map[string]string{
		"table_test.go": `package e2e_test
var testTable = []TestCase{
	{Name: "new/case"},
}
`,
	}, "")
	if err == nil {
		t.Fatal("checkTableSpecTags() accepted an untagged case missing from the baseline")
	}
	for _, want := range []string{
		"new/case",
		"add a `// spec:` line bound to an existing OpenSpec scenario",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checkTableSpecTags() error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "extend the baseline") {
		t.Errorf("checkTableSpecTags() error suggests extending the baseline: %q", err)
	}
}

func TestTableSpecTagsCheckRejectsBaselineDrift(t *testing.T) {
	tag := "/" + "/ spec: capability/scenario"
	taggedSource := map[string]string{
		"table_test.go": "package e2e_test\nvar testTable = []TestCase{\n\t" + tag + `
	{Name: "now/tagged"},
}
`,
	}
	tests := []struct {
		name     string
		baseline string
		want     string
	}{
		{name: "tagged baseline entry", baseline: "now/tagged\n", want: "now/tagged"},
		{name: "unmatched baseline entry", baseline: "missing/case\n", want: "missing/case"},
		{name: "unsorted baseline", baseline: "z/case\na/case\n", want: "z/case"},
		{name: "duplicate baseline", baseline: "same/case\nsame/case\n", want: "same/case"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTableSpecTags(taggedSource, tt.baseline)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("checkTableSpecTags() error = %v, want an error naming %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "add a `// spec:` line bound to an existing OpenSpec scenario") {
				t.Errorf("checkTableSpecTags() error omits the spec-tag fix: %q", err)
			}
			if strings.Contains(err.Error(), "extend the baseline") {
				t.Errorf("checkTableSpecTags() error suggests extending the baseline: %q", err)
			}
		})
	}
}

func TestTableSpecTagsCheckRecognizesTagForms(t *testing.T) {
	firstTag := "/" + "/ spec: capability/first"
	secondTag := "/" + "/ spec: capability/second"
	loopTag := "/" + "/ spec: capability/loop"
	appendTag := "/" + "/ spec: capability/append"
	sources := map[string]string{
		"table_test.go": "package e2e_test\nvar testTable = []TestCase{\n\t" + firstTag + "\n\t" + secondTag + `
	{Name: "stacked/tag"},
}
func init() {
	` + loopTag + `
	for range []string{"outer"} {
		for range []string{"loop/tag"} {
			testTable = append(testTable, TestCase{Name: "loop/tag"})
		}
	}
	` + appendTag + `
	testTable = append(testTable, TestCase{Name: "append/tag"})
}
`,
	}
	if err := checkTableSpecTags(sources, ""); err != nil {
		t.Fatalf("checkTableSpecTags() rejected stacked or outer-loop tags: %v", err)
	}
}

func TestTableSpecTagsCheckResolvesIdentifierAndDynamicName(t *testing.T) {
	sources := map[string]string{
		"native_test.go": `package e2e_test
func init() {
	for _, target := range []string{"claude", "gemini"} {
		tc := TestCase{Name: "native_mcp/" + target + "/user"}
		testTable = append(testTable, tc)
	}
}
`,
	}
	if err := checkTableSpecTags(sources, "native_mcp/*\n"); err != nil {
		t.Fatalf("checkTableSpecTags() did not resolve the identifier's dynamic name: %v", err)
	}
}

func TestTableSpecTagsCheckResolvesReassignedIdentifier(t *testing.T) {
	sources := map[string]string{
		"native_test.go": `package e2e_test
func init() {
	tc := TestCase{Name: "a"}
	testTable = append(testTable, tc)
	tc = TestCase{Name: "b"}
	testTable = append(testTable, tc)
}
`,
	}
	if err := checkTableSpecTags(sources, "a\nb\n"); err != nil {
		t.Fatalf("checkTableSpecTags() did not resolve each append's latest identifier assignment: %v", err)
	}
}

func TestTableSpecTagsCheckRequiresAdjacentTag(t *testing.T) {
	tag := "/" + "/ spec: capability/scenario"
	sources := map[string]string{
		"table_test.go": "package e2e_test\nvar testTable = []TestCase{\n\t" + tag + `

	{Name: "blank/gap"},
}
`,
	}
	err := checkTableSpecTags(sources, "")
	if err == nil || !strings.Contains(err.Error(), "blank/gap") {
		t.Fatalf("checkTableSpecTags() error = %v, want an untagged blank/gap case", err)
	}
}

func TestTableSpecTags(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		sources[entry.Name()] = string(content)
	}
	baseline, err := os.ReadFile(filepath.Join("testdata", "table_untagged_baseline.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTableSpecTags(sources, string(baseline)); err != nil {
		t.Fatal(err)
	}
}
