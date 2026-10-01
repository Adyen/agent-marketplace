package factory

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadHooks reads a Factory hooks.json file and canonicalizes Factory env
// tokens back to the Claude IR token vocabulary.
func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []diag.Diagnostic, error) {
	hooks, hookDiags, err := bundle.LoadHooks(src, path)
	if err != nil {
		return nil, nil, err
	}

	for _, hook := range hooks {
		hook.Command = strings.ReplaceAll(hook.Command, "${DROID_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}")
		// Record Factory provenance so the catalog resolver keeps native
		// matcher spellings and event names on same-harness output
		// (OpenSpec native-hook-integration, task 3.7).
		hook.HookSource = string(mapping.TargetFactory)
	}

	diags := make([]diag.Diagnostic, 0, len(hookDiags))
	for _, diagnostic := range hookDiags {
		diags = append(diags, *diagnostic)
	}
	return hooks, diags, nil
}
