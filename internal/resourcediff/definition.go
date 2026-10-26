package resourcediff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpinspection "github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

// Definition renders the normalized review definition of a resource: a
// deterministic, complete view of every field relevant to compilation or
// selection, derived from the canonical IR so equivalent resources acquired
// from Git and a Marketplace artifact render identically.
//
// The view is pretty-printed canonical JSON (map keys sorted) followed, for
// body-bearing kinds, by a blank line and the raw multiline body. Source
// locations, original comments, and authored formatting intentionally do not
// appear. The returned review path is the stable primary review path.
func Definition(r ir.Resource) (reviewPath string, doc string, err error) {
	if r == nil {
		return "", "", fmt.Errorf("review resource is nil")
	}
	var metadata map[string]any
	var body string
	switch typed := r.(type) {
	case *ir.Skill:
		metadata = skillMetadata(typed)
		body = typed.Body
	case *ir.Command:
		metadata = commandMetadata(typed)
		body = typed.Body
	case *ir.Subagent:
		metadata = subagentMetadata(typed)
		body = typed.Body
	case *ir.Hook:
		metadata = hookMetadata(typed)
	case *ir.McpServer:
		metadata = mcpServerMetadata(typed)
	case *ir.Collection:
		metadata = collectionMetadata(typed)
	case *ir.Rule:
		metadata = ruleMetadata(typed)
		body = typed.Body
	default:
		return "", "", fmt.Errorf("unsupported review resource %T", r)
	}
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("render normalized definition of %s %q: %w", r.Kind(), r.Name(), err)
	}
	if body == "" {
		return mpinspection.PrimaryPath(r.Kind().String()), string(encoded) + "\n", nil
	}
	out := string(encoded) + "\n\n" + body
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return mpinspection.PrimaryPath(r.Kind().String()), out, nil
}

func skillMetadata(s *ir.Skill) map[string]any {
	m := map[string]any{
		"name":        s.Name(),
		"description": s.Description,
	}
	setString(m, "argument-hint", s.ArgumentHint)
	setString(m, "model", s.Model)
	setString(m, "model-source", s.ModelSource)
	setStrings(m, "allowed-tools", s.AllowedTools)
	setString(m, "tool-source", s.ToolSource)
	if s.PermissionsSet {
		permissions := map[string]any{}
		setStrings(permissions, "allow", s.Permissions.Allow)
		setStrings(permissions, "ask", s.Permissions.Ask)
		setStrings(permissions, "deny", s.Permissions.Deny)
		if s.Permissions.Extra != nil {
			permissions["extra"] = normalizeValue(reflect.ValueOf(s.Permissions.Extra))
		}
		m["permissions"] = permissions
	}
	setStrings(m, "triggers", s.Triggers)
	setString(m, "subagent", s.Subagent)
	setString(m, "agent", s.Agent)
	setExt(m, s.Ext)
	setExtra(m, s.Extra)
	return m
}

func commandMetadata(c *ir.Command) map[string]any {
	m := map[string]any{
		"name":        c.Name(),
		"description": c.Description,
	}
	setString(m, "tool-source", c.ToolSource)
	setExt(m, c.Ext)
	setExtra(m, c.Extra)
	return m
}

func subagentMetadata(s *ir.Subagent) map[string]any {
	m := map[string]any{
		"name":        s.Name(),
		"description": s.Description,
	}
	setString(m, "model", s.Model)
	setString(m, "model-source", s.ModelSource)
	setStrings(m, "tools", s.Tools)
	setString(m, "tool-source", s.ToolSource)
	setString(m, "effort", s.Effort)
	setStrings(m, "disallowed-tools", s.DisallowedTools)
	setString(m, "isolation", s.Isolation)
	if s.MaxNesting != nil {
		m["max-nesting"] = *s.MaxNesting
	}
	setExt(m, s.Ext)
	setExtra(m, s.Extra)
	return m
}

func hookMetadata(h *ir.Hook) map[string]any {
	m := map[string]any{
		"name":  h.Name(),
		"event": h.Event,
		"type":  h.Type,
	}
	setString(m, "matcher", h.Matcher)
	setString(m, "command", h.Command)
	setString(m, "prompt", h.Prompt)
	if h.Timeout != nil {
		m["timeout"] = *h.Timeout
	}
	setString(m, "hook-source", h.HookSource)
	if len(h.GroupExtraKeys) > 0 {
		keys := make([]string, 0, len(h.GroupExtraKeys))
		for key := range h.GroupExtraKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		m["group-extra-keys"] = keys
	}
	setExt(m, h.Ext)
	setExtra(m, h.Extra)
	return m
}

func mcpServerMetadata(s *ir.McpServer) map[string]any {
	m := map[string]any{
		"name":    s.Name(),
		"command": s.Command,
	}
	setStrings(m, "args", s.Args)
	if s.Env != nil {
		m["env"] = normalizeValue(reflect.ValueOf(s.Env))
	}
	setString(m, "url", s.URL)
	setString(m, "transport", s.Transport)
	if s.Headers != nil {
		m["headers"] = normalizeValue(reflect.ValueOf(s.Headers))
	}
	if s.DisabledSet {
		m["disabled"] = s.Disabled
	}
	setString(m, "oauth-client-id", s.OAuthClientID)
	setString(m, "oauth-client-secret", s.OAuthClientSecret)
	setString(m, "oauth-resource", s.OAuthResource)
	setExt(m, s.Ext)
	setExtra(m, s.Extra)
	return m
}

func collectionMetadata(c *ir.Collection) map[string]any {
	m := map[string]any{
		"name": c.Name(),
	}
	setString(m, "version", c.Version)
	setString(m, "description", c.Description)
	if c.Members != nil {
		members := make([]any, len(c.Members))
		for i, member := range c.Members {
			entry := map[string]any{"name": member.Name}
			kind := member.Kind
			if kind == 0 {
				kind = ir.KindSkill
			}
			entry["kind"] = kind.String()
			setString(entry, "registry", member.Registry)
			setString(entry, "ref", member.Ref)
			setString(entry, "path", member.Path)
			members[i] = entry
		}
		m["members"] = members
	}
	setStrings(m, "includes", c.Includes)
	setExtra(m, c.Extra)
	return m
}

func ruleMetadata(r *ir.Rule) map[string]any {
	m := map[string]any{
		"name":        r.Name(),
		"description": r.Description,
	}
	setStrings(m, "globs", r.Globs)
	if r.AlwaysApply {
		m["always-apply"] = true
	}
	setExt(m, r.Ext)
	setExtra(m, r.Extra)
	return m
}

func setString(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func setStrings(m map[string]any, key string, values []string) {
	if values != nil {
		m[key] = values
	}
}

func setExt(m map[string]any, bag any) {
	ext := normalizeValue(reflect.ValueOf(bag))
	if extMap, ok := ext.(map[string]any); ok && len(extMap) > 0 {
		m["ext"] = extMap
	}
}

func setExtra(m map[string]any, extra map[string]any) {
	if extra != nil {
		m["extra"] = normalizeValue(reflect.ValueOf(extra))
	}
}

// normalizeValue converts an arbitrary IR-held value into JSON-encodable data
// deterministically: string-keyed maps (sorted on marshal), nil-preserving
// slices, and structs projected through their json tags. Values that JSON
// cannot carry (for example map[interface{}]interface{} from YAML parsers)
// are converted instead of failing the review.
func normalizeValue(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return normalizeValue(v.Elem())
	case reflect.Map:
		out := make(map[string]any, v.Len())
		keys := make([]reflect.Value, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			keys = append(keys, iter.Key())
		}
		sort.Slice(keys, func(i, j int) bool { return mapKeyLess(keys[i], keys[j]) })
		for _, key := range keys {
			out[mapKeyString(key)] = normalizeValue(v.MapIndex(key))
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range v.Len() {
			out[i] = normalizeValue(v.Index(i))
		}
		return out
	case reflect.Struct:
		out := map[string]any{}
		t := v.Type()
		for i := range t.NumField() {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = field.Name
			}
			value := v.Field(i)
			if strings.Contains(field.Tag.Get("json"), "omitempty") && value.IsZero() {
				continue
			}
			out[tag] = normalizeValue(value)
		}
		return out
	default:
		if !v.CanInterface() {
			return nil
		}
		switch typed := v.Interface().(type) {
		case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return typed
		default:
			return fmt.Sprintf("%v", typed)
		}
	}
}

func mapKeyString(key reflect.Value) string {
	if key.Kind() == reflect.String {
		return key.String()
	}
	return fmt.Sprintf("%v", key.Interface())
}

func mapKeyLess(a, b reflect.Value) bool {
	return mapKeyString(a) < mapKeyString(b)
}
