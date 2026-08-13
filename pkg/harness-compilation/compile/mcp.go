package compile

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// McpServerEntry builds the canonical MCP server entry map and the set of
// canonical field names used for ExtraKeys. The replace function is applied to
// the string fields that may carry env/path tokens (command, url, and the
// string values inside args, env, and headers). Transport and the OAuth trio are
// emitted verbatim, matching the existing translator behavior.
// When preserveEmptyCollections is true, empty (but non-nil) args/env/headers are
// emitted and marked as canonical, matching the Claude/Factory/Cursor behavior.
// When false, empty collections are omitted, matching Gemini's current behavior.
func McpServerEntry(server *ir.McpServer, replace func(string) string, preserveEmptyCollections bool) (map[string]any, map[string]struct{}) {
	entry := make(map[string]any, 10)
	canonical := make(map[string]struct{}, 10)

	if server.Command != "" {
		entry["command"] = replace(server.Command)
		canonical["command"] = struct{}{}
	}
	if args := mcpStringSlice(server.Args, replace, preserveEmptyCollections); args != nil {
		entry["args"] = args
		canonical["args"] = struct{}{}
	}
	if env := mcpStringMap(server.Env, replace, preserveEmptyCollections); env != nil {
		entry["env"] = env
		canonical["env"] = struct{}{}
	}
	if server.URL != "" {
		entry["url"] = replace(server.URL)
		canonical["url"] = struct{}{}
	}
	if server.Transport != "" {
		entry["transport"] = server.Transport
		canonical["transport"] = struct{}{}
	}
	if headers := mcpStringMap(server.Headers, replace, preserveEmptyCollections); headers != nil {
		entry["headers"] = headers
		canonical["headers"] = struct{}{}
	}
	if server.DisabledSet || server.Disabled {
		entry["disabled"] = server.Disabled
		canonical["disabled"] = struct{}{}
	}
	if server.OAuthClientID != "" {
		entry["oauthClientId"] = server.OAuthClientID
		canonical["oauthClientId"] = struct{}{}
	}
	if server.OAuthClientSecret != "" {
		entry["oauthClientSecret"] = server.OAuthClientSecret
		canonical["oauthClientSecret"] = struct{}{}
	}
	if server.OAuthResource != "" {
		entry["oauthResource"] = server.OAuthResource
		canonical["oauthResource"] = struct{}{}
	}

	return entry, canonical
}

func mcpStringSlice(values []string, replace func(string) string, preserveEmpty bool) []string {
	if values == nil {
		return nil
	}
	if !preserveEmpty && len(values) == 0 {
		return nil
	}
	replaced := make([]string, len(values))
	for i, v := range values {
		replaced[i] = replace(v)
	}
	return replaced
}

func mcpStringMap(values map[string]string, replace func(string) string, preserveEmpty bool) map[string]string {
	if values == nil {
		return nil
	}
	if !preserveEmpty && len(values) == 0 {
		return nil
	}
	replaced := make(map[string]string, len(values))
	for k, v := range values {
		replaced[k] = replace(v)
	}
	return replaced
}
