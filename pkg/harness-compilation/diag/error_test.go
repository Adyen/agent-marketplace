package diag

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodedErrorMessageIncludesFileAndCode(t *testing.T) {
	cause := errors.New(`duplicate JSON object key "TOKEN" at $.mcpServers.docs.env`)
	err := NewCodedError(CodeJSONDuplicateKey, SourceRef{File: ".mcp.json", Line: 4}, cause)

	want := `.mcp.json: duplicate JSON object key "TOKEN" at $.mcpServers.docs.env [json.duplicate-key]`
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is does not reach the wrapped cause")
	}
	if err.Severity != SeverityError {
		t.Fatalf("severity = %v, want error (must default to fail-closed)", err.Severity)
	}
}

func TestCodedErrorMessageOmitsEmptyFile(t *testing.T) {
	err := NewCodedError(CodeJSONMalformed, SourceRef{}, errors.New("unexpected EOF"))
	want := "unexpected EOF [json.malformed]"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

// spec: command-error-output/every-consumer-diagnostic-code-has-guidance
func TestCodedErrorDiagnosticCode(t *testing.T) {
	err := NewCodedError(CodeOwnershipStateCorrupt, SourceRef{File: "ownership.json"}, errors.New("corrupt"))
	if got := err.DiagnosticCode(); got != CodeOwnershipStateCorrupt {
		t.Fatalf("DiagnosticCode() = %q, want %q", got, CodeOwnershipStateCorrupt)
	}
}

func TestCodedErrorDiagnosticUsesCatalog(t *testing.T) {
	err := NewCodedError(CodeJSONDuplicateKey, SourceRef{File: ".mcp.json", Line: 4}, errors.New("boom"))
	d := err.Diagnostic("docs")

	if d.Code != CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", d.Code, CodeJSONDuplicateKey)
	}
	if d.Severity != SeverityError {
		t.Fatalf("severity = %v, want error", d.Severity)
	}
	if d.Resource != "docs" {
		t.Fatalf("resource = %q, want %q", d.Resource, "docs")
	}
	if d.Source.Line != 4 || d.Source.File != ".mcp.json" {
		t.Fatalf("source = %+v, want {.mcp.json 4}", d.Source)
	}
	doc, ok := DocForCode(CodeJSONDuplicateKey)
	if !ok {
		t.Fatal("json.duplicate-key is not documented in the catalog")
	}
	if d.Hint != doc.Hint {
		t.Fatalf("hint = %q, want %q", d.Hint, doc.Hint)
	}
}

func TestAsCodedErrorFindsWrappedError(t *testing.T) {
	inner := NewCodedError(CodeJSONDuplicateKey, SourceRef{File: "hooks.json", Line: 2}, errors.New("boom"))
	wrapped := fmt.Errorf("ingest/windsurf: parse hook: %w", inner)

	coded, ok := AsCodedError(wrapped)
	if !ok {
		t.Fatal("AsCodedError did not find the wrapped CodedError")
	}
	if coded.Code != CodeJSONDuplicateKey || coded.Source.File != "hooks.json" {
		t.Fatalf("coded = %+v, want the inner error", coded)
	}

	if _, ok := AsCodedError(errors.New("plain")); ok {
		t.Fatal("AsCodedError matched a plain error")
	}
}
