package bundle

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// validatePath rejects paths containing ".." (path traversal).
func validatePath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("bundle: path traversal rejected: %q", path)
	}
	return nil
}

// validateKebabName rejects names that are not kebab-case:
// lowercase letters, digits, hyphens; must start with a letter.
func validateKebabName(name string) error {
	if name == "" {
		return fmt.Errorf("bundle: name must not be empty")
	}
	if name[0] < 'a' || name[0] > 'z' {
		return fmt.Errorf("bundle: name %q must start with a lowercase letter", name)
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return fmt.Errorf("bundle: name %q must be kebab-case (lowercase letters, digits, hyphens only)", name)
		}
	}
	return nil
}

// knownFrontmatterKeys lists the keys that are consumed by each loader.
// Any key in the frontmatter not in this set is treated as "extra" and
// preserved in the Extra map for round-trip correctness.
var skillKnownKeys = map[string]bool{
	"name":          true,
	"description":   true,
	"argument-hint": true,
	"model":         true,
	"allowed-tools": true,
	"permissions":   true,
	"triggers":      true,
	"subagent":      true,
	"agent":         true,
}

var commandKnownKeys = map[string]bool{
	"name":        true,
	"description": true,
}

var subagentKnownKeys = map[string]bool{
	"name":            true,
	"description":     true,
	"model":           true,
	"tools":           true,
	"effort":          true,
	"disallowedTools": true,
	"isolation":       true,
	"max-nesting":     true,
	"maxNesting":      true,
}

var ruleKnownKeys = map[string]bool{
	"name":        true,
	"description": true,
	"globs":       true,
	"alwaysApply": true,
}

func stringValueStrict(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("value has unsupported type %T", v)
	}
	return s, nil
}

func toStringSlice(v any) ([]string, error) {
	switch value := v.(type) {
	case string:
		return []string{value}, nil
	case []string:
		return value, nil
	case []any:
		result := make([]string, len(value))
		for i, item := range value {
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
		return nil, fmt.Errorf("value has unsupported type %T", v)
	}
}

func toIntPtr(v any) (*int, error) {
	if v == nil {
		return nil, nil
	}
	var n int64
	switch value := v.(type) {
	case int:
		return &value, nil
	case int8:
		n = int64(value)
	case int16:
		n = int64(value)
	case int32:
		n = int64(value)
	case int64:
		n = value
	case uint:
		if uint64(value) > uint64(^uint(0)>>1) {
			return nil, fmt.Errorf("integer out of range")
		}
		return intPtr(int(value)), nil
	case uint8:
		n = int64(value)
	case uint16:
		n = int64(value)
	case uint32:
		n = int64(value)
	case uint64:
		if value > math.MaxInt64 {
			return nil, fmt.Errorf("integer out of range")
		}
		n = int64(value)
	case float64:
		if value < math.MinInt64 || value > math.MaxInt64 {
			return nil, fmt.Errorf("integer out of range")
		}
		if value != float64(int64(value)) {
			return nil, fmt.Errorf("value must be an integer")
		}
		n = int64(value)
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value must be an integer")
		}
		n = parsed
	default:
		return nil, fmt.Errorf("value has unsupported type %T", v)
	}
	if int64(int(n)) != n {
		return nil, fmt.Errorf("integer out of range")
	}
	return intPtr(int(n)), nil
}

func intPtr(v int) *int { return &v }

func skillPermissions(v any) (ir.Permissions, error) {
	values, ok := v.(map[string]any)
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

// extraFields returns a map of frontmatter keys not in the known set.
func extraFields(fm map[string]any, known map[string]bool) map[string]any {
	if len(fm) <= len(known) {
		// Fast path: if there are no more keys than known ones, check individually.
		hasExtra := false
		for k := range fm {
			if !known[k] {
				hasExtra = true
				break
			}
		}
		if !hasExtra {
			return nil
		}
	}
	extra := make(map[string]any)
	for k, v := range fm {
		if !known[k] {
			extra[k] = v
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// dirName returns the last path component (the directory name).
func dirName(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
