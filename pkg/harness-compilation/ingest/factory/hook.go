package factory

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
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
	}

	return hooks, copyFactoryHookDiagnostics(hookDiags), nil
}

func LoadHooksForNativeProjection(src fs.FS, path string) ([]*ir.Hook, []diag.Diagnostic, error) {
	hooks, hookDiags, err := bundle.LoadFactoryHooksForNativeProjection(src, path)
	if err != nil {
		return nil, nil, err
	}
	for _, hook := range hooks {
		hook.HookSource = string(targetmeta.TargetFactory)
	}
	return hooks, copyFactoryHookDiagnostics(hookDiags), nil
}

func copyFactoryHookDiagnostics(hookDiags []*diag.Diagnostic) []diag.Diagnostic {
	diags := make([]diag.Diagnostic, 0, len(hookDiags))
	for _, diagnostic := range hookDiags {
		diags = append(diags, *diagnostic)
	}
	return diags
}
