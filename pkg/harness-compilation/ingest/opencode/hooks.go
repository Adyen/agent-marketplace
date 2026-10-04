package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func loadHooks(src fs.FS, root string) ([]ir.Resource, []diag.Diagnostic) {
	var diagnostics []diag.Diagnostic
	recognized := false
	for _, directory := range []string{"plugins", "plugin"} {
		entries, err := fs.ReadDir(src, path.Join(root, directory))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, failure(path.Join(root, directory), err))
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".js") && !strings.HasSuffix(entry.Name(), ".ts")) {
				continue
			}
			file := path.Join(root, directory, entry.Name())
			data, err := fs.ReadFile(boundedFS{src}, file)
			if err == nil && entry.Name() == "agent-marketplace.js" && bytes.Equal(data, opencodehook.Adapter) {
				recognized = true
				continue
			}
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestOpenCodePluginUnsupported, Severity: diag.SeverityError, Source: diag.SourceRef{File: file}, Message: "Executable OpenCode plugin cannot be converted; no source code was loaded"})
		}
	}
	entries, err := fs.ReadDir(src, path.Join(root, "hooks", "agent-marketplace"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, diagnostics
	}
	if err != nil {
		return nil, append(diagnostics, failure(path.Join(root, "hooks", "agent-marketplace"), err))
	}
	var resources []ir.Resource
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		file := path.Join(root, "hooks", "agent-marketplace", entry.Name())
		data, err := fs.ReadFile(boundedFS{src}, file)
		if err != nil {
			diagnostics = append(diagnostics, failure(file, err))
			continue
		}
		var fields map[string]json.RawMessage
		if err := jsonc.Unmarshal(file, data, &fields); err != nil || !json.Valid(data) {
			diagnostics = append(diagnostics, failure(file, err))
			continue
		}
		if matcher, present := fields["matcher"]; present {
			var value any
			if json.Unmarshal(matcher, &value) != nil {
				diagnostics = append(diagnostics, failure(file, fs.ErrInvalid))
				continue
			}
			if _, valid := value.(string); !valid {
				diagnostics = append(diagnostics, failure(file, fs.ErrInvalid))
				continue
			}
		}
		var declaration opencodehook.Declaration
		if strictKeys(fields, []string{"format", "name", "event", "matcher", "command", "timeout"}) != nil || jsonc.Unmarshal(file, data, &declaration) != nil || declaration.Format != opencodehook.Format || declaration.Event != "PreToolUse" || declaration.Command == "" || declaration.Timeout < 1 || declaration.Timeout > 600 || ir.ValidateName(declaration.Name) != nil || !recognized {
			diagnostics = append(diagnostics, failure(file, fs.ErrInvalid))
			continue
		}
		hook := ir.NewHookWithMatcherOptions(declaration.Name, declaration.Event, declaration.Matcher, declaration.Command, "command", nil, ir.SourceRef{File: file, Line: 1}, ir.WithHookTimeout(declaration.Timeout))
		resources = append(resources, hook)
	}
	return resources, diagnostics
}
