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
