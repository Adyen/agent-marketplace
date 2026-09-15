// Package diagnosticidentity provides the shared canonical encoding used by
// internal diagnostic projections. It has no effect on the public HCC
// diagnostic model or on normal diagnostic rendering.
package diagnosticidentity

import (
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

const pathPlaceholder = "<path>"

type Field struct {
	Label string
	Value string
}

func FullText(value string) string {
	return diag.SanitizeText(value, len(value)+len("…(truncated)"))
}

// Canonical frames every value with a label, presence bit, and byte length.
// This prevents untrusted values from forging boundaries or making empty and
// default values indistinguishable from omitted fields.
func Canonical(fields ...Field) string {
	var b strings.Builder
	for _, field := range fields {
		value := field.Value
		b.WriteString(field.Label)
		b.WriteString("[present=")
		if value != "" {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteString(",len=")
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteString("]:")
		b.WriteString(value)
		b.WriteByte(';')
	}
	return b.String()
}

// NormalizeMessage folds only absolute filesystem paths in a message. Scope,
// root, resource, and source fields remain byte-identical identity components.
func NormalizeMessage(message string) string {
	if !strings.Contains(message, "/") && !strings.Contains(message, `\`) {
		return message
	}
	fields := strings.Split(message, " ")
	folded := false
	for i, field := range fields {
		lead := 0
		for lead < len(field) && strings.ContainsRune(`"'`+"`"+`(<[`, rune(field[lead])) {
			lead++
		}
		trail := len(field)
		for trail > lead && strings.ContainsRune(`"'`+"`"+`)>],.:;`, rune(field[trail-1])) {
			trail--
		}
		if !isAbsolutePath(field[lead:trail]) {
			continue
		}
		fields[i] = field[:lead] + pathPlaceholder + field[trail:]
		folded = true
	}
	if !folded {
		return message
	}
	return strings.Join(fields, " ")
}

func isAbsolutePath(value string) bool {
	switch {
	case len(value) > 1 && value[0] == '/':
		return true
	case len(value) > 2 && value[0] == '~' && (value[1] == '/' || value[1] == '\\'):
		return true
	case len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/'):
		return true
	default:
		return false
	}
}
