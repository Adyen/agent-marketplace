package testutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// FakeSecret is a recognizable stand-in for a credential-like MCP value. Tests
// use it as the duplicated value and assert it never appears in a diagnostic, so
// duplicate-key reporting cannot leak env or auth-header values.
//
//nolint:gosec G101 // deliberate non-credential placeholder used for duplicate-key tests
const FakeSecret = "NOT-A-REAL-SECRET-c0ffee"

// AssertDuplicateKeyDiagnostic asserts the duplicate-key contract that every
// JSON loader and the merge-with-existing path must satisfy:
//
//   - the failure is fail-closed and matches jsonutil.ErrDuplicateKey,
//   - it carries the stable diag.CodeJSONDuplicateKey code at SeverityError,
//   - the message names the source file, the JSON key path of the offending
//     object, and the duplicated key,
//   - the message never contains the duplicated value.
//
// file may be empty for layers that only see bytes; the file assertions are then
// skipped and the caller checks the path context it adds itself.
func AssertDuplicateKeyDiagnostic(t *testing.T, err error, file, keyPath, key string) {
	t.Helper()
	if err == nil {
		t.Fatal("duplicate JSON object key was accepted")
	}
	if !errors.Is(err, jsonutil.ErrDuplicateKey) {
		t.Fatalf("error = %v, want duplicate-key error", err)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T does not carry a stable diagnostic code", err)
	}
	if coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONDuplicateKey)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error (duplicate keys must stay fail-closed)", coded.Severity)
	}

	message := err.Error()
	wants := []string{keyPath, key}
	if file != "" {
		if coded.Source.File != file {
			t.Fatalf("source file = %q, want %q", coded.Source.File, file)
		}
		if coded.Source.Line < 1 {
			t.Fatalf("source line = %d, want a 1-based line number", coded.Source.Line)
		}
		wants = append(wants, file)
	}
	for _, want := range wants {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, FakeSecret) {
		t.Fatalf("error %q leaks the duplicated value", message)
	}
}
