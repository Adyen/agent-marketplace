package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

const (
	extensionManifestPath = "gemini-extension.json"
	workspaceMCPPath      = ".gemini/mcp.json"
	settingsPath          = ".gemini/settings.json"
	geminiDir             = ".gemini"
	geminiSkillsDir       = ".gemini/skills"
	agentsSkillsDir       = ".agents/skills"
	geminiAgentsDir       = ".gemini/agents"
	geminiCommandsDir     = "commands"

	claudePluginRootToken = "${CLAUDE_PLUGIN_ROOT}"
	geminiExtensionToken  = "${extensionPath}"
)

type geminiIngester struct{}

func init() {
	ingest.Register(&geminiIngester{})
}

func Detect(src fs.FS) bool {
	return (&geminiIngester{}).Detect(src)
}

func LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	return (&geminiIngester{}).LoadBundle(src)
}

func (g *geminiIngester) Source() ingest.Harness {
	return ingest.HarnessGemini
}

func (g *geminiIngester) Detect(src fs.FS) bool {
	if _, err := fs.Stat(src, extensionManifestPath); err == nil {
		return true
	}
	return hasWorkspaceMarkers(src)
}

func (g *geminiIngester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	hasManifest := fileExists(src, extensionManifestPath)
	hasWorkspace := hasWorkspaceMarkers(src)
	if !hasManifest && !hasWorkspace {
		return nil, fmt.Errorf("ingest/gemini: no recognizable Gemini layout found")
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	manifest := ir.Manifest{}

	if hasManifest {
		m, err := loadManifest(src)
		if err != nil {
			return nil, err
		}
		manifest = m
	} else {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestGeminiManifestSynthesized,
			Severity: diag.SeverityInfo,
			Resource: "bundle",
			Message:  "workspace form: manifest synthesized (no gemini-extension.json found)",
			Hint:     "Add gemini-extension.json to the source if bundle metadata is needed",
		})
	}

	skills, skillDiags, err := loadSkills(src)
	if err != nil {
		return nil, err
	}
	diags = append(diags, skillDiags...)
	for _, skill := range skills {
		resources = append(resources, skill)
	}

	commandFiles, err := listFilesWithSuffix(src, geminiCommandsDir, ".toml")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/gemini: walk %s: %w", geminiCommandsDir, err)
	}
	for _, file := range commandFiles {
		command, commandDiags, err := LoadCommand(src, file)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestGeminiLoadCommandFailed, "command", file, file, err))
			continue
		}
		diags = append(diags, commandDiags...)
		resources = append(resources, command)
	}

	subagentFiles, err := listFilesWithSuffix(src, geminiAgentsDir, ".md")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/gemini: walk %s: %w", geminiAgentsDir, err)
	}
	for _, file := range subagentFiles {
		subagent, subagentDiags, err := LoadSubagent(src, file)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestGeminiLoadSubagentFailed, "subagent", file, file, err))
			continue
		}
		diags = append(diags, subagentDiags...)
		resources = append(resources, subagent)
	}

	hooks, hookDiags, err := LoadHooks(src, settingsPath)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/gemini: load %s: %w", settingsPath, err)
	}
	if err == nil {
		diags = append(diags, hookDiags...)
		for _, hook := range hooks {
			resources = append(resources, hook)
		}
	}

	mcpPath := workspaceMCPPath
	if hasManifest {
		mcpPath = extensionManifestPath
	} else if fileExists(src, ".gemini/gemini-extension.json") {
		mcpPath = ".gemini/gemini-extension.json"
	}
	if raw, err := fs.ReadFile(src, settingsPath); err == nil {
		doc, err := jsonc.Parse(settingsPath, raw)
		if err != nil {
			return nil, err
		}
		if _, ok := doc.Root().Member("mcpServers"); ok {
			mcpPath = settingsPath
		}
	} else if !isNotExist(err) {
		return nil, fmt.Errorf("ingest/gemini: read %s: %w", settingsPath, err)
	}
	servers, mcpDiags, err := LoadMcpServers(src, mcpPath)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/gemini: load %s: %w", mcpPath, err)
	}
	if err == nil {
		diags = append(diags, mcpDiags...)
		for _, server := range servers {
			resources = append(resources, server)
		}
	}

	consumed := []string{settingsPath, mcpPath}
	if hasManifest {
		consumed = append(consumed, extensionManifestPath)
	}
	layout := "gemini workspace"
	if hasManifest {
		layout = "gemini plugin"
	}
	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestGeminiUnconsumedConfig, layout, consumed...)...)
	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

func LoadSkill(src fs.FS, root string) (*ir.Skill, error) {
	if err := validatePath(root); err != nil {
		return nil, err
	}

	mdPath := path.Join(root, "SKILL.md")
	f, err := src.Open(mdPath)
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: open %s: %w", mdPath, err)
	}
	defer func() { _ = f.Close() }()

	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: parse %s: %w", mdPath, err)
	}
	if fm == nil {
		fm = map[string]any{}
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = path.Base(root)
	}

	description, _ := fm["description"].(string)
	var triggers []string
	if raw, ok := fm["triggers"]; ok {
		triggers, err = toStringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("ingest/gemini: invalid triggers in %s: %w", mdPath, err)
		}
	}

	extra := extraFields(fm, []string{"name", "description", "triggers", "argument-hint", "model", "allowed-tools", "permissions", "subagent", "agent"})
	argumentHint, err := requiredStringField(fm, "argument-hint", "")
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: invalid argument-hint in %s: %w", mdPath, err)
	}
	skillModel, err := requiredStringField(fm, "model", "")
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: invalid model in %s: %w", mdPath, err)
	}
	subagentName, err := requiredStringField(fm, "subagent", "")
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: invalid subagent in %s: %w", mdPath, err)
	}
	agentName, err := requiredStringField(fm, "agent", "")
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: invalid agent in %s: %w", mdPath, err)
	}
	options := []ir.SkillOption{
		ir.WithSkillArgumentHint(argumentHint), ir.WithSkillModel(skillModel),
		ir.WithSkillSubagent(subagentName), ir.WithSkillAgent(agentName),
	}
	if value, ok := fm["allowed-tools"]; ok {
		allowed, err := toStringSlice(value)
		if err != nil {
			return nil, fmt.Errorf("ingest/gemini: invalid allowed-tools in %s: %w", mdPath, err)
		}
		options = append(options, ir.WithSkillAllowedTools(allowed), ir.WithSkillToolSource("gemini"))
	}
	if value, ok := fm["permissions"]; ok {
		permissions, err := skillPermissions(value)
		if err != nil {
			return nil, fmt.Errorf("ingest/gemini: invalid permissions in %s: %w", mdPath, err)
		}
		options = append(options, ir.WithSkillPermissions(permissions))
	}
	skill := ir.NewSkillWithOptions(name, description, body, triggers, extra, ir.SourceRef{File: mdPath, Line: 1}, options...)
	skill.ModelSource = "gemini"
	supportFiles, err := skillSupportFiles(src, root)
	if err != nil {
		return nil, fmt.Errorf("ingest/gemini: walk support files %s: %w", root, err)
	}
	skill.SupportFiles = supportFiles
	return skill, nil
}

func skillSupportFiles(src fs.FS, root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(src, root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(current, root+"/")
		if relative != "SKILL.md" {
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func loadSkills(src fs.FS) ([]*ir.Skill, []diag.Diagnostic, error) {
	var diags []diag.Diagnostic
	seen := make(map[string]bool)
	var skills []*ir.Skill
	for _, root := range []string{agentsSkillsDir, geminiSkillsDir} {
		skillDirs, err := listSubdirs(src, root)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("ingest/gemini: walk %s: %w", root, err)
		}
		for _, dir := range skillDirs {
			skill, err := LoadSkill(src, dir)
			if err != nil {
				diags = append(diags, ingest.LoadFailure(diag.CodeIngestGeminiLoadSkillFailed, "skill", dir, dir, err))
				continue
			}
			if seen[skill.Name()] {
				continue
			}
			seen[skill.Name()] = true
			skills = append(skills, skill)
		}
	}
	return skills, diags, nil
}

type commandDocument struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Prompt      string   `toml:"prompt"`
	Triggers    []string `toml:"triggers"`
}

func LoadCommand(src fs.FS, path string) (*ir.Command, []diag.Diagnostic, error) {
	if err := validatePath(path); err != nil {
		return nil, nil, err
	}

	data, err := fs.ReadFile(src, path)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: read %s: %w", path, err)
	}

	var doc commandDocument
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: parse %s: %w", path, err)
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: parse raw %s: %w", path, err)
	}

	name := doc.Name
	if name == "" {
		name = strings.TrimSuffix(pathpkgBase(path), ".toml")
	}

	delete(raw, "name")
	delete(raw, "description")
	delete(raw, "prompt")
	_, triggersPresent := raw["triggers"]
	delete(raw, "triggers")

	command := ir.NewCommand(name, doc.Description, normalizeEnvTokens(doc.Prompt), raw, ir.SourceRef{File: path, Line: 1})
	if _, ok := raw["allowed-tools"]; ok {
		command.ToolSource = "gemini"
	}
	command.Ext.Gemini = &extpkg.Gemini{TOMLCommand: &extpkg.GeminiTOMLCommand{
		Name:        doc.Name,
		Description: doc.Description,
		Prompt:      normalizeEnvTokens(doc.Prompt),
		Triggers:    append([]string(nil), doc.Triggers...),
		TriggersSet: triggersPresent,
	}}
	return command, nil, nil
}

func LoadSubagent(src fs.FS, path string) (*ir.Subagent, []diag.Diagnostic, error) {
	if err := validatePath(path); err != nil {
		return nil, nil, err
	}

	f, err := src.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: parse %s: %w", path, err)
	}
	if fm == nil {
		fm = map[string]any{}
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = strings.TrimSuffix(pathpkgBase(path), ".md")
	}

	description, err := requiredStringField(fm, "description", "")
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid description in %s: %w", path, err)
	}
	model, err := requiredStringField(fm, "model", "")
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid model in %s: %w", path, err)
	}
	kind, err := requiredStringField(fm, "kind", "")
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid kind in %s: %w", path, err)
	}
	_, kindSet := fm["kind"]
	var tools []string
	if value, ok := fm["tools"]; ok {
		tools, err = toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid tools in %s: %w", path, err)
		}
	}

	var disallowedTools []string
	if value, ok := fm["disallowedTools"]; ok {
		disallowedTools, err = toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid disallowedTools in %s: %w", path, err)
		}
	}
	isolation, err := requiredStringField(fm, "isolation", "")
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid isolation in %s: %w", path, err)
	}

	maxNestingValue, hasMaxNesting := fm["max-nesting"]
	if !hasMaxNesting {
		maxNestingValue, hasMaxNesting = fm["maxNesting"]
	}
	if hasMaxNesting && maxNestingValue == nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid max-nesting in %s: value must be an integer", path)
	}
	maxNesting, err := toIntPtr(maxNestingValue)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: invalid max-nesting in %s: %w", path, err)
	}

	var temperature *float64
	if value, ok := fm["temperature"]; ok {
		temperature, err = toFloat64Ptr(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid temperature in %s: %w", path, err)
		}
	}
	var maxTurns, timeoutMinutes *int
	if value, ok := fm["max_turns"]; ok {
		maxTurns, err = toNativeIntPtr(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid max_turns in %s: %w", path, err)
		}
	}
	if value, ok := fm["timeout_mins"]; ok {
		timeoutMinutes, err = toNativeIntPtr(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid timeout_mins in %s: %w", path, err)
		}
	}
	var inlineMCPServers map[string]extpkg.GeminiMCPServer
	if value, ok := fm["mcpServers"]; ok {
		inlineMCPServers, err = parseInlineMCPServers(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: invalid mcpServers in %s: %w", path, err)
		}
	}

	extra := extraFields(fm, []string{"name", "description", "kind", "model", "tools", "disallowedTools", "isolation", "max-nesting", "maxNesting", "temperature", "max_turns", "timeout_mins", "mcpServers"})

	options := []ir.SubagentOption{}
	if maxNesting != nil {
		options = append(options, ir.WithSubagentMaxNestingPtr(maxNesting))
	}
	subagent := ir.NewSubagentWithOptions(
		name,
		description,
		normalizeEnvTokens(body),
		model,
		tools,
		"",
		disallowedTools,
		isolation,
		extra,
		ir.SourceRef{File: path, Line: 1}, options...,
	)
	subagent.ModelSource = "gemini"
	if tools != nil {
		subagent.ToolSource = "gemini"
	}
	subagent.Ext.Gemini = &extpkg.Gemini{
		Kind:             kind,
		KindSet:          kindSet,
		Temperature:      temperature,
		MaxTurns:         maxTurns,
		TimeoutMinutes:   timeoutMinutes,
		InlineMCPServers: inlineMCPServers,
	}
	return subagent, nil, nil
}

type hookRule struct {
	Type        string         `json:"type"`
	Command     string         `json:"command"`
	Prompt      string         `json:"prompt,omitempty"`
	Timeout     *int           `json:"timeout,omitempty"`
	Matcher     string         `json:"matcher,omitempty"`
	ExtraFields map[string]any `json:"-"`
	GroupExtra  map[string]any `json:"-"`
}

type hookDefinition struct {
	Matcher    string
	Hooks      []hookRule
	GroupExtra map[string]any `json:"-"`
	Flat       *hookRule      `json:"-"`
}

func (r *hookDefinition) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if nested, grouped := raw["hooks"]; grouped {
		if err := json.Unmarshal(nested, &r.Hooks); err != nil {
			return err
		}
		if r.Hooks == nil {
			return fmt.Errorf("gemini hook group hooks must be an array")
		}
		if matcher, ok := raw["matcher"]; ok {
			var value *string
			if err := json.Unmarshal(matcher, &value); err != nil {
				return err
			}
			if value == nil {
				return fmt.Errorf("gemini hook group matcher must be a string")
			}
			r.Matcher = *value
		}
		delete(raw, "hooks")
		delete(raw, "matcher")
		if len(raw) > 0 {
			r.GroupExtra = make(map[string]any, len(raw))
			for key, value := range raw {
				var decoded any
				if err := json.Unmarshal(value, &decoded); err != nil {
					return err
				}
				r.GroupExtra[key] = decoded
			}
		}
		return nil
	}
	r.Flat = &hookRule{}
	return json.Unmarshal(data, r.Flat)
}

func (r *hookRule) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["type"]; ok {
		if err := json.Unmarshal(v, &r.Type); err != nil {
			return err
		}
		delete(raw, "type")
	}
	if v, ok := raw["command"]; ok {
		if err := json.Unmarshal(v, &r.Command); err != nil {
			return err
		}
		delete(raw, "command")
	}
	if v, ok := raw["prompt"]; ok {
		if err := json.Unmarshal(v, &r.Prompt); err != nil {
			return err
		}
		delete(raw, "prompt")
	}
	if v, ok := raw["timeout"]; ok {
		if err := json.Unmarshal(v, &r.Timeout); err != nil {
			return err
		}
		delete(raw, "timeout")
	}
	if v, ok := raw["matcher"]; ok {
		if err := json.Unmarshal(v, &r.Matcher); err != nil {
			return err
		}
		delete(raw, "matcher")
	}

	if len(raw) > 0 {
		r.ExtraFields = make(map[string]any, len(raw))
		for key, value := range raw {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				r.ExtraFields[key] = string(value)
			} else {
				r.ExtraFields[key] = decoded
			}
		}
	}
	return nil
}

func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []diag.Diagnostic, error) {
	if err := validatePath(path); err != nil {
		return nil, nil, err
	}

	data, err := fs.ReadFile(src, path)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: read %s: %w", path, err)
	}

	var doc struct {
		Hooks map[string][]hookDefinition `json:"hooks"`
	}
	if err := jsonc.Unmarshal(path, data, &doc); err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: parse: %w", err)
	}

	var hooks []*ir.Hook
	var diags []diag.Diagnostic
	events := make([]string, 0, len(doc.Hooks))
	for event := range doc.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	usedNames := make(map[string]bool)
	for _, event := range events {
		var rules []hookRule
		for _, entry := range doc.Hooks[event] {
			if entry.Flat != nil {
				rules = append(rules, *entry.Flat)
				continue
			}
			for _, rule := range entry.Hooks {
				if rule.Matcher != "" {
					if rule.ExtraFields == nil {
						rule.ExtraFields = make(map[string]any)
					}
					rule.ExtraFields["matcher"] = rule.Matcher
				}
				rule.Matcher = entry.Matcher
				rule.GroupExtra = entry.GroupExtra
				rules = append(rules, rule)
			}
		}
		for _, rule := range rules {
			hookType := rule.Type
			if hookType == "" {
				hookType = "command"
			}

			// Native provenance (design D7): keep the Gemini event name,
			// matcher, millisecond timeout, and command spelling as authored
			// and record HookSource so the catalog resolver sees Gemini
			// values instead of early-canonicalized ones.
			if !hookpolicy.IsDeclaredEvent(mapping.TargetGemini, event) {
				diags = append(diags, diag.Diagnostic{
					Code:     diag.CodeIngestGeminiHookEventPartialMapping,
					Severity: diag.SeverityWarning,
					Resource: event,
					Message:  fmt.Sprintf("Gemini hook event %q is outside the documented hook vocabulary; preserving the original event name", event),
					Hint:     "Use a documented Gemini hook event",
					Source:   diag.SourceRef{File: path, Line: 0},
				})
			}

			extra := make(map[string]any, len(rule.ExtraFields))
			for key, value := range rule.ExtraFields {
				extra[key] = value
			}
			if len(extra) == 0 {
				extra = nil
			}

			// Hook names become catalog resource keys, so they keep the
			// historical canonical-event spelling (e.g. BeforeTool hooks are
			// named pretooluse-command) even though the IR event stays
			// native.
			nameEvent := event
			if canonical, _, known := hookpolicy.LegacyEvent(mapping.TargetGemini, event); known {
				nameEvent = canonical
			}
			baseName := strings.ToLower(strings.ReplaceAll(nameEvent, " ", "-")) + "-" + hookType
			name := baseName
			for suffix := 2; usedNames[name]; suffix++ {
				name = fmt.Sprintf("%s-%d", baseName, suffix)
			}
			usedNames[name] = true
			options := []ir.HookOption{ir.WithHookPrompt(rule.Prompt), ir.WithHookSource(string(mapping.TargetGemini))}
			if rule.Timeout != nil {
				options = append(options, ir.WithHookTimeoutPtr(rule.Timeout))
			}

			matcher := rule.Matcher
			hook := ir.NewHookWithMatcherOptions(name, event, matcher, rule.Command, hookType, extra, ir.SourceRef{File: path, Line: 0}, options...)
			if len(rule.GroupExtra) > 0 {
				hook.Ext.Gemini = &extpkg.Gemini{HookGroupExtra: rule.GroupExtra}
			}
			hooks = append(hooks, hook)
		}
	}

	return hooks, diags, nil
}

type mcpServerEntry struct {
	Command           string            `json:"command"`
	Args              []string          `json:"args"`
	Env               map[string]string `json:"env"`
	URL               string            `json:"url"`
	Transport         string            `json:"transport"`
	Headers           map[string]string `json:"headers"`
	Disabled          bool              `json:"disabled"`
	OAuthClientID     string            `json:"oauthClientId"`
	OAuthClientSecret string            `json:"oauthClientSecret"`
	OAuthResource     string            `json:"oauthResource"`
	DisabledSet       bool              `json:"-"`
	Extra             map[string]any    `json:"-"`
}

func (e *mcpServerEntry) UnmarshalJSON(data []byte) error {
	type known struct {
		Command           string            `json:"command"`
		Args              []string          `json:"args"`
		Env               map[string]string `json:"env"`
		URL               string            `json:"url"`
		Transport         string            `json:"transport"`
		Headers           map[string]string `json:"headers"`
		Disabled          bool              `json:"disabled"`
		OAuthClientID     string            `json:"oauthClientId"`
		OAuthClientSecret string            `json:"oauthClientSecret"`
		OAuthResource     string            `json:"oauthResource"`
	}
	var k known
	if err := jsonutil.Unmarshal("", data, &k); err != nil {
		return err
	}
	e.Command, e.Args, e.Env = k.Command, k.Args, k.Env
	e.URL, e.Transport, e.Headers, e.Disabled = k.URL, k.Transport, k.Headers, k.Disabled
	e.OAuthClientID, e.OAuthClientSecret, e.OAuthResource = k.OAuthClientID, k.OAuthClientSecret, k.OAuthResource

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.DisabledSet = false
	e.Extra = nil
	knownKeys := []string{"command", "args", "env", "url", "transport", "headers", "disabled", "oauthClientId", "oauthClientSecret", "oauthResource"}
	for key := range raw {
		for _, knownKey := range knownKeys {
			if !strings.EqualFold(key, knownKey) {
				continue
			}
			if knownKey == "disabled" {
				e.DisabledSet = true
			}
			delete(raw, key)
			break
		}
	}
	if len(raw) > 0 {
		e.Extra = make(map[string]any, len(raw))
		for key, value := range raw {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				e.Extra[key] = string(value)
			} else {
				e.Extra[key] = decoded
			}
		}
	}
	return nil
}

func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, []diag.Diagnostic, error) {
	if err := validatePath(path); err != nil {
		return nil, nil, err
	}

	data, err := fs.ReadFile(src, path)
	if err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: read %s: %w", path, err)
	}

	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := jsonc.Unmarshal(path, data, &doc); err != nil {
		return nil, nil, fmt.Errorf("ingest/gemini: parse: %w", err)
	}

	servers := make([]*ir.McpServer, 0, len(doc.McpServers))
	names := make([]string, 0, len(doc.McpServers))
	for name := range doc.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		var entry mcpServerEntry
		if err := json.Unmarshal(doc.McpServers[name], &entry); err != nil {
			return nil, nil, fmt.Errorf("ingest/gemini: parse mcp server %q: %w", name, err)
		}
		options := []ir.McpServerOption{ir.WithMcpURL(normalizeMcpString(entry.URL)), ir.WithMcpTransport(entry.Transport), ir.WithMcpHeaders(normalizeMcpStringMap(entry.Headers)), ir.WithMcpOAuth(normalizeMcpString(entry.OAuthClientID), normalizeMcpString(entry.OAuthClientSecret), normalizeMcpString(entry.OAuthResource))}
		if entry.DisabledSet {
			options = append(options, ir.WithMcpDisabled(entry.Disabled))
		}
		extra, _ := normalizeMcpValue(entry.Extra).(map[string]any)
		args, _ := normalizeMcpValue(normalizeStringSlice(entry.Args)).([]string)
		servers = append(servers, ir.NewMcpServerWithOptions(name, normalizeMcpString(entry.Command), args, normalizeMcpStringMap(entry.Env), extra, ir.SourceRef{File: path, Line: 0}, options...))
	}

	return servers, nil, nil
}

func loadManifest(src fs.FS) (ir.Manifest, error) {
	// Registry-supplied manifest: duplicate keys are rejected before decoding
	// because name/version feed ownership markers and output paths.
	var raw map[string]any
	if err := ingest.LoadJSONManifest(src, extensionManifestPath, &raw); err != nil {
		return ir.Manifest{}, fmt.Errorf("ingest/gemini: load %s: %w", extensionManifestPath, err)
	}

	manifest := ir.Manifest{}
	if name, _ := raw["name"].(string); name != "" {
		manifest.Name = name
	}
	if description, _ := raw["description"].(string); description != "" {
		manifest.Description = description
	}
	if version, _ := raw["version"].(string); version != "" {
		manifest.Version = version
	}

	delete(raw, "name")
	delete(raw, "description")
	delete(raw, "version")
	delete(raw, "mcpServers")
	if len(raw) > 0 {
		manifest.Extra = raw
	}

	return manifest, nil
}

func hasWorkspaceMarkers(src fs.FS) bool {
	if _, err := fs.Stat(src, ".gemini/gemini-extension.json"); err == nil {
		return true
	}
	if _, err := fs.Stat(src, settingsPath); err == nil {
		return true
	}
	if _, err := fs.Stat(src, workspaceMCPPath); err == nil {
		return true
	}
	if entries, err := fs.ReadDir(src, geminiSkillsDir); err == nil && len(entries) > 0 {
		return true
	}
	if entries, err := fs.ReadDir(src, agentsSkillsDir); err == nil && len(entries) > 0 {
		return true
	}
	if entries, err := fs.ReadDir(src, geminiAgentsDir); err == nil && len(entries) > 0 {
		return true
	}
	if entries, err := fs.ReadDir(src, geminiCommandsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
				return true
			}
		}
	}
	return false
}

func validatePath(p string) error {
	if p == "" {
		return fmt.Errorf("path must not be empty")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("path must be relative: %s", p)
	}
	clean := path.Clean(p)
	if strings.HasPrefix(clean, "../") || clean == ".." {
		return fmt.Errorf("path must stay within the source FS: %s", p)
	}
	return nil
}

func listSubdirs(src fs.FS, root string) ([]string, error) {
	entries, err := ingest.ReadSkillEntries(src, root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, path.Join(root, entry.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func listFilesWithSuffix(src fs.FS, root, suffix string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			files = append(files, path.Join(root, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func requiredStringField(fields map[string]any, key, fallback string) (string, error) {
	value, ok := fields[key]
	if !ok {
		return fallback, nil
	}
	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, value)
	}
	return stringValue, nil
}

func toFloat64Ptr(value any) (*float64, error) {
	if value == nil {
		return nil, fmt.Errorf("value must be a number, not null")
	}
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int8:
		number = float64(typed)
	case int16:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint8:
		number = float64(typed)
	case uint16:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	default:
		return nil, fmt.Errorf("value must be a number, got %T", value)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, fmt.Errorf("value must be finite")
	}
	return &number, nil
}

func toNativeIntPtr(value any) (*int, error) {
	if value == nil {
		return nil, fmt.Errorf("value must be an integer, not null")
	}
	var number int64
	switch typed := value.(type) {
	case int:
		return intPtr(typed), nil
	case int8:
		return intPtr(int(typed)), nil
	case int16:
		return intPtr(int(typed)), nil
	case int32:
		return intPtr(int(typed)), nil
	case int64:
		number = typed
	case uint:
		if uint64(typed) > uint64(^uint(0)>>1) {
			return nil, fmt.Errorf("integer out of range")
		}
		number = int64(typed)
	case uint8:
		number = int64(typed)
	case uint16:
		number = int64(typed)
	case uint32:
		number = int64(typed)
	case uint64:
		if typed > uint64(^uint(0)>>1) {
			return nil, fmt.Errorf("integer out of range")
		}
		number = int64(typed)
	case float64:
		return toIntPtr(value)
	case string:
		return toIntPtr(value)
	default:
		return nil, fmt.Errorf("value must be an integer, got %T", value)
	}
	if int64(int(number)) != number {
		return nil, fmt.Errorf("integer out of range")
	}
	return intPtr(int(number)), nil
}

func parseInlineMCPServers(value any) (map[string]extpkg.GeminiMCPServer, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("value must be an object, got %T", value)
	}
	servers := make(map[string]extpkg.GeminiMCPServer, len(fields))
	for name, raw := range fields {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("server %q must be an object, got %T", name, raw)
		}
		server := extpkg.GeminiMCPServer{}
		var err error
		if server.Command, err = requiredStringField(entry, "command", ""); err != nil {
			return nil, fmt.Errorf("server %q command: %w", name, err)
		}
		if server.URL, err = requiredStringField(entry, "url", ""); err != nil {
			return nil, fmt.Errorf("server %q url: %w", name, err)
		}
		if rawArgs, present := entry["args"]; present {
			server.Args, err = strictStringSlice(rawArgs)
			if err != nil {
				return nil, fmt.Errorf("server %q args: %w", name, err)
			}
		}
		if rawEnv, present := entry["env"]; present {
			server.Env, err = strictStringMap(rawEnv)
			if err != nil {
				return nil, fmt.Errorf("server %q env: %w", name, err)
			}
		}
		for key, value := range entry {
			switch key {
			case "command", "url", "args", "env":
			default:
				if server.Extra == nil {
					server.Extra = make(map[string]any)
				}
				server.Extra[key] = value
			}
		}
		server.Command = normalizeEnvTokens(server.Command)
		server.Args = normalizeStringSlice(server.Args)
		server.Env = normalizeStringMap(server.Env)
		server.URL = normalizeEnvTokens(server.URL)
		servers[name] = server
	}
	return servers, nil
}

func strictStringSlice(value any) ([]string, error) {
	values, ok := value.([]any)
	if !ok {
		if values, ok := value.([]string); ok {
			return append(make([]string, 0, len(values)), values...), nil
		}
		return nil, fmt.Errorf("value must be a string list, got %T", value)
	}
	result := make([]string, len(values))
	for i, item := range values {
		stringValue, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("list item %d has unsupported type %T", i, item)
		}
		result[i] = stringValue
	}
	return result, nil
}

func strictStringMap(value any) (map[string]string, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		if values, ok := value.(map[string]string); ok {
			result := make(map[string]string, len(values))
			for key, item := range values {
				result[key] = item
			}
			return result, nil
		}
		return nil, fmt.Errorf("value must be an object, got %T", value)
	}
	result := make(map[string]string, len(fields))
	for key, raw := range fields {
		stringValue, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("value for %q must be a string, got %T", key, raw)
		}
		result[key] = stringValue
	}
	return result, nil
}

func skillPermissions(value any) (ir.Permissions, error) {
	values, ok := value.(map[string]any)
	if !ok {
		return ir.Permissions{}, fmt.Errorf("value must be an object")
	}
	result := ir.Permissions{}
	for key, raw := range values {
		if key != "allow" && key != "ask" && key != "deny" {
			if result.Extra == nil {
				result.Extra = make(map[string]any)
			}
			result.Extra[key] = raw
			continue
		}
		list, err := toStringSlice(raw)
		if err != nil {
			return ir.Permissions{}, fmt.Errorf("%s: %w", key, err)
		}
		switch key {
		case "allow":
			result.Allow = list
		case "ask":
			result.Ask = list
		case "deny":
			result.Deny = list
		}
	}
	return result, nil
}

func toIntPtr(value any) (*int, error) {
	if value == nil {
		return nil, nil
	}
	var n int64
	switch v := value.(type) {
	case int:
		return &v, nil
	case int64:
		n = v
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("value must be a finite integer")
		}
		if v < float64(math.MinInt) || v >= -float64(math.MinInt) {
			return nil, fmt.Errorf("integer out of range")
		}
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("value must be an integer")
		}
		n = int64(v)
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value must be an integer")
		}
		n = parsed
	default:
		return nil, fmt.Errorf("value has unsupported type %T", value)
	}
	if n < math.MinInt || n > math.MaxInt {
		return nil, fmt.Errorf("integer out of range")
	}
	return intPtr(int(n)), nil
}

func intPtr(v int) *int { return &v }

func toStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case string:
		return []string{typed}, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, len(typed))
		for i, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("list item %d has unsupported type %T", i, item)
			}
			result[i] = s
		}
		return result, nil
	case nil:
		return nil, fmt.Errorf("value must be a string list, not null")
	default:
		return nil, fmt.Errorf("value has unsupported type %T", value)
	}
}

func extraFields(fm map[string]any, known []string) map[string]any {
	if len(fm) == 0 {
		return nil
	}
	knownSet := make(map[string]struct{}, len(known))
	for _, key := range known {
		knownSet[key] = struct{}{}
	}

	extra := make(map[string]any)
	for key, value := range fm {
		if _, ok := knownSet[key]; ok {
			continue
		}
		extra[key] = value
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func normalizeEnvTokens(value string) string {
	return strings.ReplaceAll(value, geminiExtensionToken, claudePluginRootToken)
}

// normalizeMcpString canonicalizes a Gemini settings or extension MCP string:
// Gemini expands a bare $NAME, which canonical values write as ${NAME}.
func normalizeMcpString(value string) string {
	return mapping.BraceBareEnvReferences(normalizeEnvTokens(value))
}

func normalizeMcpStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = normalizeMcpString(value)
	}
	return out
}

func normalizeMcpValue(value any) any {
	switch typed := value.(type) {
	case string:
		return mapping.BraceBareEnvReferences(typed)
	case []string:
		if typed == nil {
			return typed
		}
		out := make([]string, len(typed))
		for i, item := range typed {
			out[i] = mapping.BraceBareEnvReferences(item)
		}
		return out
	case []any:
		if typed == nil {
			return typed
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeMcpValue(item)
		}
		return out
	case map[string]any:
		if typed == nil {
			return typed
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeMcpValue(item)
		}
		return out
	default:
		return value
	}
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = normalizeEnvTokens(value)
	}
	return out
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = normalizeEnvTokens(value)
	}
	return out
}

func fileExists(src fs.FS, p string) bool {
	_, err := fs.Stat(src, p)
	return err == nil
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func pathpkgBase(p string) string {
	return path.Base(p)
}
