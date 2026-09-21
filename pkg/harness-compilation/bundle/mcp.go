package bundle

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// LoadMcpServers reads a .mcp.json file from src at the given path.
// It parses { "mcpServers": { "<name>": { "command": ..., "args": [...], "env": {...} } } }
// and returns one *ir.McpServer per entry.
// Unknown fields on each server entry are preserved in the Extra map.
//
// JSON5-style single-line comments (// ...) are stripped before parsing
// as a convenience for config files that contain commented-out entries.
func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	data, err := fs.ReadFile(src, path)
	if err != nil {
		return nil, fmt.Errorf("bundle: read %s: %w", path, err)
	}

	// jsonc.Unmarshal is one pass that ignores comments and rejects duplicate
	// object keys. It replaces the previous stripJSONComments-then-validate
	// pair: the contract is unchanged (a commented-out duplicate is not a
	// duplicate, a real duplicate is rejected before any encoding/json decode
	// sees the bytes), but there is no second set of bytes for the two rules to
	// disagree about. Block and inline comments are now handled too.
	var mcpFile struct {
		McpServers json.RawMessage `json:"mcpServers"`
	}
	if err := jsonc.Unmarshal(path, data, &mcpFile); err != nil {
		return nil, fmt.Errorf("bundle: parse: %w", err)
	}

	if mcpFile.McpServers == nil {
		// The top-level mcpServers key is absent: nothing to load.
		return nil, nil
	}

	var mcpServers map[string]json.RawMessage
	if err := json.Unmarshal(mcpFile.McpServers, &mcpServers); err != nil {
		return nil, fmt.Errorf("bundle: %s: mcpServers is not an object: %w", path, err)
	}
	if mcpServers == nil {
		return nil, fmt.Errorf("bundle: %s: mcpServers is null", path)
	}

	var servers []*ir.McpServer
	names := make([]string, 0, len(mcpServers))
	for name := range mcpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := mcpServers[name]
		var entry mcpServerEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("bundle: parse mcpServer %q: %w", name, err)
		}

		if err := validateKebabName(name); err != nil {
			return nil, err
		}

		source := ir.SourceRef{File: path, Line: 0}
		options := []ir.McpServerOption{ir.WithMcpURL(entry.URL), ir.WithMcpTransport(entry.Transport), ir.WithMcpHeaders(entry.Headers), ir.WithMcpOAuth(entry.OAuthClientID, entry.OAuthClientSecret, entry.OAuthResource)}
		if entry.DisabledSet {
			options = append(options, ir.WithMcpDisabled(entry.Disabled))
		}
		s := ir.NewMcpServerWithOptions(name, entry.Command, entry.Args, entry.Env, entry.Extra, source, options...)
		servers = append(servers, s)
	}

	return servers, nil
}

// CanonicalRootToken is the canonical IR spelling of a source-harness
// plugin-root reference. Ingesters rewrite their native token to this form at
// the IR boundary so target translators can render the destination spelling.
const CanonicalRootToken = "${CLAUDE_PLUGIN_ROOT}"

// NormalizeMcpServerRootToken rewrites a harness-native plugin-root token to
// the canonical IR spelling across the server's command, args, env values,
// URL, and header values — the same root-reference normalization that hook
// ingestion already applies to hook commands.
func NormalizeMcpServerRootToken(server *ir.McpServer, nativeToken string) {
	replacer := strings.NewReplacer(nativeToken, CanonicalRootToken)
	server.Command = replacer.Replace(server.Command)
	for i, arg := range server.Args {
		server.Args[i] = replacer.Replace(arg)
	}
	for key, value := range server.Env {
		server.Env[key] = replacer.Replace(value)
	}
	server.URL = replacer.Replace(server.URL)
	for key, value := range server.Headers {
		server.Headers[key] = replacer.Replace(value)
	}
}

// mcpServerEntry captures the known fields of a single MCP server entry
// and preserves unknown fields in Extra.
type mcpServerEntry struct {
	Command           string            `json:"command"`
	Args              []string          `json:"args"`
	Env               map[string]string `json:"env"`
	URL               string            `json:"url"`
	Transport         string            `json:"transport"`
	Headers           map[string]string `json:"headers"`
	Disabled          bool              `json:"disabled"`
	DisabledSet       bool              `json:"-"`
	OAuthClientID     string            `json:"oauthClientId"`
	OAuthClientSecret string            `json:"oauthClientSecret"`
	OAuthResource     string            `json:"oauthResource"`
	Extra             map[string]any    `json:"-"`
}

// UnmarshalJSON implements custom unmarshalling to separate known from extra fields.
func (e *mcpServerEntry) UnmarshalJSON(data []byte) error {
	// Unmarshal known fields first.
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
	e.Command = k.Command
	e.Args = k.Args
	e.Env = k.Env
	e.URL, e.Transport, e.Headers, e.Disabled = k.URL, k.Transport, k.Headers, k.Disabled
	e.OAuthClientID, e.OAuthClientSecret, e.OAuthResource = k.OAuthClientID, k.OAuthClientSecret, k.OAuthResource

	// Now detect extra fields.
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	e.DisabledSet = false
	e.Extra = nil
	knownKeys := []string{"command", "args", "env", "url", "transport", "headers", "disabled", "oauthClientId", "oauthClientSecret", "oauthResource"}
	for key := range all {
		for _, knownKey := range knownKeys {
			if !strings.EqualFold(key, knownKey) {
				continue
			}
			if knownKey == "disabled" {
				e.DisabledSet = true
			}
			delete(all, key)
			break
		}
	}
	if len(all) > 0 {
		e.Extra = make(map[string]any, len(all))
		for key, raw := range all {
			var val any
			if err := json.Unmarshal(raw, &val); err != nil {
				e.Extra[key] = string(raw)
			} else {
				e.Extra[key] = val
			}
		}
	}

	return nil
}
