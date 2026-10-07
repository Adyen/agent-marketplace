package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// canonicalCompilerHarnesses normalizes values accepted from config mutation
// APIs, removes duplicates, and validates them before any file is written.
func canonicalCompilerHarnesses(field string, harnesses []string) ([]string, error) {
	canonical := normalizedUniqueValues(harnesses)
	if len(canonical) == 0 {
		return nil, fmt.Errorf("%s value is required", field)
	}
	if errs := validateHarnessTargets(field, canonical); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return canonical, nil
}

func validateHarnessTargets(field string, harnesses []string) []error {
	return validateTargets(field, harnesses, targetmeta.IsCompilerTarget, targetmeta.CompilerNames, "compiler")
}

func validateReadableHarnessTargets(field string, harnesses []string) []error {
	var errs []error
	for i, harness := range harnesses {
		if strings.TrimSpace(harness) == "" {
			errs = append(errs, fmt.Errorf("%s[%d] must not be empty", field, i))
		}
	}
	return errs
}

func KnownHarnessTargets(ids []string) []string {
	var out []string
	for _, id := range normalizedUniqueValues(ids) {
		if IsSupportedHarness(id) {
			out = append(out, id)
		}
	}
	return out
}

const UnsupportedHarnessWarningCode = "config.harness.unsupported"

type UnsupportedHarnessWarning struct {
	Code   string
	Source Source
	Path   string
	Count  int
}

func (w *UnsupportedHarnessWarning) Error() string {
	return fmt.Sprintf("%s: %s: %s configuration declares %d harness target identifier(s) this client does not support", w.Code, w.Path, w.Source.String(), w.Count)
}

func (l *LayerIntent) warnUnsupportedHarnesses(harnesses []string) {
	count := 0
	for _, h := range harnesses {
		if h = strings.TrimSpace(h); h != "" && !IsSupportedHarness(h) {
			count++
		}
	}
	if count == 0 {
		return
	}
	l.warnings = append(l.warnings, &UnsupportedHarnessWarning{Code: UnsupportedHarnessWarningCode, Source: l.Source, Path: l.Path, Count: count})
	slog.Warn("harness target identifiers unsupported by this client retained",
		"code", UnsupportedHarnessWarningCode,
		"source", l.Source.String(),
		"path", l.Path,
		"count", count,
	)
}

func validateSourceHarness(field, harness string) []error {
	if targetmeta.IsIngesterTarget(harness) {
		return nil
	}
	slog.Debug("invalid source harness", "field", field, "value", harness)
	return []error{fmt.Errorf("%s contains unknown or unsupported source harness %q; supported source harnesses: %s", field, harness, strings.Join(targetmeta.IngesterNames(), ", "))}
}

func validateTargets(field string, values []string, valid func(string) bool, supported func() []string, capability string) []error {
	var errs []error
	for i, value := range values {
		if valid(value) {
			continue
		}
		slog.Debug("invalid harness target", "field", field, "value", value, "capability", capability)
		errs = append(errs, fmt.Errorf("%s[%d] contains unknown or unsupported %s harness %q; supported %s harnesses: %s", field, i, capability, value, capability, strings.Join(supported(), ", ")))
	}
	return errs
}

func validateConfigHarnessTargets(c Config) []error {
	var errs []error
	errs = append(errs, validateReadableHarnessTargets("harnesses", c.Harnesses)...)
	errs = append(errs, validateRegistryHarnessTargets(c.Registries)...)
	return errs
}

func validateRegistryHarnessTargets(registries []RegistryEntry) []error {
	return validateRegistryHarnessTargetsAt("registries", registries)
}

func validateRegistryHarnessTargetsAt(fieldPrefix string, registries []RegistryEntry) []error {
	var errs []error
	for i, registry := range registries {
		if strings.TrimSpace(registry.SourceHarness) != "" {
			field := fmt.Sprintf("%s[%d].source_harness", fieldPrefix, i)
			errs = append(errs, validateSourceHarness(field, registry.SourceHarness)...)
		}
		if registry.Harnesses != nil {
			field := fmt.Sprintf("%s[%d].harnesses", fieldPrefix, i)
			errs = append(errs, validateHarnessTargets(field, registry.Harnesses)...)
		}
	}
	return errs
}
