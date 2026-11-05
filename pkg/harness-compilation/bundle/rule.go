package bundle

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadRule reads a rule file from src at the given path. Rules may be authored
// either as plain Markdown (Factory-native, typically .md, no frontmatter) or
// as .mdc files with YAML frontmatter (Cursor/Windsurf-native, carrying
// description/globs/alwaysApply). Frontmatter is parsed only for .mdc files;
// plain .md rules are treated as pure body so a leading `---` Markdown
// horizontal rule is never mistaken for a frontmatter delimiter.
//
// Real-world Cursor/Windsurf .mdc rules frequently write unquoted glob patterns
// (e.g. `globs: *.ts`) which are not valid strict YAML. When strict parsing
// fails, LoadRule falls back to a lenient line-based frontmatter parse so those
// files still ingest correctly.
//
// Name defaults to the filename without its .md/.mdc extension when the
// frontmatter does not declare a name.
func LoadRule(src fs.FS, rulePath string) (*ir.Rule, error) {
	if err := validatePath(rulePath); err != nil {
		return nil, err
	}

	data, err := fs.ReadFile(src, rulePath)
	if err != nil {
		return nil, fmt.Errorf("bundle: open %s: %w", rulePath, err)
	}

	var fm map[string]any
	var body string
	if isFrontmatterRule(rulePath) {
		fm, body, err = yamlfm.ParseYAMLFrontmatter(bytes.NewReader(data))
		if errors.Is(err, yamlfm.ErrFrontmatterTooLarge) {
			return nil, fmt.Errorf("bundle: parse frontmatter %s: %w", rulePath, err)
		}
		if err != nil {
			var ok bool
			fm, body, ok = parseLenientRuleFrontmatter(data)
			if !ok {
				return nil, fmt.Errorf("bundle: parse frontmatter %s: %w", rulePath, err)
			}
		}
	} else {
		// Plain-markdown rules (Factory .md convention) never carry
		// frontmatter, so the whole file is the body. This avoids
		// misclassifying a leading `---` Markdown horizontal rule as a YAML
		// frontmatter delimiter.
		body = string(data)
	}
	if fm == nil {
		// Plain-markdown rule with no frontmatter: the whole file is the body.
		fm = map[string]any{}
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = ruleNameFromPath(rulePath)
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	globs := parseGlobs(fm["globs"])
	alwaysApply := parseBool(fm["alwaysApply"])
	extra := extraFields(fm, ruleKnownKeys)
	source := ir.SourceRef{File: rulePath, Line: 1}

	return ir.NewRule(name, description, body, globs, alwaysApply, extra, source), nil
}

// isFrontmatterRule reports whether a rule path uses the .mdc convention that
// carries YAML frontmatter (Cursor/Windsurf). Plain .md rules (Factory) are
// pure Markdown bodies and must not be parsed for frontmatter.
func isFrontmatterRule(rulePath string) bool {
	return strings.HasSuffix(rulePath, ".mdc")
}

// parseLenientRuleFrontmatter is a fallback for .mdc files whose frontmatter is
// not valid strict YAML (most commonly because glob values start with an
// unquoted `*`). It splits the leading `---`-delimited block and parses each
// `key: value` line, keeping values as trimmed strings. It returns ok=false
// when the content has no recognizable frontmatter block.
func parseLenientRuleFrontmatter(data []byte) (map[string]any, string, bool) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, "", false
	}
	// Skip the opening delimiter line.
	rest := content[strings.IndexByte(content, '\n')+1:]

	fm := map[string]any{}
	lines := strings.Split(rest, "\n")
	closed := false
	var consumed int
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "---" {
			closed = true
			consumed = i
			break
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, repeated := fm[key]; repeated {
			return nil, "", false
		}
		fm[key] = value
	}
	if !closed {
		return nil, "", false
	}
	body := strings.Join(lines[consumed+1:], "\n")
	return fm, body, true
}

// ruleNameFromPath returns the base filename with a trailing .mdc or .md
// extension removed.
func ruleNameFromPath(p string) string {
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, ".mdc"):
		return strings.TrimSuffix(base, ".mdc")
	case strings.HasSuffix(base, ".md"):
		return strings.TrimSuffix(base, ".md")
	default:
		return base
	}
}

// parseGlobs normalizes a frontmatter globs value into a []string. It accepts a
// YAML list (["*.ts", "*.go"]), a single string, or a comma-separated string
// ("*.ts,*.go") — the last form matches Cursor's native .mdc convention.
func parseGlobs(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return splitCommaGlobs(v)
	case []string:
		return trimGlobList(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func splitCommaGlobs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trimGlobList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseBool coerces a frontmatter value into a bool. YAML parsing already
// yields a native bool for true/false, but string forms are tolerated.
func parseBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "on", "1":
			return true
		}
	}
	return false
}
