// Package jsonutil contains strict JSON helpers used by harness config loaders.
//
// Loaders must not call encoding/json directly on user- or registry-supplied
// bytes: encoding/json accepts duplicate object keys with last-value-wins
// semantics, which can silently change MCP commands, headers, env, disabled
// state, hooks, or existing aggregate configuration. Unmarshal is the single
// entry point that validates first and decodes second, and it reports failures
// as a *diag.CodedError so every source harness surfaces the same stable,
// source-aware diagnostic.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// ErrDuplicateKey identifies a JSON object that contains the same key more
// than once. encoding/json otherwise silently keeps the last value.
var ErrDuplicateKey = errors.New("duplicate JSON object key")

// ErrMaxDepth identifies JSON nested more deeply than maxDepth.
var ErrMaxDepth = errors.New("JSON nesting exceeds the maximum supported depth")

// maxDepth bounds validation recursion. Harness configuration files nest only a
// handful of levels; encoding/json's own scanner stops at 10000, so this is
// explicit insurance against unbounded recursion on hostile input rather than a
// limit any real configuration can reach.
const maxDepth = 1000

// DuplicateKeyError reports a duplicate object key together with the JSON path
// it was found at and the byte offset just past the key, used to derive a
// source line. It deliberately never carries the duplicated value, so
// credential-like values in MCP env/headers maps are never echoed into
// diagnostics or logs.
type DuplicateKeyError struct {
	Key    string // The duplicated object key
	Path   string // JSON path of the containing object, e.g. $.mcpServers.docs.env
	Offset int64  // Byte offset just past the offending key
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("%s %q at %s", ErrDuplicateKey.Error(), e.Key, e.Path)
}

// Is makes errors.Is(err, ErrDuplicateKey) work for callers that only care
// about the failure class.
func (e *DuplicateKeyError) Is(target error) bool { return target == ErrDuplicateKey }

// Unmarshal validates that data is well-formed JSON with unique object keys at
// every depth and then decodes it into v. On failure it returns a
// *diag.CodedError carrying a stable diagnostic code (diag.CodeJSONDuplicateKey
// or diag.CodeJSONMalformed), the source file, and the 1-based line of the
// offending key. file may be empty when the caller only has bytes; the caller
// is then responsible for adding path context.
func Unmarshal(file string, data []byte, v any) error {
	if err := Validate(file, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return diag.NewCodedError(diag.CodeJSONMalformed, diag.SourceRef{File: file, Line: 1}, err)
	}
	return nil
}

// Validate reports whether data is well-formed JSON with unique object keys at
// every depth, returning a *diag.CodedError on failure. Use Unmarshal unless
// the bytes are decoded by something other than encoding/json.
func Validate(file string, data []byte) error {
	err := ValidateUniqueKeys(data)
	if err == nil {
		return nil
	}
	return Coded(file, data, err)
}

// Coded wraps a JSON parse failure in the stable coded diagnostic shape shared
// by every JSON entry point: json.duplicate-key when err is (or wraps) a
// *DuplicateKeyError, json.malformed otherwise, with the 1-based source line
// derived from the recorded byte offset. It exists so the concrete-syntax
// JSONC parser in pkg/harness-compilation/jsonc reports duplicate keys
// identically to this package instead of inventing a second diagnostic shape.
// err must never carry a user value; DuplicateKeyError deliberately does not.
func Coded(file string, data []byte, err error) error {
	if err == nil {
		return nil
	}
	code := diag.CodeJSONMalformed
	line := 1
	var duplicate *DuplicateKeyError
	if errors.As(err, &duplicate) {
		code = diag.CodeJSONDuplicateKey
		line = lineForOffset(data, duplicate.Offset)
	}
	return diag.NewCodedError(code, diag.SourceRef{File: file, Line: line}, err)
}

// ValidateUniqueKeys validates JSON syntax and rejects duplicate keys at every
// object depth. It intentionally does not decode into maps, because doing so
// would lose the duplicate-key information before it can be checked.
func ValidateUniqueKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := validateValue(decoder, "$", 0); err != nil {
		return err
	}
	return expectEOF(decoder)
}

// ObjectWithUniqueKeys reports whether data is a single well-formed JSON object.
// Malformed JSON, arrays, scalars, and empty input report false with no error so
// callers can classify them as non-object input. A JSON object with duplicate
// keys reports true together with the duplicate-key error, so the caller can
// fail closed instead of decoding it.
//
// This exists so callers that need both answers pay for a single token pass
// instead of a full decode plus a separate validation pass.
func ObjectWithUniqueKeys(data []byte) (bool, error) {
	// Cheap reject first: anything that does not start with '{' cannot be an
	// object, and text fragments must not pay for a decoder allocation.
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return false, nil
	}
	if token != json.Delim('{') {
		return false, nil
	}
	if err := validateObject(decoder, "$", 1); err != nil {
		if errors.Is(err, ErrDuplicateKey) || errors.Is(err, ErrMaxDepth) {
			return true, Coded("", data, err)
		}
		return false, nil
	}
	if err := expectEOF(decoder); err != nil {
		return false, nil
	}
	return true, nil
}

func expectEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value after document")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func validateValue(decoder *json.Decoder, path string, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	if depth >= maxDepth {
		return fmt.Errorf("%w at %s", ErrMaxDepth, path)
	}
	switch delim {
	case '{':
		return validateObject(decoder, path, depth+1)
	case '[':
		return validateArray(decoder, path, depth+1)
	default:
		return fmt.Errorf("%s: unexpected delimiter %q", path, delim)
	}
}

// validateObject validates an object whose opening brace has already been
// consumed from decoder.
func validateObject(decoder *json.Decoder, path string, depth int) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("%s: object key is not a string", path)
		}
		if _, exists := seen[key]; exists {
			return &DuplicateKeyError{Key: key, Path: path, Offset: decoder.InputOffset()}
		}
		seen[key] = struct{}{}
		if err := validateValue(decoder, path+"."+key, depth); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != json.Delim('}') {
		return fmt.Errorf("%s: expected object close, got %v", path, closing)
	}
	return nil
}

// validateArray validates an array whose opening bracket has already been
// consumed from decoder.
func validateArray(decoder *json.Decoder, path string, depth int) error {
	for index := 0; decoder.More(); index++ {
		if err := validateValue(decoder, fmt.Sprintf("%s[%d]", path, index), depth); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != json.Delim(']') {
		return fmt.Errorf("%s: expected array close, got %v", path, closing)
	}
	return nil
}

// lineForOffset converts a byte offset into a 1-based line number.
func lineForOffset(data []byte, offset int64) int {
	if offset <= 0 || offset > int64(len(data)) {
		return 1
	}
	return 1 + bytes.Count(data[:offset], []byte{'\n'})
}
