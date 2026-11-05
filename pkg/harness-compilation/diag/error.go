package diag

import (
	"errors"
	"fmt"
)

// CodedError is an error that carries a stable diagnostic code plus the source
// location the failure was detected at. Consumer UIs (for example the
// agent-marketplace TUI and `status`) can key off Code instead of matching
// free-text messages, which keeps load failures machine-readable even when
// they travel through the error return path instead of a []Diagnostic.
//
// CodedError is fail-closed by construction: the zero Severity is
// SeverityError, so wrapping a failure never downgrades it to a warning.
type CodedError struct {
	Code     string    // Stable code from the codes.md catalog
	Severity Severity  // Defaults to SeverityError
	Source   SourceRef // Where the failure was detected
	Err      error     // Underlying cause; never contains user values
}

// NewCodedError builds a CodedError for the given code and source location.
func NewCodedError(code string, source SourceRef, err error) *CodedError {
	return &CodedError{
		Code:     code,
		Severity: SeverityError,
		Source:   source,
		Err:      err,
	}
}

// Error renders "<file>: <cause> [<code>]", or "<cause> [<code>]" when the
// source file is unknown. The code is always present so callers that only have
// a string can still recognize the failure class.
func (e *CodedError) Error() string {
	if e == nil {
		return ""
	}
	cause := "unknown error"
	if e.Err != nil {
		cause = e.Err.Error()
	}
	if e.Source.File == "" {
		return fmt.Sprintf("%s [%s]", cause, e.Code)
	}
	return fmt.Sprintf("%s: %s [%s]", e.Source.File, cause, e.Code)
}

// Unwrap exposes the underlying cause to errors.Is/errors.As.
func (e *CodedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *CodedError) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// Diagnostic converts the error into a structured Diagnostic for the named
// resource. Severity and Hint are taken from the code catalog when the code is
// documented, so a coded error and a directly emitted diagnostic for the same
// code are indistinguishable to consumers.
func (e *CodedError) Diagnostic(resource string) Diagnostic {
	if e == nil {
		return Diagnostic{}
	}
	d := Diagnostic{
		Code:     e.Code,
		Severity: e.Severity,
		Resource: resource,
		Message:  e.Error(),
		Source:   e.Source,
	}
	if doc, ok := DocForCode(e.Code); ok {
		d.Severity = doc.Severity
		d.Hint = doc.Hint
	}
	return d
}

// AsCodedError reports whether err, or any error it wraps, is a *CodedError.
func AsCodedError(err error) (*CodedError, bool) {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded, true
	}
	return nil, false
}
