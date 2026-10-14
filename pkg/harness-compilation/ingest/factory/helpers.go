package factory

import (
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var skillKnownKeys = map[string]bool{
	"name": true, "description": true, "triggers": true,
	"argument-hint": true, "model": true, "allowed-tools": true, "permissions": true,
	"subagent": true, "agent": true,
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
	"disallowedTools": true,
	"reasoningEffort": true,
	"isolation":       true,
	"max-nesting":     true,
	"maxNesting":      true,
}

func parseMarkdownResource(src fs.FS, path string) (map[string]any, string, error) {
	if err := validatePath(path); err != nil {
		return nil, "", err
	}
	f, err := src.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("ingest/factory: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, "", fmt.Errorf("ingest/factory: parse frontmatter %s: %w", path, err)
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return fm, body, nil
}

func validatePath(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("ingest/factory: path traversal rejected: %q", path)
	}
	return nil
}

func validateKebabName(name string) error {
	if name == "" {
		return fmt.Errorf("ingest/factory: name must not be empty")
	}
	if name[0] < 'a' || name[0] > 'z' {
		return fmt.Errorf("ingest/factory: name %q must start with a lowercase letter", name)
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return fmt.Errorf("ingest/factory: name %q must be kebab-case (lowercase letters, digits, hyphens only)", name)
		}
	}
	return nil
}

func extraFields(fm map[string]any, known map[string]bool) map[string]any {
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

func dirName(p string) string {
	p = strings.TrimRight(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func baseNameWithoutExt(p string) string {
	base := path.Base(p)
	if strings.HasSuffix(base, ".md") {
		return strings.TrimSuffix(base, ".md")
	}
	return base
}

func stringValue(v any) string { s, _ := v.(string); return s }

func toIntPtr(v any) (*int, error) {
	if v == nil {
		return nil, nil
	}
	var n int64
	switch value := v.(type) {
	case int:
		return &value, nil
	case int64:
		n = value
	case uint64:
		if value > ^uint64(0)>>1 {
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

func toStringSlice(v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []string:
		return t, nil
	case []any:
		values := make([]string, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("list item %d has unsupported type %T", i, item)
			}
			values[i] = s
		}
		return values, nil
	case nil:
		return nil, fmt.Errorf("value must be a string list, not null")
	default:
		return nil, fmt.Errorf("value has unsupported type %T", v)
	}
}

func supportFiles(src fs.FS, root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(src, root, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(current, root+"/")
		if rel == "SKILL.md" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func sourceRef(path string) ir.SourceRef {
	return ir.SourceRef{File: path, Line: 1}
}
