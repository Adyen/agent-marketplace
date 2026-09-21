package factory

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// droidPluginRootToken is the Factory-native plugin-root reference spelling.
const droidPluginRootToken = "${DROID_PLUGIN_ROOT}"

// LoadHooks reads a Factory hooks.json file and canonicalizes Factory env
// tokens back to the Claude IR token vocabulary.
func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []diag.Diagnostic, error) {
	hooks, hookDiags, err := bundle.LoadHooks(src, path)
	if err != nil {
		return nil, nil, err
	}

	for _, hook := range hooks {
		hook.Command = strings.ReplaceAll(hook.Command, droidPluginRootToken, bundle.CanonicalRootToken)
	}

	diags := make([]diag.Diagnostic, 0, len(hookDiags))
	for _, diagnostic := range hookDiags {
		diags = append(diags, *diagnostic)
	}
	return hooks, diags, nil
}
