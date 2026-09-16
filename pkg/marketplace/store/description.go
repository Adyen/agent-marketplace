package store

import (
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxDescriptionBytes bounds a resource description, in bytes, both as stored
// and as served. Descriptions are one-line summaries authored in third-party
// registries, so the field is deliberately capped rather than allowed to carry
// an arbitrary blob into the index and into every list response.
const MaxDescriptionBytes = 1024

// descriptionEllipsis marks a description that was cut short. Its length is
// included in MaxDescriptionBytes so a bounded description never exceeds the
// limit.
const descriptionEllipsis = "..."

// BoundDescription normalizes an untrusted resource description into a single
// safely renderable line of at most MaxDescriptionBytes bytes.
//
// Descriptions come from third-party Git registries and are therefore
// attacker-controlled. Three properties are enforced here rather than at each
// call site:
//
//   - Control characters are removed. ESC in particular would otherwise let a
//     registry inject ANSI escape sequences into `agent-marketplace search`
//     terminal output, and newlines would let a one-line summary forge extra
//     lines in list renderings.
//   - Runs of whitespace collapse to a single space and the result is trimmed,
//     so padding cannot be used to push a card's layout around.
//   - The result is truncated on a UTF-8 rune boundary, so a bounded
//     description is always valid UTF-8 and encodes to JSON without
//     replacement characters.
func BoundDescription(description string) string {
	// The overwhelmingly common case is an already-normalized description:
	// ingest bounds what it writes, so re-bounding on the way out of the API
	// re-checks a clean string once per listed resource on every request. Scan
	// first and return the original when there is nothing to change, so the
	// egress check costs no allocations.
	if descriptionIsNormalized(description) {
		return description
	}

	cleaned := strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, description)
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	if len(cleaned) <= MaxDescriptionBytes {
		return cleaned
	}

	truncated := truncateRunes(cleaned, MaxDescriptionBytes-len(descriptionEllipsis)) + descriptionEllipsis
	slog.Debug("bounded resource description",
		"original_bytes", len(description),
		"bounded_bytes", len(truncated),
		"max_bytes", MaxDescriptionBytes,
	)
	return truncated
}

// descriptionIsNormalized reports whether BoundDescription would return s
// unchanged. It is a single allocation-free pass and must stay exactly as
// strict as the normalization below, so that the two paths cannot disagree.
func descriptionIsNormalized(s string) bool {
	if len(s) == 0 {
		return true
	}
	if len(s) > MaxDescriptionBytes {
		return false
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return false
	}

	previousWasSpace := false
	for _, r := range s {
		if r == ' ' {
			if previousWasSpace {
				return false
			}
			previousWasSpace = true
			continue
		}
		previousWasSpace = false

		// RuneError also covers invalid UTF-8 bytes, which range yields as
		// U+FFFD. unicode.IsSpace catches whitespace the normalization would
		// collapse but the control/format checks would not.
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// truncateRunes returns the longest prefix of s that is at most max bytes long
// and does not split a UTF-8 rune.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
