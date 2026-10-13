package opencode

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
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
	matcher, ok := declarationMatcher(h.Matcher, opts.ToolOverride())
	if !ok {
		return fail()
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
	declaration := opencodehook.Declaration{Format: opencodehook.Format, Name: h.Name(), Event: h.Event, Matcher: matcher, Command: command, Timeout: timeout}
	data, err := json.MarshalIndent(declaration, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	// The adapter is one fixed reviewed file shared by every accepted hook in
	// a bundle. It is emitted as a fragment that coalesces byte-identical
	// copies, so a multi-resource bundle publishes exactly one adapter
	// alongside all of its declarations instead of colliding on the shared
	// path. A single-resource compile still returns the adapter unchanged.
	return []artifact.Artifact{
		{Path: path.Join(opencodehook.DeclarationsDir, h.Name()+".json"), Bytes: append(data, '\n'), Mode: 0600},
		{Path: opencodehook.AdapterPath, Bytes: append([]byte(nil), opencodehook.Adapter...), Mode: 0644, Fragment: true, FragmentKey: opencodehook.AdapterFragmentKey, DedupeFragmentEntries: true},
	}, nil, nil
}

// declarationMatcher resolves the canonical matcher to the spelling the fixed
// adapter compares at runtime and reports whether every entry is supported.
//
// An unoverridden tool must be a curated canonical tool: the adapter
// canonicalizes native tool names before comparing them to a declaration
// matcher, so a canonical entry such as "Bash" matches native bash invocations.
//
// An overridden tool uses the override spelling, the same mapping authority as
// restrictive subagent policies. The override value must be a plain tool name
// that the adapter does NOT canonicalize: a value the fixed runtime maps back
// to a canonical name would never compare equal after canonicalization, so
// such a mapping fails closed here instead of installing a silently dead
// matcher. Task stays unsupported regardless of mapping because nested
// subagent tool-execution coverage is not advertised.
func declarationMatcher(matcher string, overrides map[string]string) (string, bool) {
	if matcher == "" || matcher == "*" {
		return matcher, true
	}
	var emitted []string
	for _, tool := range strings.Split(matcher, "|") {
		if tool == "Task" {
			return "", false
		}
		if value, overridden := overrides[tool]; overridden {
			if value == "" || value == "*" || strings.Contains(value, "|") {
				return "", false
			}
			if _, known := mapping.ReverseTool(value, mapping.TargetOpenCode); known {
				return "", false
			}
			emitted = append(emitted, value)
			continue
		}
		if _, ok := mapping.ToolWithOverride(tool, mapping.TargetOpenCode, nil); !ok {
			return "", false
		}
		emitted = append(emitted, tool)
	}
	return strings.Join(emitted, "|"), true
}
