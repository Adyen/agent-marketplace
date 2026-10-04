package opencode

import (
	"encoding/json"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func hook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fail := func() ([]artifact.Artifact, []diag.Diagnostic, error) {
		return nil, []diag.Diagnostic{diagnostic(diag.CodeHookOpenCodeUnsupported, diag.SeverityError, h.Name(), h.Source, "OpenCode supports command PreToolUse hooks with bare tool matchers and positive timeouts up to 600 seconds; hook omitted")}, nil
	}
	if h.Event != "PreToolUse" || (h.Type != "" && h.Type != "command") || h.Command == "" || h.Prompt != "" || len(h.Extra) > 0 {
		return fail()
	}
	if h.Matcher != "" && h.Matcher != "*" {
		for _, tool := range strings.Split(h.Matcher, "|") {
			if _, ok := tools[tool]; !ok || tool == "Task" {
				return fail()
			}
		}
	}
	timeout := 60
	if h.Timeout != nil {
		timeout = *h.Timeout
	}
	if timeout < 1 || timeout > 600 {
		return fail()
	}
	command := h.Command
	if strings.Contains(command, "${CLAUDE_PLUGIN_ROOT}") {
		if opts.PluginRoot() == "" {
			return fail()
		}
		command = strings.ReplaceAll(command, "${CLAUDE_PLUGIN_ROOT}", opts.PluginRoot())
	}
	declaration := opencodehook.Declaration{Format: opencodehook.Format, Name: h.Name(), Event: h.Event, Matcher: h.Matcher, Command: command, Timeout: timeout}
	data, err := json.MarshalIndent(declaration, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{
		{Path: "hooks/agent-marketplace/" + h.Name() + ".json", Bytes: append(data, '\n'), Mode: 0600},
		{Path: "plugins/agent-marketplace.js", Bytes: append([]byte(nil), opencodehook.Adapter...), Mode: 0644},
	}, nil, nil
}
