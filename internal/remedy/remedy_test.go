package remedy

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

type testCodedError struct {
	code     string
	scope    string
	variant  string
	file     string
	lockFile string
	locator  string
	line     int
	path     string
	text     string
}

func (e testCodedError) Error() string          { return e.text }
func (e testCodedError) DiagnosticCode() string { return e.code }
func (e testCodedError) DiagnosticScope() string {
	return e.scope
}
func (e testCodedError) DiagnosticVariant() string  { return e.variant }
func (e testCodedError) DiagnosticFile() string     { return e.file }
func (e testCodedError) DiagnosticLine() int        { return e.line }
func (e testCodedError) DiagnosticLockFile() string { return e.lockFile }
func (e testCodedError) DiagnosticLocator() string  { return e.locator }
func (e testCodedError) DiagnosticPath() string     { return e.path }

type testRemediableError struct {
	testCodedError
	hint string
}

func (e testRemediableError) Remediation() string { return e.hint }

func TestForLookupOrder(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", testRemediableError{
		testCodedError: testCodedError{code: "lock.stale", text: "stale"},
		hint:           "Use the custom repair.",
	})
	if got := For(err, "sync"); got != "Use the custom repair." {
		t.Fatalf("For() = %q, want remediation before catalog", got)
	}

	if got := For(testCodedError{code: "lock.stale", text: "stale"}, "sync"); !strings.Contains(got, "agent-marketplace lock") ||
		strings.Contains(got, "--scope repo") {
		t.Fatalf("For() = %q, want code-specific guidance without a guessed lock scope", got)
	}
	wantRecoveryHint := "Run `agent-marketplace sync` or `agent-marketplace lock` while online (not with `--check`, `--locked`, or `--offline`) to finish the interrupted write."
	if got := For(testCodedError{code: "dependency.recovery-required", text: "interrupted write"}, "sync"); got != wantRecoveryHint {
		t.Fatalf("For() = %q, want %q", got, wantRecoveryHint)
	}
	if got := For(errors.New("unclassified"), "sync"); !strings.Contains(got, "agent-marketplace sync --check") {
		t.Fatalf("For() = %q, want command-aware fallback", got)
	}
	if got := For(errors.New("unclassified"), ""); got != "" {
		t.Fatalf("For() without a command = %q, want no fallback for TUI lookup", got)
	}
}

func TestForFindsFirstNonemptyRemediationInJoinedErrors(t *testing.T) {
	err := errors.Join(
		testRemediableError{testCodedError: testCodedError{code: "lock.stale", text: "stale"}, hint: ""},
		testRemediableError{testCodedError: testCodedError{code: "lock.missing", text: "missing"}, hint: "Use the second repair."},
	)
	if got := For(err, "sync"); got != "Use the second repair." {
		t.Fatalf("For() = %q, want first nonempty remediation from the error chain", got)
	}
}

func TestForUsesCodedDiagnosticFromHarnessLibrary(t *testing.T) {
	err := diag.NewCodedError(diag.CodeOwnershipStateCorrupt, diag.SourceRef{File: "ownership.json"}, errors.New("corrupt"))
	if hint := For(err, "sync"); !strings.Contains(hint, "agent-marketplace doctor --repair") {
		t.Fatalf("For() = %q, want ownership-specific guidance", hint)
	}
}

func TestOrganizationScopedHintsDirectUsersToTheirAdministrator(t *testing.T) {
	for _, code := range []string{
		"lock.stale",
		"lock.snapshot-unavailable",
		"organization.trust.lock.read",
		"organization.trust.include.missing",
		"v4.api-version-required",
		"v4.marketplace-lock-mismatch",
		"v4.source-name",
	} {
		t.Run(code, func(t *testing.T) {
			hint := For(testCodedError{code: code, scope: "organization", text: "organization failure"}, "sync")
			lower := strings.ToLower(hint)
			if !strings.Contains(lower, "ask your administrator") ||
				strings.Contains(lower, "agent-marketplace lock --scope") ||
				strings.Contains(lower, "agent-marketplace migrate") {
				t.Fatalf("%s organization hint is not administrator-owned: %q", code, hint)
			}
		})
	}
}

func TestCatalogHintsAreSurfaceNeutral(t *testing.T) {
	for code, entry := range catalog {
		t.Run(code, func(t *testing.T) {
			hint := strings.ToLower(entry(hintContext{Scope: "repo"}))
			for _, uiCopy := range []string{"press `r`", "resource list", "tui"} {
				if strings.Contains(hint, uiCopy) {
					t.Fatalf("%s catalog hint contains TUI-only guidance %q: %s", code, uiCopy, hint)
				}
			}
		})
	}
}

func TestUnknownLockScopeDoesNotGuessRepository(t *testing.T) {
	for _, code := range []string{"lock.missing", "lock.stale", "lock.change-required", "lock.scope-mismatch"} {
		t.Run(code, func(t *testing.T) {
			hint := For(testCodedError{code: code, text: "lock failure"}, "lock")
			if strings.Contains(hint, "--scope repo") {
				t.Fatalf("%s guessed a repository scope: %q", code, hint)
			}
		})
	}
}

// spec: command-error-output/coded-errors-print-code-specific-guidance
func TestLockRestoreHintPlacesPathBeforeGuidance(t *testing.T) {
	got := For(testCodedError{
		code: "lock.invalid", path: "/workspace/.agents/lock.yaml", text: "invalid lock",
	}, "doctor")
	want := strings.TrimSuffix(restoreLockGuidance, ".") + " (lock file: /workspace/.agents/lock.yaml)."
	if got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

// spec: command-error-output/coded-errors-print-code-specific-guidance
func TestMarketplaceLockMismatchVariantsUseStructuredGuidance(t *testing.T) {
	file := "/workspace/.agents/config.yaml"
	cases := []struct {
		name, variant, scope, lockFile, want string
	}{
		{
			name: "repository resource", variant: "resource-missing", scope: "repo", lockFile: ".agents/lock.yaml",
			want: "Run `agent-marketplace sync` while online (not with `--check`, `--locked`, or `--offline`) to add \"awesome-ai/collection/coderabbit-skills\" to .agents/lock.yaml, then commit .agents/lock.yaml.",
		},
		{
			name: "user resource", variant: "resource-missing", scope: "user", lockFile: "/home/alex/.agents/lock.yaml",
			want: "Run `agent-marketplace sync` while online (not with `--check`, `--locked`, or `--offline`) to add \"awesome-ai/collection/coderabbit-skills\" to /home/alex/.agents/lock.yaml.",
		},
		{
			name: "personal-local resource", variant: "resource-missing", scope: "local", lockFile: ".agents/lock.local.yaml",
			want: "Run `agent-marketplace sync` while online (not with `--check`, `--locked`, or `--offline`) to add \"awesome-ai/collection/coderabbit-skills\" to .agents/lock.local.yaml.",
		},
		{
			name: "duplicate request", variant: "duplicate-request", scope: "repo", lockFile: ".agents/lock.yaml",
			want: "In " + file + ":7, change the Marketplace requests so each selects a distinct locked resource, then run `agent-marketplace doctor` to check the fix.",
		},
		{
			name: "stale endpoint", variant: "stale-endpoint", scope: "repo", lockFile: ".agents/lock.yaml",
			want: "In .agents/lock.yaml, remove the stale Marketplace endpoint or add its matching Marketplace source, then run `agent-marketplace doctor` to check the fix.",
		},
		{
			name: "unreadable lock", variant: "lock-read", scope: "repo", lockFile: ".agents/lock.yaml",
			want: "Restore the acquisition lock file .agents/lock.yaml from a known-good copy or correct its permissions, then run `agent-marketplace sync --check` to verify it.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hint := For(testCodedError{
				code: "v4.marketplace-lock-mismatch", scope: tc.scope, variant: tc.variant,
				file: file, line: 7, lockFile: tc.lockFile, locator: "awesome-ai/collection/coderabbit-skills",
				text: "lock mismatch",
			}, "doctor")
			if hint != tc.want {
				t.Fatalf("hint = %q, want %q", hint, tc.want)
			}
		})
	}
}

// spec: command-error-output/coded-errors-print-code-specific-guidance
func TestLowerFirstPreservesUTF8(t *testing.T) {
	if got := lowerFirst("Ändern"); got != "ändern" {
		t.Fatalf("lowerFirst() = %q, want %q", got, "ändern")
	}
}

func TestConfigurationHintsNameLocationAndVerification(t *testing.T) {
	for _, code := range []string{"v4.source-name", "config.edit.invalid-binding"} {
		t.Run(code, func(t *testing.T) {
			hint := For(testCodedError{
				code: code, scope: "repo", file: "/workspace/.agents/config.yaml", line: 7, text: "invalid configuration",
			}, "doctor")
			if !strings.HasPrefix(hint, "In /workspace/.agents/config.yaml:7,") ||
				!strings.HasSuffix(hint, "then run `agent-marketplace doctor` to check the fix.") {
				t.Fatalf("configuration hint lacks authored location or verification step: %q", hint)
			}
		})
	}
}

func TestCatalogGuidanceIsActionableAndSafe(t *testing.T) {
	if len(catalog) == 0 {
		t.Fatal("diagnostic catalog is empty")
	}
	for code, entry := range catalog {
		t.Run(code, func(t *testing.T) {
			hint := entry(hintContext{
				File:    "/workspace/.agents/config.yaml",
				Line:    7,
				Scope:   "repo",
				Path:    "/workspace/.agents/lock.yaml",
				Locator: "tools/skill/review",
			})
			if strings.TrimSpace(hint) == "" {
				t.Fatalf("%s has empty guidance", code)
			}
			if !actionableGuidance(hint) {
				t.Fatalf("%s guidance does not name a supported command or action: %q", code, hint)
			}
			lower := strings.ToLower(hint)
			for _, unsafe := range []struct{ phrase, prohibition string }{
				{"delete the lock", "do not delete the lock"},
				{"delete your lock", "do not delete your lock"},
				{"update to bypass", "do not run `update` to bypass"},
				{"edit organization", "do not edit organization"},
				{"edit the organization", "do not edit the organization"},
			} {
				if strings.Contains(lower, unsafe.phrase) && !strings.Contains(lower, unsafe.prohibition) {
					t.Fatalf("%s guidance suggests unsafe action %q: %q", code, unsafe.phrase, hint)
				}
			}
		})
	}
}

func actionableGuidance(hint string) bool {
	lower := strings.ToLower(strings.TrimSpace(hint))
	if strings.Contains(lower, "agent-marketplace") || strings.HasPrefix(lower, "ask your administrator") {
		return true
	}
	for _, verb := range []string{
		"add ", "change ", "check ", "choose ", "connect ", "convert ", "correct ", "declare ",
		"edit ", "fix ", "keep ", "make ", "move ", "place ", "remove ", "replace ",
		"rename ", "resolve ", "restore ", "retry ", "rerun ", "run ", "specify ", "set ",
		"use ", "wait ", "inspect ",
	} {
		if strings.HasPrefix(lower, verb) {
			return true
		}
	}
	return strings.HasPrefix(lower, "no action is needed")
}

// spec: command-error-output/every-consumer-diagnostic-code-has-guidance
func TestConsumerDiagnosticCodesHaveCatalogEntries(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd", "agent-marketplace"),
		filepath.Join(root, "pkg", "harness-compilation", "diag"),
	}
	materializeRoot := filepath.Join(root, "internal", "materialize")
	diagRoot := filepath.Join(root, "pkg", "harness-compilation", "diag")
	var materializeFiles, diagFiles []*ast.File
	codes := make(map[string]struct{})
	for _, scanRoot := range roots {
		err := filepath.WalkDir(scanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			if strings.HasPrefix(path, materializeRoot+string(filepath.Separator)) {
				materializeFiles = append(materializeFiles, file)
			}
			if strings.HasPrefix(path, diagRoot+string(filepath.Separator)) {
				diagFiles = append(diagFiles, file)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil && consumerCodePattern.MatchString(value) && !nonCodeConsumerLiteral[value] {
					codes[value] = struct{}{}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var missing []string
	for code := range codes {
		if _, ok := catalog[code]; !ok {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("consumer diagnostic codes lack catalog entries: %s", strings.Join(missing, ", "))
	}
	constants := constStringValues(diagFiles, "Code")
	for name, value := range constStringValues(materializeFiles, "") {
		constants[name] = value
	}
	ownedCodes, err := collectOwnedAggregateDiagnosticCodes(materializeFiles, constants)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireOwnedAggregateGuidance(ownedCodes, func(code string) bool {
		doc, documented := diag.DocForCode(code)
		return documented && strings.TrimSpace(doc.Hint) != "" || catalog[code] != nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedAggregateGuidanceGuardRejectsMissingGuidance(t *testing.T) {
	if err := requireOwnedAggregateGuidance([]string{"materialize.unlisted"}, func(string) bool { return false }); err == nil || !strings.Contains(err.Error(), "materialize.unlisted") {
		t.Fatalf("guidance guard error = %v, want an uncovered-code failure", err)
	}
}

func constStringValues(files []*ast.File, prefix string) map[string]string {
	constants := make(map[string]string)
	for _, file := range files {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, spec := range group.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range values.Names {
					if !strings.HasPrefix(name.Name, prefix) || i >= len(values.Values) {
						continue
					}
					if literal, ok := values.Values[i].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						constants[name.Name], _ = strconv.Unquote(literal.Value)
					}
				}
			}
		}
	}
	return constants
}

func collectOwnedAggregateDiagnosticCodes(files []*ast.File, constants map[string]string) ([]string, error) {
	optionFields := make(map[string][]string)
	var codes []string
	add := func(expression ast.Expr) error {
		resolved, err := resolveOwnedAggregateCode(expression, constants, optionFields)
		if err != nil {
			return err
		}
		codes = append(codes, resolved...)
		return nil
	}
	for _, file := range files {
		var scanErr error
		ast.Inspect(file, func(node ast.Node) bool {
			if scanErr != nil {
				return false
			}
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typeName := ""
			switch typ := literal.Type.(type) {
			case *ast.Ident:
				typeName = typ.Name
			case *ast.SelectorExpr:
				typeName = typ.Sel.Name
			}
			if !strings.HasSuffix(typeName, "Options") {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if ok && strings.HasSuffix(name.Name, "Code") {
					values, err := resolveOwnedAggregateCode(field.Value, constants, nil)
					if err != nil {
						scanErr = err
						return false
					}
					optionFields[name.Name] = append(optionFields[name.Name], values...)
					codes = append(codes, values...)
				}
			}
			return true
		})
		if scanErr != nil {
			return nil, scanErr
		}
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			var scanErr error
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if scanErr != nil {
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch function := call.Fun.(type) {
				case *ast.Ident:
					name = function.Name
				case *ast.SelectorExpr:
					name = function.Sel.Name
				}
				index := -1
				switch {
				case name == "ownedAggregateDiag" && function.Name.Name != "ownedJSONShapeDiag" &&
					function.Name.Name != "ownedAggregateCodedDiag" && function.Name.Name != "codexRepairFileDiag":
					index = 0
				case name == "ownedAggregateCodedDiag" && function.Name.Name != "ownedAggregateLockSessionDiag",
					name == "ownedAggregateLockSessionDiag":
					index = len(call.Args) - 1
				case name == "ownedJSONShapeDiag":
					index = 0
				case name == "codexRepairFileDiag":
					index = 3
				}
				if index >= 0 && index < len(call.Args) {
					scanErr = add(call.Args[index])
				} else if index >= 0 {
					scanErr = fmt.Errorf("owned-aggregate diagnostic call has no code argument")
				}
				return true
			})
			if scanErr != nil {
				return nil, scanErr
			}
		}
	}
	return codes, nil
}

func resolveOwnedAggregateCode(expression ast.Expr, constants map[string]string, optionFields map[string][]string) ([]string, error) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return nil, fmt.Errorf("owned-aggregate code is not a string literal: %s", value.Value)
		}
		code, err := strconv.Unquote(value.Value)
		if err != nil {
			return nil, err
		}
		return []string{code}, nil
	case *ast.ParenExpr:
		return resolveOwnedAggregateCode(value.X, constants, optionFields)
	case *ast.Ident:
		if code, ok := constants[value.Name]; ok {
			return []string{code}, nil
		}
	case *ast.SelectorExpr:
		if base, ok := value.X.(*ast.Ident); ok && base.Name == "diag" {
			if code, ok := constants[value.Sel.Name]; ok {
				return []string{code}, nil
			}
		}
		if fields := optionFields[value.Sel.Name]; len(fields) > 0 {
			return fields, nil
		}
	}
	return nil, fmt.Errorf("cannot resolve owned-aggregate code expression %T", expression)
}

func requireOwnedAggregateGuidance(codes []string, hasGuidance func(string) bool) error {
	var missing []string
	for _, code := range codes {
		if !hasGuidance(code) {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("owned-aggregate diagnostic codes lack guidance: %s", strings.Join(missing, ", "))
	}
	return nil
}

var consumerCodePattern = regexp.MustCompile(`^(v4|lock|dependency|ownership|organization\.trust|config\.edit|source|git|marketplace\.(install|remove))\.[a-z0-9][a-z0-9.-]*$`)

var nonCodeConsumerLiteral = map[string]bool{
	"lock.local.yaml": true,
	"lock.yaml":       true,
	"source.kind":     true,
	"source.local":    true,
	"source.org":      true,
	"source.repo":     true,
	"source.user":     true,
}
