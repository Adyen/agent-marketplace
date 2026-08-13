package compile

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
)

// RenderYAMLKey renders one deterministic, parser-safe YAML mapping entry.
// It quotes both the key and string scalars and delegates nested values to the
// YAML encoder, so maps and lists never fall back to fmt's non-YAML syntax.
func RenderYAMLKey(key string, value any) (string, error) {
	key = yamlfm.QuoteScalar(key)
	if scalar, ok := value.(string); ok {
		return fmt.Sprintf("%s: %s\n", key, yamlfm.QuoteScalar(scalar)), nil
	}
	if value == nil {
		return key + ": null\n", nil
	}

	data, err := marshalYAML(value)
	if err != nil {
		return "", fmt.Errorf("render YAML value for key %q: %w", key, err)
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	lines := strings.Split(string(data), "\n")
	valueType := reflect.TypeOf(value)
	needsBlock := len(lines) > 1 || (len(lines) == 1 && lines[0] != "[]" && lines[0] != "{}" && valueType != nil && (valueType.Kind() == reflect.Map || valueType.Kind() == reflect.Slice || valueType.Kind() == reflect.Array))
	if !needsBlock {
		return fmt.Sprintf("%s: %s\n", key, lines[0]), nil
	}

	var builder strings.Builder
	builder.WriteString(key)
	builder.WriteString(":\n")
	for _, line := range lines {
		if line == "" {
			builder.WriteByte('\n')
			continue
		}
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

// Frontmatter accumulates YAML frontmatter keys in insertion order so target
// translators render byte-stable documents without maintaining their own
// ordered-map copy. Set is idempotent for key order: re-setting an existing key
// replaces the value and keeps its original position.
type Frontmatter struct {
	keys   []string
	values map[string]any
}

// Set records key with value, appending the key on first use.
func (f *Frontmatter) Set(key string, value any) {
	if f.values == nil {
		f.values = make(map[string]any)
	}
	if _, exists := f.values[key]; !exists {
		f.keys = append(f.keys, key)
	}
	f.values[key] = value
}

// Has reports whether key was set.
func (f *Frontmatter) Has(key string) bool {
	_, exists := f.values[key]
	return exists
}

// Keys returns the keys in insertion order.
func (f *Frontmatter) Keys() []string {
	return append([]string(nil), f.keys...)
}

// Value returns the value recorded for key.
func (f *Frontmatter) Value(key string) any { return f.values[key] }

// Render renders the accumulated keys as a delimited YAML frontmatter block,
// including the trailing delimiter and newline.
func (f *Frontmatter) Render() (string, error) {
	var builder strings.Builder
	builder.WriteString("---\n")
	for _, key := range f.keys {
		line, err := RenderYAMLKey(key, f.values[key])
		if err != nil {
			return "", err
		}
		builder.WriteString(line)
	}
	builder.WriteString("---\n")
	return builder.String(), nil
}

// marshalYAML converts yaml.v3's panic-on-unsupported-value behavior into an
// ordinary error so callers can suppress the artifact instead of emitting an
// unsafe fallback representation.
func marshalYAML(value any) (data []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("yaml marshal panic: %v", recovered)
		}
	}()
	return yaml.Marshal(value)
}
