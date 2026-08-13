package diag

import (
	"bytes"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file replaces a line-based regex with a syntax-tree scan.
//
// The retired regex matcher in severity_guard_test.go only sees a comparison
// when the operator and the identifier `SeverityError` land on the SAME
// physical line with nothing between them. severity_guard_adversarial_test.go
// enumerates the reintroductions that slip past it: gofmt-legal line wrapping,
// `int(...)` conversions, a named constant aliasing SeverityError, a bare
// `>= 0`, a helper taking two Severity parameters, sorting by the ordinal, and
// a `//` inside a string literal that truncates the naive comment stripper.
//
// The scan below works on the parsed AST instead, so line breaks, comments and
// string literals cannot hide anything. The rule it enforces is deliberately
// stricter than "do not mention SeverityError":
//
//	A magnitude comparison (<, <=, >, >=) involving a diag.Severity operand is
//	forbidden in production code UNLESS the other operand is written literally
//	as SeverityWarning or SeverityInfo.
//
// Rationale: `> SeverityWarning` ("drop Info") is the one shape that is both
// common and readable, because the constant names the boundary. Everything
// else — a variable threshold, a numeric literal, a sort comparator, a helper
// on two Severity parameters — hides the fact that the ordinals count DOWN in
// gravity and must go through Severity.AtLeastAsSevereAs / Diagnostic.IsError.

// Severity.AtLeastAsSevereAs is the only method allowed to compare Severity
// ordinals directly. This is where the ordinal direction is defined, so it
// necessarily contains the raw operator.
func isSeverityComparisonExemptFunc(fn *ast.FuncDecl) bool {
	if fn.Name.Name != "AtLeastAsSevereAs" || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	receiver, ok := fn.Recv.List[0].Type.(*ast.Ident)
	return ok && receiver.Name == "Severity"
}

const severityASTGuardExplanation = `
Severity ordinals count DOWN in gravity (SeverityError=0, SeverityWarning=1,
SeverityInfo=2). A magnitude comparison on a Severity therefore reads backwards
from what it does. Use:

    d.IsError()                             // fail-closed error
    d.Severity.AtLeastAsSevereAs(threshold) // at least as grave as threshold
    diag.HasErrors(diagnostics)             // any error in a slice

The only magnitude comparison that stays legible is one whose other operand is
written literally as SeverityWarning or SeverityInfo ("at most a warning",
"drop info"). A variable threshold, a numeric literal, an int() conversion, a
sort comparator or a two-Severity helper all hide the direction and are
rejected.`

// TestNoSeverityOrdinalComparisonsInProductionCode is the strengthened guard.
func TestNoSeverityOrdinalComparisonsInProductionCode(t *testing.T) {
	offenders := scanSeverityOrdinalComparisons(t, repoRoot(t))
	if len(offenders) > 0 {
		t.Fatalf("Severity ordinal comparison in production code:\n  %s\n%s",
			strings.Join(offenders, "\n  "), severityASTGuardExplanation)
	}
}

// scanSeverityOrdinalComparisons walks root and returns "file:line: src" for
// every offending comparison in a non-test .go file.
func scanSeverityOrdinalComparisons(t *testing.T, root string) []string {
	t.Helper()

	type parsedFile struct {
		file *ast.File
		rel  string
	}
	type packageFiles struct {
		fset        *token.FileSet
		packagePath string
		files       []parsedFile
	}

	skipDirs := map[string]bool{".git": true, "dist": true, "node_modules": true, "testdata": true, "vendor": true}
	fileSets := make(map[string]*token.FileSet)
	packages := make(map[string]*packageFiles)
	var packageKeys []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		dir := filepath.Dir(path)
		fset := fileSets[dir]
		if fset == nil {
			fset = token.NewFileSet()
			fileSets[dir] = fset
		}
		file, parseErr := parser.ParseFile(fset, path, data, 0)
		if parseErr != nil {
			return parseErr
		}
		key := dir + "\x00" + file.Name.Name
		group := packages[key]
		if group == nil {
			group = &packageFiles{fset: fset, packagePath: dir + "/" + file.Name.Name}
			packages[key] = group
			packageKeys = append(packageKeys, key)
		}
		group.files = append(group.files, parsedFile{file: file, rel: rel})
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}

	var offenders []string
	for _, key := range packageKeys {
		group := packages[key]
		files := make([]*ast.File, len(group.files))
		for index, parsed := range group.files {
			files[index] = parsed.file
		}
		info := severityTypeInfo(group.packagePath, group.fset, files)
		for _, parsed := range group.files {
			for _, hit := range severityOffendersInFile(group.fset, parsed.file, info) {
				offenders = append(offenders, parsed.rel+":"+strconv.Itoa(hit.line)+": "+hit.src)
			}
		}
	}
	return offenders
}

type severityHit struct {
	line int
	src  string
}

func severityOffendersInFile(fset *token.FileSet, file *ast.File, info *types.Info) []severityHit {
	var hits []severityHit
	ast.Walk(severityVisitor{fset: fset, hits: &hits, typeInfo: info}, file)
	return hits
}

type severityVisitor struct {
	fset     *token.FileSet
	hits     *[]severityHit
	typeInfo *types.Info
	exempt   bool
	sevNames map[string]bool
}

func (v severityVisitor) Visit(n ast.Node) ast.Visitor {
	switch node := n.(type) {
	case *ast.FuncDecl:
		v.exempt = isSeverityComparisonExemptFunc(node)
		v.sevNames = severityTypedNames(node.Recv, node.Type)
	case *ast.FuncLit:
		names := severityTypedNames(nil, node.Type)
		for name := range v.sevNames {
			names[name] = true
		}
		v.sevNames = names
	case *ast.BinaryExpr:
		if v.exempt || !isMagnitudeComparison(node.Op) {
			return v
		}
		left := renderExpr(v.fset, node.X)
		right := renderExpr(v.fset, node.Y)
		if !severityFlavoured(node.X, left, v.sevNames, v.typeInfo) && !severityFlavoured(node.Y, right, v.sevNames, v.typeInfo) {
			return v
		}
		// Allowed: the other operand names the boundary literally.
		if (isNamedThreshold(right) && !mentionsSeverityError(left)) ||
			(isNamedThreshold(left) && !mentionsSeverityError(right)) {
			return v
		}
		*v.hits = append(*v.hits, severityHit{
			line: v.fset.Position(node.Pos()).Line,
			src:  left + " " + node.Op.String() + " " + right,
		})
	}
	return v
}

func isMagnitudeComparison(op token.Token) bool {
	switch op {
	case token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

// severityTypedNames returns the identifier names in a receiver/signature
// whose declared type is Severity or pkg.Severity.
func severityTypedNames(recv *ast.FieldList, sig *ast.FuncType) map[string]bool {
	names := map[string]bool{}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			if !isSeverityTypeExpr(field.Type) {
				continue
			}
			for _, id := range field.Names {
				names[id.Name] = true
			}
		}
	}
	add(recv)
	if sig != nil {
		add(sig.Params)
		add(sig.Results)
	}
	return names
}

func isSeverityTypeExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Severity"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Severity"
	case *ast.StarExpr:
		return isSeverityTypeExpr(t.X)
	}
	return false
}

// severityFlavoured reports whether an operand is (or clearly derives from) a
// Severity value. go/types resolves local declarations and shadowing; sevNames
// preserves the existing signature fallback for partially checked files.
func severityFlavoured(expr ast.Expr, src string, sevNames map[string]bool, info *types.Info) bool {
	if strings.Contains(src, "Severity") || isSeverityGoType(info.TypeOf(expr)) {
		return true
	}
	id, ok := expr.(*ast.Ident)
	return ok && sevNames[id.Name]
}

func isSeverityGoType(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		t = types.Unalias(pointer.Elem())
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "Severity"
}

func severityTypeInfo(packagePath string, fset *token.FileSet, files []*ast.File) *types.Info {
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
	config := types.Config{
		Importer: &severityImporter{fallback: importer.Default(), packages: make(map[string]*types.Package)},
		Error:    func(error) {},
	}
	_, _ = config.Check(packagePath, fset, files, info)
	return info
}

type severityImporter struct {
	fallback types.Importer
	packages map[string]*types.Package
}

func (i *severityImporter) Import(importPath string) (*types.Package, error) {
	if pkg, ok := i.packages[importPath]; ok {
		return pkg, nil
	}
	if pkg, err := i.fallback.Import(importPath); err == nil {
		i.packages[importPath] = pkg
		return pkg, nil
	}

	pkg := types.NewPackage(importPath, pathpkg.Base(importPath))
	severity := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Severity", nil), types.Typ[types.Int], nil)
	pkg.Scope().Insert(severity.Obj())
	for value, name := range []string{"SeverityError", "SeverityWarning", "SeverityInfo"} {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, name, severity, constant.MakeInt64(int64(value))))
	}
	diagnosticFields := []*types.Var{types.NewField(token.NoPos, pkg, "Severity", severity, false)}
	diagnostic := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "Diagnostic", nil),
		types.NewStruct(diagnosticFields, nil),
		nil,
	)
	pkg.Scope().Insert(diagnostic.Obj())
	pkg.MarkComplete()
	i.packages[importPath] = pkg
	return pkg, nil
}

func mentionsSeverityError(src string) bool {
	return strings.Contains(src, "SeverityError")
}

// isNamedThreshold reports whether the operand is written literally as the
// Warning or Info constant, optionally package-qualified.
func isNamedThreshold(src string) bool {
	src = strings.TrimSpace(src)
	if idx := strings.LastIndex(src, "."); idx >= 0 {
		src = src[idx+1:]
	}
	return src == "SeverityWarning" || src == "SeverityInfo"
}

func renderExpr(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return "<unprintable>"
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// TestSeverityASTGuardCatchesEveryKnownBypass feeds the strengthened scan the
// same reintroductions that defeat the regex guard. Each must be caught.
func TestSeverityASTGuardCatchesEveryKnownBypass(t *testing.T) {
	shouldFlag := map[string]string{
		"canonical single line": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return d.Severity >= diag.SeverityError }
`,
		"wrapped across two lines": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool {
	return d.Severity >=
		diag.SeverityError
}
`,
		"named constant alias": `
package p
import "d/diag"
const errThreshold = diag.SeverityError
func f(d diag.Diagnostic) bool { return d.Severity >= errThreshold }
`,
		"variable threshold": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool {
	threshold := diag.SeverityError
	return d.Severity < threshold
}
`,
		"helper on two Severity params": `
package p
import "d/diag"
func atLeast(a, b diag.Severity) bool { return a >= b }
`,
		"explicitly typed local Severity": `
package p
import "d/diag"
func f() bool {
	var severity diag.Severity
	return severity >= 0
}
`,
		"inferred local Severity": `
package p
import "d/diag"
func f() bool {
	severity := diag.SeverityWarning
	return severity >= 0
}
`,
		"same-named free function is not the Severity method": `
package p
import "d/diag"
func AtLeastAsSevereAs(a, b diag.Severity) bool { return a <= b }
`,
		"same-named method on another receiver is not the Severity method": `
package p
import "d/diag"
type comparator struct{}
func (comparator) AtLeastAsSevereAs(a, b diag.Severity) bool { return a <= b }
`,
		"function after the exempt method is still checked": `
package p
type Severity int
func (s Severity) AtLeastAsSevereAs(threshold Severity) bool { return s <= threshold }
func broken(a, b Severity) bool { return a >= b }
`,
		"numeric literal zero": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return d.Severity >= 0 }
`,
		"int conversion": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return int(d.Severity) >= int(diag.SeverityError) }
`,
		"comparison after a string containing a double slash": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool {
	_ = "see https://wiki/severity"
	return d.Severity >= diag.SeverityError
}
`,
		"sort comparator on the ordinal": `
package p
import (
	"sort"
	"d/diag"
)
func f(ds []diag.Diagnostic) {
	sort.Slice(ds, func(i, j int) bool { return ds[i].Severity > ds[j].Severity })
}
`,
		"running worst-of": `
package p
import "d/diag"
func f(ds []diag.Diagnostic) diag.Severity {
	var worst diag.Severity
	for _, d := range ds {
		if d.Severity > worst {
			worst = d.Severity
		}
	}
	return worst
}
`,
		"switch with a numeric range": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool {
	switch {
	case d.Severity <= 0:
		return true
	}
	return false
}
`,
	}

	for name, src := range shouldFlag {
		t.Run(name, func(t *testing.T) {
			if hits := offendersInSource(t, src); len(hits) == 0 {
				t.Fatalf("AST guard missed a reintroduction of the zero-value severity bug:\n%s", src)
			}
		})
	}
}

func TestSeverityASTGuardTypeChecksSiblingProductionFilesTogether(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"severity.go": `package p

type Severity int

const SeverityWarning Severity = 1
`,
		"comparison.go": `package p

func broken() bool {
	severity := SeverityWarning
	return severity >= 0
}
`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	hits := scanSeverityOrdinalComparisons(t, root)
	if len(hits) != 1 || !strings.Contains(hits[0], "comparison.go:5: severity >= 0") {
		t.Fatalf("package-wide AST guard hits = %v, want sibling-typed local comparison", hits)
	}
}

// TestSeverityASTGuardAllowsLegitimateShapes pins the false-positive budget.
func TestSeverityASTGuardAllowsLegitimateShapes(t *testing.T) {
	shouldNotFlag := map[string]string{
		"drop info via the named warning boundary": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return d.Severity > diag.SeverityWarning }
`,
		"at least a warning via the named boundary": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return d.Severity <= diag.SeverityWarning }
`,
		"equality against SeverityError is exact and correct": `
package p
import "d/diag"
func f(d diag.Diagnostic) bool { return d.Severity == diag.SeverityError }
`,
		"the predicate itself is exempt": `
package p
type Severity int
func (s Severity) AtLeastAsSevereAs(threshold Severity) bool { return s <= threshold }
`,
		"block comment prose about the footgun": `
package p
import "d/diag"

/*
d.Severity >= diag.SeverityError is true for EVERY severity.
*/
func f(d diag.Diagnostic) bool { return d.IsError() }
`,
		"unrelated int comparison in the same function": `
package p
import "d/diag"
func f(ds []diag.Diagnostic, i int) bool { return i < len(ds) && ds[i].Severity == diag.SeverityError }
`,
	}

	for name, src := range shouldNotFlag {
		t.Run(name, func(t *testing.T) {
			if hits := offendersInSource(t, src); len(hits) > 0 {
				t.Fatalf("AST guard false positive %v on:\n%s", hits, src)
			}
		})
	}
}

func offendersInSource(t *testing.T, src string) []severityHit {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	info := severityTypeInfo("p", fset, []*ast.File{file})
	return severityOffendersInFile(fset, file, info)
}
