package admin

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ValidationError explains why a proposal was rejected. It reports as
// ErrInvalidConfiguration through errors.Is, so callers that only need the
// category keep working, while the HTTP layer can surface Message and Fields
// to the editor instead of a generic rejection.
type ValidationError struct {
	// Message summarises the rejection for a human.
	Message string
	// Fields maps a registry draft path such as "slug", "origin" or
	// "roots[0].path" to the problem with that value. It is empty when the
	// rejection concerns the operation rather than one input.
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return ErrInvalidConfiguration.Error() + ": " + e.Message
}

func (e *ValidationError) Is(target error) bool { return target == ErrInvalidConfiguration }

// invalid rejects one draft field, or the whole operation when field is empty.
func invalid(field, message string) error {
	e := &ValidationError{Message: message}
	if field != "" {
		e.Fields = map[string]string{field: message}
	}
	return e
}

func invalidf(field, format string, args ...any) error {
	return invalid(field, fmt.Sprintf(format, args...))
}

// registryFieldPrefix is the operator-config path config.Registry.Validate
// prefixes every problem with; the admin draft has no such array index.
var registryFieldPrefix = regexp.MustCompile(`^registries\[\d+\]\.`)

// registryValidationError converts the joined output of
// config.Registry.Validate into one ValidationError keyed by draft field.
// Every message there starts with the field path, followed by either a
// colon or a space.
func registryValidationError(err error) error {
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	fields := make(map[string]string, len(errs))
	problems := make([]string, 0, len(errs))
	for _, e := range errs {
		text := registryFieldPrefix.ReplaceAllString(e.Error(), "")
		field, rest, _ := strings.Cut(text, " ")
		field = strings.TrimSuffix(field, ":")
		if rest = strings.TrimSpace(rest); rest == "" {
			rest = text
		}
		if prior, seen := fields[field]; seen {
			rest = prior + "; " + rest
		}
		fields[field] = rest
		problems = append(problems, field+" "+rest)
	}
	return &ValidationError{Message: summarise(problems), Fields: fields}
}

func summarise(problems []string) string {
	if len(problems) == 1 {
		return problems[0]
	}
	slices.Sort(problems)
	return fmt.Sprintf("%d configuration problems: %s", len(problems), strings.Join(problems, "; "))
}
