package codemode

import "fmt"

const (
	ErrCodeTimeout          = "codemode.timeout"
	ErrCodeOutputLimit      = "codemode.output-limit"
	ErrCodeHostCallLimit    = "codemode.host-call-limit"
	ErrCodeScriptError      = "codemode.script-error"
	ErrCodeRevoked          = "resource.revoked"
	ErrCodeRejected         = "resource.rejected"
	ErrCodeNotFound         = "resource.not-found"
	ErrCodeUnsupportedKind  = "resource.unsupported-kind"
	ErrCodeNotDeclared      = "mcp.not-declared"
	ErrCodeStartFailed      = "mcp.start-failed"
	ErrCodeToolError        = "mcp.tool-error"
	ErrCodeCatalogStale     = "catalog.stale"
	ErrCodeInvalidArguments = "codemode.invalid-arguments"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func CodeError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
