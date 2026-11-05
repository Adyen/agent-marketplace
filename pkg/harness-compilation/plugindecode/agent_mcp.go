package plugindecode

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// loadAgentMcp decodes an Agent Plugins 1.0.0 mcp.json against the bundled
// schema. §7.2.2 failure boundaries apply: an unreadable or top-level-invalid
// file disables MCP for the plugin while other component types load; one
// invalid server entry is skipped while valid siblings remain.
func (d *decoder) loadAgentMcp(file string) {
	data, err := d.readBounded(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		d.warn(diag.CodePluginFormatComponentInvalid,
			"mcp.json cannot be read; MCP is disabled for this plugin", file)
		d.record(file, "component", "unreadable", diag.CodePluginFormatComponentInvalid)
		return
	}
	var obj map[string]any
	if err := jsonc.Unmarshal(file, data, &obj); err != nil || obj == nil {
		d.warn(diag.CodePluginFormatComponentInvalid,
			"mcp.json is not a valid JSON object; MCP is disabled for this plugin", file)
		d.record(file, "component", "invalid-json", diag.CodePluginFormatComponentInvalid)
		return
	}
	schema, err := agentSchemas()
	if err != nil {
		return
	}
	if id, _ := obj["$schema"].(string); id != agentPluginMcpSchemaID {
		d.warn(diag.CodePluginFormatComponentInvalid,
			"mcp.json declares an Agent Plugins MCP schema identifier this decoder does not support; MCP is disabled for this plugin", file)
		d.record(file, "component", "version-unsupported", diag.CodePluginFormatComponentInvalid)
		return
	}
	servers, ok := obj["mcpServers"].(map[string]any)
	if !ok {
		d.warn(diag.CodePluginFormatComponentInvalid,
			"mcp.json lacks an mcpServers object; MCP is disabled for this plugin", file)
		d.record(file, "component", "invalid-shape", diag.CodePluginFormatComponentInvalid)
		return
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if server, ok := d.decodeAgentServer(name, servers[name], schema, file); ok {
			d.addMember(server, file)
		}
	}
}

// decodeAgentServer validates one mcp.json entry against the closed variants
// declared by the bundled schema and builds the leaf ir.McpServer. The
// boolean reports whether the entry is a member; a false result has already
// been diagnosed and recorded.
func (d *decoder) decodeAgentServer(name string, raw any, schema agentPluginSchema, file string) (*ir.McpServer, bool) {
	invalid := func(reason string) (*ir.McpServer, bool) {
		d.warn(diag.CodePluginMemberInvalid,
			fmt.Sprintf("MCP server entry %q is invalid (%s) and was skipped", name, reason), file)
		d.record(file, "entry", reason, diag.CodePluginMemberInvalid)
		return nil, false
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return invalid("not an object")
	}
	typ, _ := obj["type"].(string)
	variant := map[string]string{"stdio": "stdioServer", "streamable-http": "streamableHttpServer", "sse": "sseServer"}[typ]
	def, known := schema.serverDefs[variant]
	if variant == "" || !known {
		return invalid("unknown transport type")
	}
	for key := range obj {
		if _, allowed := def.properties[key]; !allowed {
			return invalid("unknown field")
		}
	}
	for _, required := range def.required {
		v, present := obj[required]
		if !present {
			return invalid("missing required field " + required)
		}
		switch required {
		case "type":
		case "command", "url":
			if s, isStr := v.(string); !isStr || s == "" {
				return invalid("required field " + required + " must be a non-empty string")
			}
		}
	}
	server := ir.NewMcpServer(name, "", nil, nil, nil, ir.SourceRef{File: file})
	if typ == "stdio" {
		command, _ := obj["command"].(string)
		if !validAgentCommand(command) {
			return invalid("command must be a bare executable token or a plugin-relative ./ path that stays inside the root")
		}
		server.Command = command
		if raw, present := obj["args"]; present {
			args, ok := stringSlice(raw)
			if !ok {
				return invalid("args must be a string array")
			}
			server.Args = args
		}
		if raw, present := obj["env"]; present {
			env, ok := stringMap(raw)
			if !ok {
				return invalid("env must be an object of strings")
			}
			for key := range env {
				if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" {
					return invalid("env must not contain the reserved names PLUGIN_ROOT or PLUGIN_DATA")
				}
			}
			server.Env = env
		}
		if raw, present := obj["cwd"]; present {
			cwd, isStr := raw.(string)
			if !isStr || schema.cwdPattern == nil || !schema.cwdPattern.MatchString(cwd) || !validAgentCwd(cwd) {
				return invalid("cwd is outside its permitted forms or escapes its root")
			}
			server.Cwd = cwd
			server.Extra = map[string]any{"cwd": cwd}
		}
		return server, true
	}
	url, _ := obj["url"].(string)
	server.URL = url
	server.Transport = typ
	if raw, present := obj["headers"]; present {
		headers, ok := stringMap(raw)
		if !ok {
			return invalid("headers must be an object of strings")
		}
		folded := map[string]bool{}
		for key := range headers {
			lower := strings.ToLower(key)
			if folded[lower] {
				return invalid("headers contain the same name under different casing")
			}
			folded[lower] = true
		}
		server.Headers = headers
	}
	return server, true
}

// validAgentCommand implements §7.2.1: a bare executable token or a
// plugin-relative path beginning with "./" that stays inside the plugin root.
func validAgentCommand(command string) bool {
	if command == "" {
		return false
	}
	if strings.HasPrefix(command, "./") {
		clean := path.Clean(strings.TrimPrefix(command, "./"))
		return clean != ".." && !strings.HasPrefix(clean, "../") && clean != "."
	}
	return !strings.ContainsAny(command, "/\\")
}

// validAgentCwd checks the post-pattern containment rule: plugin-relative
// and ${PLUGIN_ROOT}-rooted values stay inside the root; ${PLUGIN_DATA}
// values stay inside the data root. Any ".." element escapes.
func validAgentCwd(cwd string) bool {
	rest := cwd
	for _, prefix := range []string{"./", "${PLUGIN_ROOT}/", "${PLUGIN_DATA}/"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimPrefix(rest, prefix)
			break
		}
	}
	if rest == "${PLUGIN_ROOT}" || rest == "${PLUGIN_DATA}" {
		rest = ""
	}
	for _, element := range strings.Split(rest, "/") {
		if element == ".." {
			return false
		}
	}
	return true
}

func stringSlice(raw any) ([]string, bool) {
	list, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func stringMap(raw any) (map[string]string, bool) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out[k] = s
	}
	return out, true
}
