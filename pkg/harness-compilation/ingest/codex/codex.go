// Package codex ingests documented Codex project layouts.
package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const kindKey = "x-agent-marketplace-kind"

type ingester struct{}

func init() { ingest.Register(ingester{}) }

func (ingester) Source() ingest.Harness { return ingest.HarnessCodex }

func (ingester) Detect(src fs.FS) bool {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		return false
	}
	for _, partition := range layout.Partitions {
		if !partition.InstallationEvidence {
			continue
		}
		info, err := fs.Stat(src, partition.Prefix)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (i ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func Detect(src fs.FS) bool { return ingester{}.Detect(src) }

func LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func load(src fs.FS) (*ingest.Bundle, error) {
	slog.Info("codex ingest start", "source", ingest.HarnessCodex)
	var resources []ir.Resource
	diagnostics := []diag.Diagnostic{{Code: diag.CodeIngestCodexManifestSynthesized, Severity: diag.SeverityInfo, Resource: "bundle", Message: "Codex workspace has no bundle manifest; manifest synthesized"}}
	for _, loader := range []func(fs.FS) ([]ir.Resource, []diag.Diagnostic){loadSkills, loadSubagents, loadHooks, loadMCP, loadPrompts} {
		loaded, loaderDiagnostics := loader(src)
		resources = append(resources, loaded...)
		diagnostics = append(diagnostics, loaderDiagnostics...)
		logCodexIngestDiagnostics(loaderDiagnostics)
	}
	resources, duplicateDiagnostics := suppressDuplicates(resources)
	diagnostics = append(diagnostics, duplicateDiagnostics...)
	logCodexIngestDiagnostics(duplicateDiagnostics)
	consumed := make([]string, 0, 2)
	if file, err := codexAggregateFile(ir.KindHook); err == nil {
		consumed = append(consumed, file)
	}
	if file, err := codexAggregateFile(ir.KindMcpServer); err == nil {
		consumed = append(consumed, file)
	}
	diagnostics = append(diagnostics, ingest.UnconsumedConfigs(src, diag.CodeIngestCodexUnconsumedConfig, "codex workspace", consumed...)...)
	slog.Info("codex ingest complete", "source", ingest.HarnessCodex, "resources", len(resources), "diagnostics", len(diagnostics))
	return &ingest.Bundle{Manifest: ir.Manifest{Name: "codex-workspace"}, Resources: resources, Diagnostics: diagnostics}, nil
}

func logCodexIngestDiagnostics(diagnostics []diag.Diagnostic) {
	for _, diagnostic := range diagnostics {
		attrs := []any{
			"code", diagnostic.Code,
			"severity", diagnostic.Severity.String(),
			"resource", diagnostic.Resource,
			"file", diagnostic.Source.File,
		}
		switch diagnostic.Severity {
		case diag.SeverityError:
			slog.Error("codex ingest input skipped", attrs...)
		case diag.SeverityWarning:
			slog.Warn("codex ingest decision", attrs...)
		default:
			slog.Debug("codex ingest decision", attrs...)
		}
	}
}

func loadSkills(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	root, err := codexResourceRoot(ir.KindSkill)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, []diag.Diagnostic{loadFailure(root, err)}
	}
	var resources []ir.Resource
	var diagnostics []diag.Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile, err := codexResourceFile(ir.KindSkill, entry.Name())
		if err != nil {
			diagnostics = append(diagnostics, loadFailure("codex layout", err))
			continue
		}
		skill, err := bundle.LoadSkill(src, path.Dir(skillFile))
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(skillFile, err))
			continue
		}
		skill.ModelSource = "codex"
		if marker, ok := skill.Extra[kindKey]; ok && marker == "command" {
			if skill.AllowedTools != nil || skill.PermissionsSet || skill.Triggers != nil || skill.Agent != "" || skill.Subagent != "" {
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     diag.CodeSkillCodexSecurityRestrictionUnsupported,
					Severity: diag.SeverityError,
					Resource: skill.Name(),
					Message:  "marked Codex command carries an authority-bearing skill control; command skipped",
					Source:   diag.SourceRef(skill.Source),
				})
				continue
			}
			if skill.ArgumentHint != "" || skill.Model != "" || len(skill.SupportFiles) > 0 {
				diagnostics = append(diagnostics, diag.Diagnostic{
					Code:     diag.CodeIngestCodexFieldDropped,
					Severity: diag.SeverityWarning,
					Resource: skill.Name(),
					Message:  "marked Codex command metadata or support files have no command IR equivalent and were omitted",
					Source:   diag.SourceRef(skill.Source),
				})
			}
			delete(skill.Extra, kindKey)
			resources = append(resources, ir.NewCommand(skill.Name(), skill.Description, skill.Body, skill.Extra, skill.Source))
			continue
		}
		resources = append(resources, skill)
	}
	return resources, diagnostics
}

type subagentDocument struct {
	Name         string `toml:"name"`
	Description  string `toml:"description"`
	Model        string `toml:"model"`
	Effort       string `toml:"model_reasoning_effort"`
	Instructions string `toml:"developer_instructions"`
	SandboxMode  string `toml:"sandbox_mode"`
}

func loadSubagents(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	root, err := codexResourceRoot(ir.KindSubagent)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	files, err := filesWithSuffix(src, root, ".toml")
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(root, err)}
	}
	resources := make([]ir.Resource, 0, len(files))
	var diagnostics []diag.Diagnostic
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		var doc subagentDocument
		if err := toml.Unmarshal(raw, &doc); err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		var fields map[string]any
		if err := toml.Unmarshal(raw, &fields); err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		name := doc.Name
		if strings.TrimSpace(doc.Name) == "" || strings.TrimSpace(doc.Description) == "" || strings.TrimSpace(doc.Instructions) == "" {
			diagnostics = append(diagnostics, loadFailure(file, errors.New("codex subagent requires name, description, and developer_instructions")))
			continue
		}
		if !mapping.CodexSandboxModeSupported(doc.SandboxMode) {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexSubagentSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex subagent has an invalid sandbox_mode; subagent skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		securityLoss, benignLoss := codexSubagentFieldLoss(fields)
		if securityLoss {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexSubagentSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex subagent authority fields cannot be represented safely; subagent skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		if benignLoss {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: name, Message: "benign Codex subagent configuration fields were omitted", Source: diag.SourceRef{File: file, Line: 1}})
		}
		subagent := ir.NewSubagent(name, doc.Description, doc.Instructions, doc.Model, nil, doc.Effort, nil, "", nil, ir.SourceRef{File: file, Line: 1})
		subagent.ModelSource = "codex"
		if doc.SandboxMode != "" {
			subagent.Ext.Codex = &extpkg.Codex{SandboxMode: doc.SandboxMode}
		}
		resources = append(resources, subagent)
	}
	return resources, diagnostics
}

func codexSubagentFieldLoss(fields map[string]any) (security, benign bool) {
	for field := range fields {
		switch field {
		case "name", "description", "model", "model_reasoning_effort", "developer_instructions", "sandbox_mode":
		case "model_reasoning_summary", "model_verbosity", "nickname_candidates", "personality":
			benign = true
		default:
			security = true
		}
	}
	return security, benign
}

func loadHooks(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	file, err := codexAggregateFile(ir.KindHook)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	var document map[string]json.RawMessage
	if err := jsonutil.Unmarshal(file, raw, &document); err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	var diagnostics []diag.Diagnostic
	for field := range document {
		switch field {
		case "hooks":
		case "description":
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: "hooks", Message: "Codex hook description metadata was omitted", Source: diag.SourceRef{File: file, Line: 1}})
		default:
			return nil, []diag.Diagnostic{codexHookUnsupported("hooks", file, "unsupported Codex hook document field; hooks skipped")}
		}
	}
	rawHooks, ok := document["hooks"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawHooks), []byte("null")) {
		return nil, []diag.Diagnostic{codexHookUnsupported("hooks", file, "Codex hook document has no hooks object")}
	}
	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(rawHooks, &hooks); err != nil {
		return nil, []diag.Diagnostic{codexHookUnsupported("hooks", file, "Codex hooks field is malformed; hooks skipped")}
	}
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	var resources []ir.Resource
	for _, event := range events {
		var entries []json.RawMessage
		if rawEvent := hooks[event]; bytes.Equal(bytes.TrimSpace(rawEvent), []byte("null")) || json.Unmarshal(rawEvent, &entries) != nil {
			diagnostics = append(diagnostics, codexHookUnsupported(strings.ToLower(event), file, "Codex hook event entries are malformed; event skipped"))
			continue
		}
		canonicalEvent, mappedMatcher, supported := mapping.ReverseHookEvent(event, mapping.TargetCodex)
		for entryIndex, rawEntry := range entries {
			entryName := fmt.Sprintf("%s-%d", strings.ToLower(event), entryIndex+1)
			matcher, handlers, err := decodeCodexHookEntry(rawEntry)
			if err != nil {
				diagnostics = append(diagnostics, codexHookUnsupported(entryName, file, "unsupported or malformed Codex hook entry was skipped"))
				continue
			}
			if !supported || matcher != "" && (canonicalEvent == "UserPromptSubmit" || canonicalEvent == "Stop") {
				diagnostics = append(diagnostics, codexHookUnsupported(entryName, file, "unsupported Codex hook event or matcher; entry skipped"))
				continue
			}
			if matcher == "" {
				matcher = mappedMatcher
			}
			for handlerIndex, rawHandler := range handlers {
				name := fmt.Sprintf("%s-%d", entryName, handlerIndex+1)
				handler, benignLoss, err := decodeCodexHookHandler(rawHandler)
				if err != nil {
					diagnostics = append(diagnostics, codexHookUnsupported(name, file, "unsupported or malformed Codex hook handler was skipped"))
					continue
				}
				if canonicalEvent == "SessionEnd" && handler.async != nil && *handler.async {
					handler.async = nil
					diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: name, Message: "SessionEnd always runs synchronously; async was omitted", Source: diag.SourceRef{File: file, Line: 1}})
				}
				if canonicalEvent == "SessionEnd" && handler.timeout != nil && *handler.timeout > 3 {
					timeout := 3
					handler.timeout = &timeout
					diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: name, Message: "SessionEnd supports at most a three-second timeout; timeout was capped", Source: diag.SourceRef{File: file, Line: 1}})
				}
				if benignLoss {
					diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: name, Message: "benign Codex hook display metadata was omitted", Source: diag.SourceRef{File: file, Line: 1}})
				}
				hook := ir.NewHookWithMatcherOptions(name, canonicalEvent, matcher, handler.command, handler.kind, nil, ir.SourceRef{File: file, Line: 1}, ir.WithHookTimeoutPtr(handler.timeout))
				hook.Ext.Codex = &extpkg.Codex{
					NativeHook: true, HookAsync: handler.async, HookStatusMessage: handler.statusMessage,
					CommandWindows: handler.commandWindows, AdditionalContextLimit: handler.additionalContext,
				}
				resources = append(resources, hook)
			}
		}
	}
	return resources, diagnostics
}

func decodeCodexHookEntry(raw json.RawMessage) (string, []json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", nil, err
	}
	for field := range fields {
		if field != "matcher" && field != "hooks" {
			return "", nil, fmt.Errorf("unsupported entry field")
		}
	}
	var matcher string
	if rawMatcher, ok := fields["matcher"]; ok {
		if bytes.Equal(bytes.TrimSpace(rawMatcher), []byte("null")) || json.Unmarshal(rawMatcher, &matcher) != nil {
			return "", nil, fmt.Errorf("invalid matcher")
		}
	}
	var handlers []json.RawMessage
	rawHandlers, ok := fields["hooks"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawHandlers), []byte("null")) || json.Unmarshal(rawHandlers, &handlers) != nil {
		return "", nil, fmt.Errorf("missing or malformed handlers")
	}
	return matcher, handlers, nil
}

type codexHookHandler struct {
	kind              string
	command           string
	async             *bool
	statusMessage     *string
	timeout           *int
	commandWindows    *string
	additionalContext *int
}

func decodeCodexHookHandler(raw json.RawMessage) (codexHookHandler, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return codexHookHandler{}, false, err
	}
	var handler codexHookHandler
	if err := json.Unmarshal(fields["type"], &handler.kind); err != nil {
		return codexHookHandler{}, false, fmt.Errorf("missing handler type")
	}
	benignLoss := false
	for field := range fields {
		supported := field == "type" || field == "timeout" || field == "statusMessage"
		switch handler.kind {
		case "command":
			supported = supported || field == "command" || field == "async" || field == "additionalContextLimit" || field == "commandWindows" || field == "command_windows"
		default:
			return codexHookHandler{}, false, fmt.Errorf("unsupported handler type")
		}
		if !supported {
			return codexHookHandler{}, false, fmt.Errorf("unsupported handler field")
		}
	}
	switch handler.kind {
	case "command":
		if err := json.Unmarshal(fields["command"], &handler.command); err != nil || handler.command == "" {
			return codexHookHandler{}, false, fmt.Errorf("missing handler command")
		}
		if rawAsync, ok := fields["async"]; ok {
			var async bool
			if err := json.Unmarshal(rawAsync, &async); err != nil {
				return codexHookHandler{}, false, fmt.Errorf("invalid async")
			}
			handler.async = &async
		}
	}
	if rawTimeout, ok := fields["timeout"]; ok && !bytes.Equal(bytes.TrimSpace(rawTimeout), []byte("null")) {
		var timeout int
		if err := json.Unmarshal(rawTimeout, &timeout); err != nil || timeout < 0 {
			return codexHookHandler{}, false, fmt.Errorf("invalid handler timeout")
		}
		handler.timeout = &timeout
	}
	rawCommand, camelCase := fields["commandWindows"]
	if snakeCase, ok := fields["command_windows"]; ok {
		if camelCase {
			return codexHookHandler{}, false, fmt.Errorf("ambiguous Windows command")
		}
		rawCommand, camelCase = snakeCase, true
	}
	if camelCase && !bytes.Equal(bytes.TrimSpace(rawCommand), []byte("null")) {
		var command string
		if err := json.Unmarshal(rawCommand, &command); err != nil {
			return codexHookHandler{}, false, fmt.Errorf("invalid commandWindows")
		}
		handler.commandWindows = &command
	}
	if rawLimit, ok := fields["additionalContextLimit"]; ok && !bytes.Equal(bytes.TrimSpace(rawLimit), []byte("null")) {
		var limit int
		if err := json.Unmarshal(rawLimit, &limit); err != nil || limit < 0 {
			return codexHookHandler{}, false, fmt.Errorf("invalid additionalContextLimit")
		}
		handler.additionalContext = &limit
	}
	if rawStatus, ok := fields["statusMessage"]; ok && !bytes.Equal(bytes.TrimSpace(rawStatus), []byte("null")) {
		var status string
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			return codexHookHandler{}, false, fmt.Errorf("invalid statusMessage")
		}
		handler.statusMessage = &status
	}
	return handler, benignLoss, nil
}

func codexHookUnsupported(resource, file, message string) diag.Diagnostic {
	return diag.Diagnostic{Code: diag.CodeIngestCodexHookUnsupported, Severity: diag.SeverityError, Resource: resource, Message: message, Source: diag.SourceRef{File: file, Line: 1}}
}

type mcpDocument struct {
	Command                 string            `toml:"command"`
	Args                    []string          `toml:"args"`
	CWD                     string            `toml:"cwd"`
	ExperimentalEnvironment string            `toml:"experimental_environment"`
	Env                     map[string]string `toml:"env"`
	URL                     string            `toml:"url"`
	HTTPHeaders             map[string]string `toml:"http_headers"`
	Enabled                 *bool             `toml:"enabled"`
	EnvHTTPHeaders          map[string]string `toml:"env_http_headers"`
	BearerTokenEnvVar       string            `toml:"bearer_token_env_var"`
	OAuthResource           string            `toml:"oauth_resource"`
}

func loadMCP(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	file, err := codexAggregateFile(ir.KindMcpServer)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	rawServers, exists := document["mcp_servers"]
	if !exists {
		return nil, nil
	}
	servers, ok := rawServers.(map[string]any)
	if !ok {
		return nil, []diag.Diagnostic{malformedCodexMCP("mcp_servers", file)}
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	resources := make([]ir.Resource, 0, len(names))
	var diagnostics []diag.Diagnostic
	for _, name := range names {
		fields, ok := servers[name].(map[string]any)
		if !ok {
			diagnostics = append(diagnostics, malformedCodexMCP(name, file))
			continue
		}
		securityLoss, benignLoss := codexMCPFieldLoss(fields)
		securityLoss = securityLoss || codexMCPFormSecurityLoss(fields)
		if securityLoss {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexMcpSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex MCP security or execution fields cannot be represented safely; server skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		localEnvVars, remoteEnvVars, err := decodeCodexMCPEnvVars(fields["env_vars"])
		if err != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexMcpSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex MCP environment forwarding cannot be represented safely; server skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		encoded, err := toml.Marshal(fields)
		if err != nil {
			diagnostics = append(diagnostics, malformedCodexMCP(name, file))
			continue
		}
		var server mcpDocument
		if err := toml.Unmarshal(encoded, &server); err != nil {
			diagnostics = append(diagnostics, malformedCodexMCP(name, file))
			continue
		}
		remoteExecution := server.ExperimentalEnvironment == "remote"
		validEnvironment := server.ExperimentalEnvironment == "" || server.ExperimentalEnvironment == "local" || remoteExecution
		if !validEnvironment || (len(remoteEnvVars) > 0 && !remoteExecution) || (server.URL != "" && server.ExperimentalEnvironment != "") {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexMcpSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex MCP remote execution provenance cannot be represented safely; server skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		if benignLoss {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexFieldDropped, Severity: diag.SeverityWarning, Resource: name, Message: "benign Codex MCP timing fields were omitted", Source: diag.SourceRef{File: file, Line: 1}})
		}
		env := make(map[string]string, len(server.Env)+len(localEnvVars))
		for key, value := range server.Env {
			env[key] = value
		}
		var invalidEnv bool
		for _, envVar := range localEnvVars {
			value := "${" + envVar + "}"
			existing, exists := env[envVar]
			if !validEnvironmentName(envVar) || exists && existing != value {
				invalidEnv = true
				break
			}
			env[envVar] = value
		}
		for _, envVar := range remoteEnvVars {
			if _, exists := env[envVar]; exists {
				invalidEnv = true
				break
			}
		}
		if invalidEnv {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexMcpSecurityUnsupported, Severity: diag.SeverityError, Resource: name, Message: "Codex MCP environment forwarding cannot be represented safely; server skipped", Source: diag.SourceRef{File: file, Line: 1}})
			continue
		}
		if len(env) == 0 {
			env = nil
		}
		options := []ir.McpServerOption{}
		if server.URL != "" {
			options = append(options, ir.WithMcpURL(server.URL))
		}
		if server.HTTPHeaders != nil {
			options = append(options, ir.WithMcpHeaders(server.HTTPHeaders))
		}
		if server.Enabled != nil {
			options = append(options, ir.WithMcpDisabled(!*server.Enabled))
		}
		if server.OAuthResource != "" {
			options = append(options, ir.WithMcpOAuth("", "", server.OAuthResource))
		}
		var extra map[string]any
		if server.CWD != "" {
			extra = map[string]any{"cwd": server.CWD}
		}
		resource := ir.NewMcpServerWithOptions(name, server.Command, server.Args, env, extra, ir.SourceRef{File: file, Line: 1}, options...)
		resource.Ext.Codex = &extpkg.Codex{
			MCP: &extpkg.CodexMCP{
				Command:                 server.Command,
				Args:                    slices.Clone(server.Args),
				CWD:                     server.CWD,
				ExperimentalEnvironment: server.ExperimentalEnvironment,
				Env:                     cloneStrings(server.Env),
				EnvVars:                 slices.Clone(localEnvVars),
				RemoteEnvVars:           slices.Clone(remoteEnvVars),
				URL:                     server.URL,
				HTTPHeaders:             cloneStrings(server.HTTPHeaders),
			},
			BearerTokenEnvVar: server.BearerTokenEnvVar,
			EnvHTTPHeaders:    cloneStrings(server.EnvHTTPHeaders),
		}
		resources = append(resources, resource)
	}
	return resources, diagnostics
}

func codexMCPFormSecurityLoss(fields map[string]any) bool {
	command, commandOK := fields["command"].(string)
	url, urlOK := fields["url"].(string)
	if _, exists := fields["command"]; exists && !commandOK {
		return false // Per-server typed decoding reports the malformed value.
	}
	if _, exists := fields["url"]; exists && !urlOK {
		return false // Per-server typed decoding reports the malformed value.
	}
	stdio, remote := command != "", url != ""
	if stdio == remote {
		return true
	}
	if stdio {
		return hasAnyField(fields, "url", "http_headers", "env_http_headers", "bearer_token_env_var", "oauth_resource")
	}
	return hasAnyField(fields, "command", "args", "cwd", "env", "env_vars")
}

func hasAnyField(fields map[string]any, names ...string) bool {
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func malformedCodexMCP(resource, file string) diag.Diagnostic {
	return diag.Diagnostic{Code: diag.CodeIngestCodexLoadResourceFailed, Severity: diag.SeverityError, Resource: resource, Message: "Codex MCP server is malformed and was skipped", Source: diag.SourceRef{File: file, Line: 1}}
}

func codexMCPFieldLoss(fields map[string]any) (security, benign bool) {
	for field := range fields {
		switch field {
		case "command", "args", "cwd", "experimental_environment", "env", "env_vars", "url", "http_headers", "enabled", "env_http_headers", "bearer_token_env_var", "oauth_resource":
		case "name", "required", "supports_parallel_tool_calls", "startup_timeout_ms", "startup_timeout_sec", "tool_timeout_sec":
			benign = true
		default:
			security = true
		}
	}
	return security, benign
}

func decodeCodexMCPEnvVars(raw any) (local, remote []string, err error) {
	if raw == nil {
		return nil, nil, nil
	}
	var entries []any
	switch typed := raw.(type) {
	case []any:
		entries = typed
	case []string:
		entries = make([]any, len(typed))
		for i := range typed {
			entries[i] = typed[i]
		}
	default:
		return nil, nil, errors.New("env_vars must be an array")
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name, source := "", "local"
		switch typed := entry.(type) {
		case string:
			name = typed
		case map[string]any:
			if len(typed) != 2 {
				return nil, nil, errors.New("env_vars object has unsupported fields")
			}
			var ok bool
			name, ok = typed["name"].(string)
			if !ok {
				return nil, nil, errors.New("env_vars object has invalid name")
			}
			source, ok = typed["source"].(string)
			if !ok {
				return nil, nil, errors.New("env_vars object has invalid source")
			}
		default:
			return nil, nil, errors.New("env_vars entry is malformed")
		}
		if !validEnvironmentName(name) {
			return nil, nil, errors.New("env_vars entry has invalid name")
		}
		if _, exists := seen[name]; exists {
			return nil, nil, errors.New("env_vars contains duplicate names")
		}
		seen[name] = struct{}{}
		switch source {
		case "local":
			local = append(local, name)
		case "remote":
			remote = append(remote, name)
		default:
			return nil, nil, errors.New("env_vars entry has unsupported source")
		}
	}
	return local, remote, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func cloneStrings(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func loadPrompts(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	files, err := filesWithSuffix(src, ".codex/prompts", ".md")
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(".codex/prompts", err)}
	}
	resources := make([]ir.Resource, 0, len(files))
	diagnostics := make([]diag.Diagnostic, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		name := strings.TrimSuffix(path.Base(file), ".md")
		resources = append(resources, ir.NewCommand(name, "Deprecated Codex prompt", string(raw), nil, ir.SourceRef{File: file, Line: 1}))
		diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexPromptDeprecated, Severity: diag.SeverityWarning, Resource: name, Message: "deprecated Codex prompt ingested as a command", Source: diag.SourceRef{File: file, Line: 1}})
	}
	return resources, diagnostics
}

func filesWithSuffix(src fs.FS, dir, suffix string) ([]string, error) {
	entries, err := fs.ReadDir(src, dir)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			files = append(files, path.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

func codexResourceRoot(kind ir.ResourceKind) (string, error) {
	root, ok := targetmeta.NativeResourceRoot(targetmeta.TargetCodex, kind)
	if !ok {
		return "", fmt.Errorf("native resource root for %s is not registered", kind)
	}
	return root, nil
}

func codexResourceFile(kind ir.ResourceKind, name string) (string, error) {
	file, ok := targetmeta.NativeResourcePath(targetmeta.TargetCodex, kind, name)
	if !ok {
		return "", fmt.Errorf("native resource path for %s is not registered", kind)
	}
	return file, nil
}

func codexAggregateFile(kind ir.ResourceKind) (string, error) {
	file, ok := targetmeta.NativeAggregatePath(targetmeta.TargetCodex, kind)
	if !ok {
		return "", fmt.Errorf("native aggregate path for %s is not registered", kind)
	}
	return file, nil
}

func isMissing(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func loadFailure(file string, err error) diag.Diagnostic {
	diagnostic := ingest.LoadFailure(diag.CodeIngestCodexLoadResourceFailed, "Codex resource", path.Base(file), file, err)
	diagnostic.Message = "malformed Codex resource was skipped"
	if diagnostic.Hint == "" {
		diagnostic.Hint = "fix the reported native file; independent valid resources were retained"
	}
	return diagnostic
}

func suppressDuplicates(resources []ir.Resource) ([]ir.Resource, []diag.Diagnostic) {
	counts := make(map[string]int, len(resources))
	for _, resource := range resources {
		counts[resource.Kind().String()+"\x00"+resource.Name()]++
	}
	kept := resources[:0]
	var diagnostics []diag.Diagnostic
	reported := map[string]bool{}
	for _, resource := range resources {
		key := resource.Kind().String() + "\x00" + resource.Name()
		if counts[key] == 1 {
			kept = append(kept, resource)
			continue
		}
		if !reported[key] {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexDuplicateIdentity, Severity: diag.SeverityError, Resource: resource.Name(), Message: "duplicate Codex resource identity was suppressed without choosing a winner"})
			reported[key] = true
		}
	}
	return kept, diagnostics
}
