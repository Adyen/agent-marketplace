package windsurf

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func LoadWorkflow(src fs.FS, file string) (*ir.Command, []diag.Diagnostic, error) {
	f, err := src.Open(file)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/windsurf: open %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()

	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/windsurf: parse workflow %s: %w", file, err)
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = strings.TrimSuffix(path.Base(file), path.Ext(file))
	}
	description, _ := fm["description"].(string)
	modeValue, hasMode := fm["auto_execution_mode"]
	mode := ""
	if hasMode {
		var valid bool
		mode, valid = modeValue.(string)
		if !valid {
			return nil, nil, fmt.Errorf("ingest/windsurf: cannot preserve non-string auto_execution_mode in %s", file)
		}
	}
	extra := map[string]any{}
	for key, value := range fm {
		if key != "name" && key != "description" && key != "auto_execution_mode" {
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		extra = nil
	}

	command := ir.NewCommand(name, description, body, extra, ir.SourceRef{File: file, Line: 1})
	if hasMode {
		command.Ext.Windsurf = &extpkg.Windsurf{AutoExecutionMode: mode, AutoExecutionModeSet: true}
	}
	diags := []diag.Diagnostic{{
		Code:     diag.CodeIngestWindsurfWorkflowAssumedCommand,
		Severity: diag.SeverityInfo,
		Resource: name,
		Message:  "Windsurf workflow was ingested as a command",
		Hint:     "Windsurf workflows are treated as ir.Command resources during ingestion",
		Source:   diag.SourceRef{File: file, Line: 1},
	}}
	return command, diags, nil
}
