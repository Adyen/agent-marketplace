package pluginexport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

const (
	pluginSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	mcpSchemaID    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

type Options struct {
	AllowLoss bool
}

type File struct {
	Path       string
	Bytes      []byte
	Executable bool
}

type Loss struct {
	Path   string
	Reason string
}

type Result struct {
	Files        []File
	Losses       []Loss
	Restrictions []Loss
}

type fileState struct {
	file     ir.PluginFile
	consumed map[string]bool
	output   File
	lost     bool
}

// Export computes a complete Agent Plugins package without writing to disk.
func Export(plugin *ir.Plugin, source fs.FS, options Options) (Result, error) {
	result := Result{}
	info, known, err := pluginpolicy.LookupPackageFormat(plugin.Format)
	layout, hasLayout := target.LookupPackageLayout(plugin.Format)
	if err != nil || !known || !hasLayout || info.Role != pluginpolicy.RolePackage {
		return result, exportError(diag.CodePluginFormatUnsupported, "", "source format is not a plugin package")
	}
	if err := ir.ValidatePluginName("agent-plugin", plugin.Manifest.Name); err != nil {
		addLoss(&result.Losses, "plugin.json:name", "name is not valid for Agent Plugins 1.0.0")
		return result, exportError(diag.CodePluginExportLoss, "plugin.json", "plugin name cannot be represented")
	}

	files := make(map[string]*fileState, len(plugin.Files))
	for _, file := range plugin.Files {
		state := &fileState{file: file}
		files[file.Path] = state
		if !fs.ValidPath(file.Path) || file.Path == "." {
			addLoss(&result.Losses, file.Path, "package file path is invalid")
			state.lost = true
		}
	}
	manifest, manifestLosses := exportManifest(plugin, source, layout.ManifestPath)
	for _, loss := range manifestLosses {
		addLoss(&result.Losses, loss.Path, loss.Reason)
	}

	markLost := func(sourcePath string) {
		for filePath, state := range files {
			if (filePath == sourcePath || strings.HasPrefix(filePath, strings.TrimSuffix(sourcePath, "/")+"/")) &&
				len(state.consumed) == 0 && state.output.Path == "" {
				state.lost = true
			}
		}
	}
	consume := func(sourcePath, artifact string) {
		state := files[sourcePath]
		if state == nil || !fs.ValidPath(sourcePath) || sourcePath == "." {
			addLoss(&result.Losses, sourcePath, "source artifact file is absent from the package file inventory")
			return
		}
		if state.consumed == nil {
			state.consumed = map[string]bool{}
		}
		state.consumed[artifact] = true
		state.lost = false
	}
	if files[layout.ManifestPath] != nil && !files[layout.ManifestPath].lost {
		consume(layout.ManifestPath, "plugin.json")
	}
	skillDirectories, skillContents := map[string]string{}, map[string][]byte{}
	mcpServers := map[string]any{}
	for _, invalid := range plugin.Invalid {
		addLoss(&result.Losses, invalid.Path, "source content was skipped during package decoding")
		markLost(invalid.Path)
	}

	for _, member := range plugin.Members {
		if member.Resource == nil {
			addLoss(&result.Losses, "plugin member", "member has no resource data")
			continue
		}
		switch resource := member.Resource.(type) {
		case *ir.Skill:
			sourceFile := resource.Source.File
			if sourceFile == "" {
				sourceFile = path.Join("skills", resource.Name(), "SKILL.md")
			}
			dir := path.Dir(sourceFile)
			if !fs.ValidPath(sourceFile) || !strings.HasPrefix(dir, "skills/") || strings.Contains(strings.TrimPrefix(dir, "skills/"), "/") {
				addLoss(&result.Losses, member.Key(), "skill is not an immediate child of skills/")
				markLost(dir)
				continue
			}
			if !validAgentSkillName(resource.Name()) {
				addLoss(&result.Losses, member.Key(), "skill name is invalid for Agent Skills")
				markLost(dir)
				continue
			}
			skillDirectories[dir] = resource.Name()
			frontmatterLosses, skillSource, err := skillLosses(resource, source, sourceFile)
			for _, loss := range frontmatterLosses {
				addLoss(&result.Losses, loss.Path, loss.Reason)
			}
			if err != nil {
				addLoss(&result.Losses, sourceFile, "skill definition cannot be read or parsed")
				markLost(dir)
				delete(skillDirectories, dir)
				continue
			}
			if len(frontmatterLosses) > 0 {
				skillSource, err = rewriteSkill(skillSource)
				if err != nil {
					addLoss(&result.Losses, sourceFile, "skill definition cannot be rewritten safely")
					markLost(dir)
					delete(skillDirectories, dir)
					continue
				}
			}
			skillContents[dir] = skillSource
			if resource.PermissionsSet {
				addLoss(&result.Restrictions, sourceFile+":permissions", "skill permissions are not represented by Agent Skills")
			}
			if resource.Triggers != nil && !slices.Contains(resource.Triggers, "model") {
				addLoss(&result.Restrictions, sourceFile+":triggers", "skill triggers exclude model invocation")
			}
		case *ir.McpServer:
			entry, losses, restriction, representable := exportMcpServer(resource, plugin.Format)
			for _, loss := range losses {
				addLoss(&result.Losses, loss.Path, loss.Reason)
			}
			if restriction != nil {
				addLoss(&result.Restrictions, restriction.Path, restriction.Reason)
			}
			if representable {
				mcpServers[resource.Name()] = entry
				if resource.Source.File != "" {
					consume(resource.Source.File, "mcp.json")
				}
			} else if resource.Source.File != "" {
				markLost(resource.Source.File)
			}
		default:
			omitMemberFiles(markLost, member.Resource)
			addLoss(&result.Losses, member.Key(), "member kind is not represented by Agent Plugins")
		}
	}

	mcpGenerated := len(mcpServers) > 0
	destinationSources := map[string]string{"plugin.json": "generated plugin.json"}
	if mcpGenerated {
		destinationSources["mcp.json"] = "generated mcp.json"
	}
	for _, sourcePath := range slices.Sorted(maps.Keys(files)) {
		state := files[sourcePath]
		if len(state.consumed) > 0 || state.lost {
			continue
		}
		if sourcePath == ".mcp.json" || (sourcePath == "mcp.json" && !mcpGenerated) {
			addLoss(&result.Losses, sourcePath, "source file is not represented by Agent Plugins")
			markLost(sourcePath)
			continue
		}
		dir, name := skillDirectory(sourcePath, skillDirectories)
		var data []byte
		if name != "" && sourcePath == path.Join(dir, "SKILL.md") {
			data = skillContents[dir]
		} else {
			var err error
			data, err = readPackageFile(source, sourcePath, plugindecode.MaxPackageFileBytes)
			if err != nil {
				addLoss(&result.Losses, sourcePath, "package file cannot be read")
				markLost(sourcePath)
				continue
			}
		}
		destPath := sourcePath
		if name != "" {
			destPath = path.Join("skills", name, strings.TrimPrefix(sourcePath, dir+"/"))
		}
		if previous, exists := destinationSources[destPath]; exists {
			reason := "source file conflicts with " + previous
			if !strings.HasPrefix(previous, "generated ") {
				reason = "multiple source files map to the same output path"
			}
			addLoss(&result.Losses, sourcePath, reason)
			markLost(sourcePath)
			continue
		}
		destinationSources[destPath] = sourcePath
		state.output = File{Path: destPath, Bytes: data, Executable: state.file.Executable}
	}
	for _, file := range plugin.Files {
		state := files[file.Path]
		if len(state.consumed) == 0 && state.output.Path == "" && !state.lost {
			addLoss(&result.Losses, file.Path, "source file is not represented by Agent Plugins")
			state.lost = true
		}
	}
	sortLosses(&result)
	if len(result.Restrictions) > 0 {
		return result, exportError(diag.CodePluginExportRestriction, "", "export would drop a security restriction")
	}
	if len(result.Losses) > 0 && !options.AllowLoss {
		return result, exportError(diag.CodePluginExportLoss, "", "package contains content Agent Plugins cannot represent")
	}

	output := make([]File, 0, len(files)+2)
	for _, state := range files {
		if state.output.Path != "" {
			output = append(output, state.output)
		}
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return result, fmt.Errorf("encode plugin.json: %w", err)
	}
	output = append(output, File{Path: "plugin.json", Bytes: append(manifestBytes, '\n')})
	if mcpGenerated {
		mcpBytes, err := json.MarshalIndent(map[string]any{"$schema": mcpSchemaID, "mcpServers": mcpServers}, "", "  ")
		if err != nil {
			return result, fmt.Errorf("encode mcp.json: %w", err)
		}
		output = append(output, File{Path: "mcp.json", Bytes: append(mcpBytes, '\n')})
	}
	slices.SortFunc(output, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	result.Files = output
	return result, nil
}

func exportManifest(plugin *ir.Plugin, source fs.FS, manifestPath string) (map[string]any, []Loss) {
	raw, err := readPackageFile(source, manifestPath, 1<<20)
	var authored map[string]any
	if err == nil && json.Unmarshal(raw, &authored) != nil {
		err = errors.New("invalid source manifest")
	}
	fields := map[string]any{"$schema": pluginSchemaID, "name": plugin.Manifest.Name}
	var losses []Loss
	if err != nil || authored == nil {
		return fields, []Loss{{Path: "plugin.json", Reason: "source manifest fields are unavailable"}}
	}
	for key, value := range authored {
		if key == "$schema" || key == "name" || key == "extensions" {
			continue
		}
		if !manifestFieldValid(key, value) {
			if !isSourceComponentField(key) {
				losses = append(losses, Loss{Path: "plugin.json:" + key, Reason: "manifest field is not represented by Agent Plugins"})
			}
			continue
		}
		fields[key] = value
	}
	if _, authoredExtensions := authored["extensions"]; authoredExtensions || plugin.Extensions != nil {
		losses = append(losses, Loss{Path: "plugin.json:extensions", Reason: "client extension data has no portable semantics"})
	}
	return fields, losses
}

func isSourceComponentField(name string) bool {
	switch name {
	case "skills", "commands", "agents", "droids", "hooks", "mcp", "mcpServers", "output-styles":
		return true
	default:
		return false
	}
}

func manifestFieldValid(key string, value any) bool {
	switch key {
	case "version", "description", "homepage", "repository", "license":
		_, ok := value.(string)
		return ok
	case "author":
		author, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for name, item := range author {
			if name != "name" && name != "email" && name != "url" {
				return false
			}
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case "keywords":
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func skillLosses(skill *ir.Skill, source fs.FS, file string) ([]Loss, []byte, error) {
	raw, err := readPackageFile(source, file, plugindecode.MaxPackageFileBytes)
	if err != nil {
		return nil, nil, err
	}
	fields, _, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(raw)))
	if err != nil {
		return nil, raw, err
	}
	allowed := map[string]bool{
		"name": true, "description": true, "license": true, "compatibility": true, "metadata": true, "allowed-tools": true,
	}
	var losses []Loss
	add := func(name string) {
		losses = append(losses, Loss{Path: file + ":" + name, Reason: "skill frontmatter field is not represented by Agent Skills"})
	}
	for key := range fields {
		if !allowed[key] {
			add(key)
			continue
		}
		if !skillFieldValid(key, fields[key]) {
			add(key)
		}
	}
	for key := range skill.Extra {
		if !allowed[key] {
			add(key)
		}
	}
	for key, value := range map[string]string{
		"argument-hint": skill.ArgumentHint, "model": skill.Model, "subagent": skill.Subagent, "agent": skill.Agent,
	} {
		if value != "" {
			add(key)
		}
	}
	if skill.Triggers != nil {
		add("triggers")
	}
	return losses, raw, nil
}

func skillFieldValid(key string, value any) bool {
	switch key {
	case "name", "description", "license", "compatibility":
		_, ok := value.(string)
		return ok
	case "allowed-tools":
		switch value.(type) {
		case string, []any:
			return true
		}
		return false
	case "metadata":
		metadata, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range metadata {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func exportMcpServer(server *ir.McpServer, sourceFormat string) (map[string]any, []Loss, *Loss, bool) {
	base := "mcpServers/" + server.Name()
	entry := map[string]any{}
	var losses []Loss
	add := func(field, reason string) {
		losses = append(losses, Loss{Path: base + "/" + field, Reason: reason})
	}
	if sourceFormat != "agent-plugin" {
		if hasNativePluginRootToken(server) {
			add("", "native plugin-root text would gain expansion meaning")
			return nil, losses, nil, false
		}
		server = normalizeNativeRootReferences(server)
	}
	transport, typeConsumed := server.Transport, false
	if sourceFormat != "agent-plugin" {
		if value, present := server.Extra["type"]; present {
			nativeType, ok := value.(string)
			if !ok {
				add("type", "MCP transport type is not recognized")
				return nil, losses, nil, false
			}
			switch nativeType {
			case "stdio":
				if server.Command == "" || server.URL != "" || transport != "" {
					add("type", "MCP transport type conflicts with its command and URL")
					return nil, losses, nil, false
				}
			case "http", "streamable-http", "sse":
				remoteType := nativeType
				if nativeType == "http" {
					remoteType = "streamable-http"
				}
				if server.URL == "" || server.Command != "" || (transport != "" && transport != remoteType) {
					add("type", "MCP transport type conflicts with its command and URL")
					return nil, losses, nil, false
				}
				transport = remoteType
			default:
				add("type", "MCP transport type is not recognized")
				return nil, losses, nil, false
			}
			typeConsumed = true
		}
	}
	cwd := server.Cwd
	cwdConsumed := false
	if value, present := server.Extra["cwd"]; present {
		if authored, ok := value.(string); ok && (sourceFormat != "agent-plugin" || server.Cwd != "") {
			if cwd != "" && cwd != authored {
				add("cwd", "MCP working directory has conflicting values")
				return nil, losses, nil, false
			}
			cwd, cwdConsumed = authored, true
		}
	}
	if sourceFormat != "agent-plugin" && mcpHasSourceInterpolation(server) {
		add("", "source-format interpolation would become literal")
		return nil, losses, nil, false
	}
	if server.OAuthClientID != "" {
		add("oauth.client_id", "OAuth data is not represented by Agent Plugins")
	}
	if server.OAuthClientSecret != "" {
		add("oauth.client_secret", "OAuth data is not represented by Agent Plugins")
	}
	if server.OAuthResource != "" {
		add("oauth.resource", "OAuth data is not represented by Agent Plugins")
	}
	for key := range server.Extra {
		if (key == "type" && typeConsumed) || (key == "cwd" && cwdConsumed) {
			continue
		}
		add(key, "MCP extension field is not represented by Agent Plugins")
	}
	if server.Ext != (ext.Bag{}) {
		add("extensions", "MCP format sidecar is not represented by Agent Plugins")
	}
	switch {
	case server.Command != "" && server.URL == "":
		command := server.Command
		if strings.HasPrefix(command, "${PLUGIN_ROOT}/") {
			command = "./" + strings.TrimPrefix(command, "${PLUGIN_ROOT}/")
		}
		if !agentCommand(command) {
			add("", "stdio command cannot retain its package-relative meaning")
			return nil, losses, nil, false
		}
		entry["type"], entry["command"] = "stdio", command
		if server.Args != nil {
			entry["args"] = server.Args
		}
		if server.Env != nil {
			env := make(map[string]string, len(server.Env))
			for key, value := range server.Env {
				if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" {
					add("env/"+key, "reserved runtime variable cannot be declared as an environment key")
					continue
				}
				env[key] = value
			}
			if len(env) > 0 {
				entry["env"] = env
			}
		}
		if cwd != "" {
			if cwd == "." {
				cwd = "./"
			}
			if !agentCwd(cwd) {
				add("cwd", "working directory cannot retain its package-root meaning")
			} else {
				entry["cwd"] = cwd
			}
		}
	case server.Command == "" && server.URL != "":
		if transport != "streamable-http" && transport != "sse" {
			add("", "remote transport is not represented by Agent Plugins")
			return nil, losses, nil, false
		}
		entry["type"], entry["url"] = transport, server.URL
		if server.Headers != nil {
			entry["headers"] = server.Headers
		}
	default:
		add("", "MCP server does not have one supported transport")
		return nil, losses, nil, false
	}
	if server.DisabledSet && server.Disabled {
		restriction := Loss{Path: base + "/disabled", Reason: "explicitly disabled MCP server cannot be enabled by export"}
		return entry, losses, &restriction, true
	}
	return entry, losses, nil, true
}

func hasNativePluginRootToken(server *ir.McpServer) bool {
	contains := func(value string) bool {
		return strings.Contains(value, "${PLUGIN_ROOT}") || strings.Contains(value, "${PLUGIN_DATA}")
	}
	if contains(server.Command) || contains(server.Cwd) {
		return true
	}
	for _, arg := range server.Args {
		if contains(arg) {
			return true
		}
	}
	for _, value := range server.Env {
		if contains(value) {
			return true
		}
	}
	if cwd, ok := server.Extra["cwd"].(string); ok {
		return contains(cwd)
	}
	return false
}

func normalizeNativeRootReferences(server *ir.McpServer) *ir.McpServer {
	normalized := *server
	replace := func(value string) string {
		return strings.ReplaceAll(value, bundle.CanonicalRootToken, "${PLUGIN_ROOT}")
	}
	normalized.Command = replace(server.Command)
	normalized.Cwd = replace(server.Cwd)
	normalized.Args = slices.Clone(server.Args)
	for i, arg := range normalized.Args {
		normalized.Args[i] = replace(arg)
	}
	normalized.Env = maps.Clone(server.Env)
	for key, value := range normalized.Env {
		normalized.Env[key] = replace(value)
	}
	normalized.Extra = maps.Clone(server.Extra)
	if cwd, ok := normalized.Extra["cwd"].(string); ok {
		normalized.Extra["cwd"] = replace(cwd)
	}
	return &normalized
}

func mcpHasSourceInterpolation(server *ir.McpServer) bool {
	conflicts := pluginpolicy.HasCanonicalInterpolationConflict
	if conflicts(server.Command) {
		return true
	}
	for _, arg := range server.Args {
		if conflicts(arg) {
			return true
		}
	}
	for _, value := range server.Env {
		if conflicts(value) {
			return true
		}
	}
	for _, value := range server.Headers {
		if conflicts(value) {
			return true
		}
	}
	if conflicts(server.Cwd) || conflicts(server.URL) {
		return true
	}
	if cwd, ok := server.Extra["cwd"].(string); ok && conflicts(cwd) {
		return true
	}
	return false
}

func agentCommand(command string) bool {
	if command == "" {
		return false
	}
	if strings.HasPrefix(command, "./") {
		clean := path.Clean(strings.TrimPrefix(command, "./"))
		return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
	}
	return !strings.ContainsAny(command, "/\\")
}

func agentCwd(cwd string) bool {
	for _, prefix := range []string{".", "${PLUGIN_ROOT}", "${PLUGIN_DATA}"} {
		if cwd == prefix {
			return true
		}
		if rest, ok := strings.CutPrefix(cwd, prefix+"/"); ok {
			return !slices.Contains(strings.Split(rest, "/"), "..")
		}
	}
	return false
}

func rewriteSkill(raw []byte) ([]byte, error) {
	fields, body, err := yamlfm.ParseYAMLFrontmatter(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	for key := range fields {
		if !skillFieldValid(key, fields[key]) {
			delete(fields, key)
		}
	}
	data, err := yaml.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(data) + "---\n" + body), nil
}

func readPackageFile(source fs.FS, name string, limit int64) ([]byte, error) {
	file, err := source.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("package input is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("package file exceeds the bounded read limit")
	}
	return data, nil
}

func skillDirectory(sourcePath string, directories map[string]string) (string, string) {
	for dir, name := range directories {
		if sourcePath == dir || strings.HasPrefix(sourcePath, dir+"/") {
			return dir, name
		}
	}
	return "", ""
}

func omitMemberFiles(markLost func(string), resource ir.Resource) {
	var file string
	switch typed := resource.(type) {
	case *ir.Command:
		file = typed.Source.File
	case *ir.Subagent:
		file = typed.Source.File
	case *ir.Hook:
		file = typed.Source.File
	case *ir.Rule:
		file = typed.Source.File
	}
	if file != "" {
		markLost(file)
	}
}

func validAgentSkillName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func addLoss(losses *[]Loss, file, reason string) {
	for _, loss := range *losses {
		if loss.Path == file {
			return
		}
	}
	*losses = append(*losses, Loss{Path: file, Reason: reason})
}

func sortLosses(result *Result) {
	sort := func(losses []Loss) {
		slices.SortFunc(losses, func(a, b Loss) int {
			if order := strings.Compare(a.Path, b.Path); order != 0 {
				return order
			}
			return strings.Compare(a.Reason, b.Reason)
		})
	}
	sort(result.Losses)
	sort(result.Restrictions)
}

func exportError(code, file, message string) error {
	return diag.NewCodedError(code, diag.SourceRef{File: file}, errors.New(message))
}
