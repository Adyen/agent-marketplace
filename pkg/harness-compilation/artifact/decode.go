package artifact

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// DecodeResource reconstructs an ir.Resource from the canonical JSON payload
// produced by resourceJSON. kind must be one of the Marketplace-selectable
// resource kind strings: skill, command, subagent, hook, or mcpserver.
func DecodeResource(kind string, payload []byte) (ir.Resource, error) {
	k, ok := canonicalToIRKind(kind)
	if !ok {
		return nil, fmt.Errorf("unsupported resource kind %q for DecodeResource", kind)
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("decode resource payload: %w", err)
	}

	name, _ := raw["name"].(string)
	if err := ir.ValidateName(name); err != nil {
		return nil, fmt.Errorf("resource payload name: %w", err)
	}

	var resource ir.Resource
	switch k {
	case ir.KindSkill:
		resource = &ir.Skill{}
	case ir.KindCommand:
		resource = &ir.Command{}
	case ir.KindSubagent:
		resource = &ir.Subagent{}
	case ir.KindHook:
		resource = &ir.Hook{}
	case ir.KindMcpServer:
		resource = &ir.McpServer{}
	default:
		return nil, fmt.Errorf("unsupported resource kind %q for DecodeResource", kind)
	}

	extra, err := populateStructFromMap(raw, resource)
	if err != nil {
		return nil, fmt.Errorf("populate %s: %w", kind, err)
	}

	setName(resource, name)
	setExtra(resource, extra)

	return resource, nil
}

func canonicalToIRKind(kind string) (ir.ResourceKind, bool) {
	switch kind {
	case "skill":
		return ir.KindSkill, true
	case "command":
		return ir.KindCommand, true
	case "subagent":
		return ir.KindSubagent, true
	case "hook":
		return ir.KindHook, true
	case "mcpserver":
		return ir.KindMcpServer, true
	}
	return 0, false
}

func setName(r ir.Resource, name string) {
	switch v := r.(type) {
	case *ir.Skill:
		v.SetName(name)
	case *ir.Command:
		v.SetName(name)
	case *ir.Subagent:
		v.SetName(name)
	case *ir.Hook:
		v.SetName(name)
	case *ir.McpServer:
		v.SetName(name)
	}
}

func setExtra(r ir.Resource, extra map[string]any) {
	switch v := r.(type) {
	case *ir.Skill:
		v.Extra = extra
	case *ir.Command:
		v.Extra = extra
	case *ir.Subagent:
		v.Extra = extra
	case *ir.Hook:
		v.Extra = extra
	case *ir.McpServer:
		v.Extra = extra
	}
}

// populateStructFromMap sets exported struct fields from raw using per-field
// JSON unmarshaling. Extra is left for the caller. The returned map contains
// all keys that did not match a known field (with exact-match priority, then
// case-insensitive fallback), plus the injected "name" key.
func populateStructFromMap(raw map[string]any, v any) (map[string]any, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	rt := rv.Type()

	used := make(map[string]struct{}, len(raw))
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if isExtraField(f) {
			continue
		}

		key, skip := jsonKey(f)
		if skip {
			continue
		}

		val, matchedKey, ok := findValue(raw, key)
		if !ok {
			continue
		}
		used[matchedKey] = struct{}{}

		b, err := json.Marshal(val)
		if err != nil {
			return nil, fmt.Errorf("marshal field %s: %w", f.Name, err)
		}
		if err := json.Unmarshal(b, rv.Field(i).Addr().Interface()); err != nil {
			return nil, fmt.Errorf("unmarshal field %s: %w", f.Name, err)
		}
	}

	used["name"] = struct{}{}

	extra := make(map[string]any, len(raw))
	for k, val := range raw {
		if _, ok := used[k]; ok {
			continue
		}
		extra[k] = val
	}
	if len(extra) == 0 {
		return nil, nil
	}
	return normalizeExtraMap(extra), nil
}

func isExtraField(f reflect.StructField) bool {
	return f.Name == "Extra" && f.Type.String() == "map[string]interface {}"
}

func jsonKey(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if tag != "" {
		if idx := strings.Index(tag, ","); idx != -1 {
			tag = tag[:idx]
		}
		if tag != "" {
			return tag, false
		}
	}
	return f.Name, false
}

func findValue(raw map[string]any, key string) (any, string, bool) {
	if v, ok := raw[key]; ok {
		return v, key, true
	}
	for k, v := range raw {
		if strings.EqualFold(k, key) {
			return v, k, true
		}
	}
	return nil, "", false
}
