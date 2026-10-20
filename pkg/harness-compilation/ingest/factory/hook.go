package factory

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// LoadHooks reads a Factory hooks file. Factory spellings — the `Execute`
// matcher, group fields such as `commandRegex`, and `${DROID_PLUGIN_ROOT}` —
// stay as authored; the catalog resolver translates them through the recorded
// Factory provenance instead of the ingester rewriting them early.
// nativeHooksFile is the standalone Factory hook file; it is a pure
// top-level event map with no unrelated members.
const nativeHooksFile = ".factory/hooks.json"
const droidPluginRootToken = "${DROID_PLUGIN_ROOT}"

func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []diag.Diagnostic, error) {
	load := bundle.LoadHooks
	native := path == nativeHooksFile
	if native {
		load = bundle.LoadHooksStrict
	}
	hooks, hookDiags, err := load(src, path)
	if err != nil {
		return nil, nil, err
	}

	for _, hook := range hooks {
		hook.HookSource = string(mapping.TargetFactory)
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
