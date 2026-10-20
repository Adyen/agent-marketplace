package lockfile

import "fmt"

type ErrorCode string

const (
	CodeMalformedYAML      ErrorCode = "lock.malformed"
	CodeUnknownField       ErrorCode = "lock.unknown-field"
	CodeDuplicateField     ErrorCode = "lock.duplicate-field"
	CodeYAMLAlias          ErrorCode = "lock.alias-forbidden"
	CodeYAMLMerge          ErrorCode = "lock.merge-forbidden"
	CodeMultipleDocuments  ErrorCode = "lock.multiple-documents"
	CodeConflictMarkers    ErrorCode = "lock.conflicted"
	CodeUnsupportedVersion ErrorCode = "lock.unsupported-version"
	CodeInvalidScope       ErrorCode = "lock.invalid-scope"
	CodeScopeMismatch      ErrorCode = "lock.scope-mismatch"
	CodeInvalidDocument    ErrorCode = "lock.invalid-document"
	CodeIdentityConflict   ErrorCode = "lock.identity-conflict"
	CodeInvalidGitOID      ErrorCode = "lock.git-oid-invalid"
	CodeUnsupportedGitOID  ErrorCode = "lock.git-oid-unsupported"
	CodeInvalidDigest      ErrorCode = "lock.digest-invalid"
	CodeInvalidGitURL      ErrorCode = "lock.git-url-invalid"
	CodeInvalidPath        ErrorCode = "lock.path-invalid"
)

type Error struct {
	Code ErrorCode
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }
func (e *Error) DiagnosticCode() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

func coded(code ErrorCode, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}
