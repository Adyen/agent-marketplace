package ingest

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// LoadFailure builds the error diagnostic an ingester must emit when loading a
// single resource fails. Every source harness routes through this helper so a
// consumer sees the same shape regardless of which layout produced the failure:
// a stable code, SeverityError, the resource identity, and a SourceRef pointing
// at the offending file.
//
// When err carries a stable JSON code (for example a duplicate object key), that
// code and its source location win over the per-harness load-failed code, so a
// duplicate-key failure is keyed identically for Claude, Cursor, Factory,
// Gemini, and Windsurf sources.
//
// kind is the human-readable resource kind used in the message ("skill",
// "command", "MCP servers", ...). resource is the reported resource identity,
// which may be normalized per harness; sourceFile is the path as it appears in
// the source layout.
func LoadFailure(code, kind, resource, sourceFile string, err error) diag.Diagnostic {
	d := diag.Diagnostic{
		Code:     code,
		Severity: diag.SeverityError,
		Resource: resource,
		Message:  fmt.Sprintf("failed to load %s %q: %v", kind, sourceFile, err),
		Source:   diag.SourceRef{File: sourceFile, Line: 1},
	}
	if coded, ok := diag.AsCodedError(err); ok {
		d.Code = coded.Code
		if coded.Source.File != "" {
			d.Source = coded.Source
		}
		if doc, found := diag.DocForCode(coded.Code); found {
			d.Hint = doc.Hint
		}
	}
	return d
}

// LoadJSONManifest reads path from src and decodes it into v. It is the single
// entry point for registry-supplied plugin manifests: duplicate object keys are
// rejected before decoding, so a repeated "name", "version", "description", or
// "triggers" cannot silently rewrite ownership markers or output paths.
//
// A missing file is returned unwrapped so callers can test it with
// errors.Is(err, fs.ErrNotExist) and synthesize a manifest instead.
func LoadJSONManifest(src fs.FS, path string, v any) error {
	data, err := fs.ReadFile(src, path)
	if err != nil {
		return err
	}
	return jsonutil.Unmarshal(path, data, v)
}
