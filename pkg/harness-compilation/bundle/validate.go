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

// toToolList reads a subagent tool list. Claude documents tools and
// disallowedTools as either a YAML list, taken as written, or a
// comma-separated string such as "Read, Grep, Bash". A comma inside
// parentheses belongs to a scoped tool such as Agent(worker, researcher) and
// does not split it, and a backslash makes the next character literal, as in
// permission rules. A string that names no tool, whose parentheses do not
// balance, or that ends in a backslash with nothing after it is not a
// well-formed list: it keeps the single entry that earlier versions produced
// for every string, so a subagent that loaded before still loads and a source
// revision that lists it still verifies.
func toToolList(v any) ([]string, error) {
	text, ok := v.(string)
	if !ok {
		return toStringSlice(v)
	}
	tools := make([]string, 0, strings.Count(text, ",")+1)
	depth, start := 0, 0
	appendTool := func(end int) {
		if tool := strings.TrimSpace(text[start:end]); tool != "" {
			tools = append(tools, tool)
		}
	}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if i+1 == len(text) {
				return toStringSlice(v)
			}
			i++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return toStringSlice(v)
			}
			depth--
		case ',':
			if depth == 0 {
				appendTool(i)
				start = i + 1
			}
		}
	}
	appendTool(len(text))
	if depth != 0 || len(tools) == 0 {
		return toStringSlice(v)
	}
	return tools, nil
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
		if value < float64(math.MinInt) || value >= -float64(math.MinInt) {
			return nil, fmt.Errorf("integer out of range")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) {
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
	if n < math.MinInt || n > math.MaxInt {
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
