package windsurf

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

type hookFile struct {
	Command    string         `json:"command"`
	Prompt     string         `json:"prompt,omitempty"`
	Event      string         `json:"event"`
	Type       string         `json:"type"`
	Matcher    string         `json:"matcher,omitempty"`
	Timeout    *int           `json:"timeout,omitempty"`
	PowerShell string         `json:"powershell,omitempty"`
	ShowOutput *bool          `json:"show_output,omitempty"`
	WorkingDir string         `json:"working_dir,omitempty"`
	Extra      map[string]any `json:"-"`
}

type hookRule struct {
	Type       string         `json:"type"`
	Command    string         `json:"command"`
	Prompt     string         `json:"prompt,omitempty"`
	Matcher    string         `json:"matcher,omitempty"`
	Timeout    *int           `json:"timeout,omitempty"`
	PowerShell string         `json:"powershell,omitempty"`
	ShowOutput *bool          `json:"show_output,omitempty"`
	WorkingDir string         `json:"working_dir,omitempty"`
	Extra      map[string]any `json:"-"`
}

func (h *hookRule) UnmarshalJSON(data []byte) error {
	var decoded hookFile
	if err := jsonutil.Unmarshal("", data, &decoded); err != nil {
		return err
	}
	*h = hookRule{
		Type: decoded.Type, Command: decoded.Command, Prompt: decoded.Prompt,
		Matcher: decoded.Matcher, Timeout: decoded.Timeout,
		PowerShell: decoded.PowerShell, ShowOutput: decoded.ShowOutput,
		WorkingDir: decoded.WorkingDir, Extra: decoded.Extra,
	}
	return nil
}

func (h *hookFile) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "command":
			if err := json.Unmarshal(value, &h.Command); err != nil {
				return err
			}
		case "prompt":
			if err := json.Unmarshal(value, &h.Prompt); err != nil {
				return err
			}
		case "timeout":
			if err := json.Unmarshal(value, &h.Timeout); err != nil {
				return err
			}
		case "event":
			if err := json.Unmarshal(value, &h.Event); err != nil {
				return err
			}
		case "type":
			if err := json.Unmarshal(value, &h.Type); err != nil {
				return err
			}
		case "matcher":
			if err := json.Unmarshal(value, &h.Matcher); err != nil {
				return err
			}
		case "powershell":
			if err := json.Unmarshal(value, &h.PowerShell); err != nil {
				return err
			}
		case "show_output":
			if err := json.Unmarshal(value, &h.ShowOutput); err != nil {
				return err
			}
		case "working_dir":
			if err := json.Unmarshal(value, &h.WorkingDir); err != nil {
				return err
			}
		default:
			if h.Extra == nil {
				h.Extra = map[string]any{}
			}
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				h.Extra[key] = string(value)
			} else {
				h.Extra[key] = decoded
			}
		}
	}
	return nil
}

func LoadHooks(src fs.FS, dirOrFile string) ([]*ir.Hook, []diag.Diagnostic, error) {
	return loadHooksWithOptions(src, dirOrFile, LoadOptions{})
}

func loadHooksWithOptions(src fs.FS, dirOrFile string, opts LoadOptions) ([]*ir.Hook, []diag.Diagnostic, error) {
	entries, err := fs.ReadDir(src, dirOrFile)
	if err == nil {
		return loadHooksDir(src, dirOrFile, entries)
	}
	data, fileErr := fs.ReadFile(src, dirOrFile)
	if fileErr != nil {
		return nil, nil, fileErr
	}
	if opts.LegacyHookInventory {
		return loadHooksAliasLegacy(data, dirOrFile)
	}
	return loadHooksAlias(data, dirOrFile)
}

func loadHooksDir(src fs.FS, dir string, entries []fs.DirEntry) ([]*ir.Hook, []diag.Diagnostic, error) {
	var hooks []*ir.Hook
	var diags []diag.Diagnostic
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		file := path.Join(dir, entry.Name())
		data, err := fs.ReadFile(src, file)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/windsurf: read hook %s: %w", file, err)
		}
		var rule hookFile
		if err := jsonc.Unmarshal(file, data, &rule); err != nil {
			return nil, nil, fmt.Errorf("ingest/windsurf: parse hook: %w", err)
		}
		loaded, hookDiags := canonicalizeHook(file, rule)
		if loaded != nil {
			hooks = append(hooks, loaded)
		}
		diags = append(diags, hookDiags...)
	}
	return hooks, diags, nil
}

func loadHooksAliasLegacy(data []byte, file string) ([]*ir.Hook, []diag.Diagnostic, error) {
	var payload struct {
		Hooks map[string][]hookRule `json:"hooks"`
	}
	if err := jsonc.Unmarshal(file, data, &payload); err != nil {
		return nil, nil, fmt.Errorf("ingest/windsurf: parse hooks alias: %w", err)
	}
	var hooks []*ir.Hook
	var diags []diag.Diagnostic
	events := make([]string, 0, len(payload.Hooks))
	for event := range payload.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	for _, event := range events {
		for _, rule := range payload.Hooks[event] {
			loaded, hookDiags := canonicalizeHook(file, hookFile{Command: rule.Command, Prompt: rule.Prompt, Event: event, Type: rule.Type, Matcher: rule.Matcher, Timeout: rule.Timeout, PowerShell: rule.PowerShell, ShowOutput: rule.ShowOutput, WorkingDir: rule.WorkingDir, Extra: rule.Extra})
			if loaded != nil {
				hooks = append(hooks, loaded)
			}
			diags = append(diags, hookDiags...)
		}
	}
	return hooks, diags, nil
}

func loadHooksAlias(data []byte, file string) ([]*ir.Hook, []diag.Diagnostic, error) {
	hooks, diags, err := loadHooksAliasLegacy(data, file)
	if err != nil {
		return nil, nil, err
	}
	distinctHookNames(hooks)
	return hooks, diags, nil
}

func distinctHookNames(hooks []*ir.Hook) {
	last := make(map[string]int, len(hooks))
	used := make(map[string]bool, len(hooks))
	next := make(map[string]int, len(hooks))
	for i, hook := range hooks {
		last[hook.Name()] = i
		used[hook.Name()] = true
	}
	for i, hook := range hooks {
		base := hook.Name()
		if last[base] == i {
			continue
		}
		start := next[base]
		if start < 2 {
			start = 2
		}
		for suffix := start; ; suffix++ {
			name := fmt.Sprintf("%s-%d", base, suffix)
			if !used[name] {
				hook.SetName(name)
				used[name] = true
				next[base] = suffix + 1
				break
			}
		}
	}
}

func canonicalizeHook(file string, rule hookFile) (*ir.Hook, []diag.Diagnostic) {
	hookType := rule.Type
	if hookType == "" {
		hookType = "command"
	}
	canonicalEvent, matcher, ok := mapping.ReverseHookEvent(rule.Event, compile.TargetWindsurf)
	var diags []diag.Diagnostic
	if !ok {
		// Keep the native event in the sidecar and use the source event as the
		// canonical placeholder. This lets the resource survive ingestion and
		// round-trip back to Windsurf without pretending it maps to Claude.
		canonicalEvent = rule.Event
		matcher = rule.Matcher
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestWindsurfHookEventUnmappable,
			Severity: diag.SeverityWarning,
			Resource: path.Base(file),
			Message:  fmt.Sprintf("Windsurf hook event %q has no Claude equivalent; original event preserved", rule.Event),
			Hint:     "Use a mappable Windsurf hook event or preserve the native event through the Windsurf sidecar",
			Source:   diag.SourceRef{File: file, Line: 1},
		})
	} else {
		matcher = normalizeMatcher(rule.Event, matcher)
	}
	extra := rule.Extra
	if len(extra) == 0 {
		extra = nil
	}
	name := strings.TrimSuffix(path.Base(file), ".json")
	options := []ir.HookOption{ir.WithHookPrompt(rule.Prompt)}
	if rule.Timeout != nil {
		options = append(options, ir.WithHookTimeoutPtr(rule.Timeout))
	}
	var sidecar *extpkg.Windsurf
	if !ok || rule.PowerShell != "" || rule.ShowOutput != nil || rule.WorkingDir != "" {
		sidecar = &extpkg.Windsurf{HookPowerShell: rule.PowerShell, HookShowOutput: rule.ShowOutput, HookWorkingDir: rule.WorkingDir}
		if !ok {
			sidecar.OriginalHookEvent = rule.Event
		}
	}
	if sidecar != nil {
		options = append(options, ir.WithHookWindsurfSidecar(sidecar))
	}
	if matcher != "" {
		return ir.NewHookWithMatcherOptions(name, canonicalEvent, matcher, rule.Command, hookType, extra, ir.SourceRef{File: file, Line: 1}, options...), diags
	}
	return ir.NewHookWithOptions(name, canonicalEvent, rule.Command, hookType, extra, ir.SourceRef{File: file, Line: 1}, options...), diags
}

func normalizeMatcher(event, matcher string) string {
	switch event {
	case "pre_write_code":
		return "Write"
	case "pre_mcp_tool_use":
		return "mcp__*"
	default:
		return matcher
	}
}
