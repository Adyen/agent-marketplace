package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
)

// Analyzer configures one explicitly named, versioned analyzer.
type Analyzer struct {
	ID       string            `yaml:"id"`
	Enabled  *bool             `yaml:"enabled,omitempty"`
	Role     string            `yaml:"role,omitempty"`
	Version  string            `yaml:"version,omitempty"`
	Run      *AnalyzerRun      `yaml:"run,omitempty"`
	Rulepack *AnalyzerRulepack `yaml:"rulepack,omitempty"`
	Config   map[string]any    `yaml:"config,omitempty"`
}

// AnalyzerRun configures an external analyzer executable.
type AnalyzerRun struct {
	Command              string   `yaml:"command"`
	Args                 []string `yaml:"args,omitempty"`
	Timeout              Duration `yaml:"timeout"`
	EnvPassthrough       []string `yaml:"env_passthrough,omitempty"`
	ImplementationDigest string   `yaml:"implementation_digest"`
}

// AnalyzerRulepack configures a static rulepack analyzer.
type AnalyzerRulepack struct {
	Path string `yaml:"path"`
}

const rulepackID = "marketplace.rulepack"

var (
	analyzerIDRE = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z][a-z0-9]*)*$`)
	envNameRE    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	argSecretRE  = regexp.MustCompile(`(?:^|[-_.])(token|password|secret|authorization|cookie)(?:[-_.]|$)`)
)

// configSecretKeys are substrings that flag a config key as bearing a secret.
var configSecretKeys = []string{"token", "secret", "password", "credential", "private_key", "api_key", "authorization", "cookie"}

func validateAnalyzers(list []Analyzer) []error {
	if len(list) == 0 {
		return nil
	}

	builtin := make(map[string]string)
	for _, reg := range analyze.DefaultRegistrations() {
		builtin[reg.Analyzer.ID()] = reg.Analyzer.Version()
	}

	var errs []error
	seen := make(map[string]struct{})
	for i, a := range list {
		id := a.ID
		if id == "" {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].id is required", i))
			continue
		}
		if id != strings.TrimSpace(id) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].id must not have leading or trailing whitespace", i))
			continue
		}
		if !analyzerIDRE.MatchString(id) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].id is not a safe dotted lower-case identifier", i))
			continue
		}
		if _, ok := seen[id]; ok {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].id is duplicated", i))
		}
		seen[id] = struct{}{}

		if a.Role != "" && a.Role != strings.TrimSpace(a.Role) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].role must not have leading or trailing whitespace", i))
		} else if a.Role != "" && a.Role != analyze.RoleGating && a.Role != analyze.RoleAdvisory {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].role must be %q or %q", i, analyze.RoleGating, analyze.RoleAdvisory))
		}

		if a.Version != "" && a.Version != strings.TrimSpace(a.Version) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].version must not have leading or trailing whitespace", i))
		}

		switch {
		case builtin[id] != "":
			if a.Run != nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] built-in %q must not declare a run block", i, id))
			}
			if a.Rulepack != nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] built-in %q must not declare a rulepack block", i, id))
			}
			if a.Version != "" && a.Version == strings.TrimSpace(a.Version) && a.Version != builtin[id] {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d].version does not match built-in version %q", i, builtin[id]))
			}
		case id == rulepackID:
			if a.Version != "" && a.Version == strings.TrimSpace(a.Version) {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d].version is not configurable for the built-in rulepack analyzer", i))
			}
			if a.Run != nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] rulepack %q must not declare a run block", i, id))
			}
			if a.Rulepack == nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] rulepack %q requires a rulepack path", i, id))
			} else {
				p := a.Rulepack.Path
				if p != strings.TrimSpace(p) {
					errs = append(errs, fmt.Errorf("policy.analyzers[%d].rulepack.path must not have leading or trailing whitespace", i))
				} else if p == "" || !filepath.IsAbs(p) {
					errs = append(errs, fmt.Errorf("policy.analyzers[%d].rulepack.path must be an absolute non-empty path", i))
				}
			}
		default:
			if a.Role == "" {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d].role must be %q or %q", i, analyze.RoleGating, analyze.RoleAdvisory))
			}
			if a.Rulepack != nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] %q must not declare a rulepack block", i, id))
			}
			if a.Run == nil {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d] %q requires a run block", i, id))
			} else {
				errs = append(errs, a.Run.validate(i, id, a.Version)...)
			}
		}

		if a.Config != nil {
			errs = append(errs, validateAnalyzerConfig(fmt.Sprintf("policy.analyzers[%d].config", i), a.Config)...)
		}
	}
	return errs
}

func (r *AnalyzerRun) validate(idx int, id, version string) []error {
	var errs []error
	if r.Command == "" {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.command is required", idx))
	} else if r.Command != strings.TrimSpace(r.Command) {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.command must not have leading or trailing whitespace", idx))
	} else if !filepath.IsAbs(r.Command) {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.command must be an absolute path", idx))
	}
	if r.Timeout.Duration() <= 0 {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.timeout must be a positive duration", idx))
	}
	if version == "" {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].version is required for a run-backed analyzer", idx))
	}
	if _, err := canonicalDigest(r.ImplementationDigest); err != nil {
		errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.implementation_digest is invalid: %w", idx, err))
	}

	seen := make(map[string]struct{})
	for i, name := range r.EnvPassthrough {
		if name != strings.TrimSpace(name) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.env_passthrough[%d] must not have leading or trailing whitespace", idx, i))
			continue
		}
		if !envNameRE.MatchString(name) {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.env_passthrough[%d] is not a valid environment variable name", idx, i))
			continue
		}
		if _, ok := seen[name]; ok {
			errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.env_passthrough[%d] is duplicated", idx, i))
			continue
		}
		seen[name] = struct{}{}
	}

	errs = append(errs, validateRunArgs(idx, r.Args)...)
	return errs
}

func canonicalDigest(d string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(d, prefix) {
		return "", errors.New("must start with \"sha256:\"")
	}
	hex := d[len(prefix):]
	if len(hex) != 64 {
		return "", errors.New("must contain 64 hex characters after \"sha256:\"")
	}
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		if !isLowerHex(c) {
			return "", fmt.Errorf("contains non-hex or uppercase character at position %d", i)
		}
	}
	return hex, nil
}

func isLowerHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

func validateRunArgs(idx int, args []string) []error {
	var errs []error
	for i, arg := range args {
		if eq := strings.Index(arg, "="); eq >= 0 {
			key := arg[:eq]
			if isSecretArgOption(key) {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.args[%d] contains an inline secret literal", idx, i))
			}
		}
	}
	for i := 0; i < len(args); i++ {
		if isSecretArgOption(args[i]) {
			if i+1 < len(args) && !isFileEnvRef(args[i+1]) && !strings.HasPrefix(args[i+1], "-") {
				errs = append(errs, fmt.Errorf("policy.analyzers[%d].run.args[%d] is followed by a secret literal", idx, i))
			}
		}
	}
	return errs
}

func isSecretArgOption(key string) bool {
	key = strings.TrimLeft(key, "-")
	key = strings.ToLower(key)
	if isFileEnvRef(key) {
		return false
	}
	if strings.Contains(key, "api-key") || strings.Contains(key, "api_key") {
		return true
	}
	return argSecretRE.MatchString(key)
}

func isFileEnvRef(s string) bool {
	s = strings.ToLower(s)
	return strings.HasSuffix(s, "-file") || strings.HasSuffix(s, "-env")
}

func validateAnalyzerConfig(path string, cfg map[string]any) []error {
	return walkAnalyzerValue(path, cfg)
}

func walkAnalyzerValue(path string, v any) []error {
	switch val := v.(type) {
	case map[string]any:
		return walkAnalyzerMap(path, val)
	case []any:
		return walkAnalyzerList(path, val)
	}
	return nil
}

func walkAnalyzerMap(path string, m map[string]any) []error {
	var errs []error
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		full := key
		if path != "" {
			full = path + "." + key
		}
		if isSecretConfigKey(key) {
			errs = append(errs, validateSecretReference(full, key, m[key])...)
			continue
		}
		errs = append(errs, walkAnalyzerValue(full, m[key])...)
	}
	return errs
}

func walkAnalyzerList(path string, list []any) []error {
	var errs []error
	for i, elem := range list {
		errs = append(errs, walkAnalyzerValue(fmt.Sprintf("%s[%d]", path, i), elem)...)
	}
	return errs
}

func validateSecretReference(path, key string, val any) []error {
	if val == nil {
		return nil
	}
	if s, ok := val.(string); ok && s == "" {
		return nil
	}
	m, ok := val.(map[string]any)
	if !ok {
		return []error{fmt.Errorf("%s key %q contains a disallowed secret value", path, key)}
	}
	if len(m) != 1 {
		return []error{fmt.Errorf("%s key %q must have exactly one file or env reference", path, key)}
	}
	var errs []error
	for refKey, refVal := range m {
		lk := strings.ToLower(refKey)
		if lk != "file" && lk != "env" {
			errs = append(errs, fmt.Errorf("%s key %q reference must be named file or env", path, key))
			continue
		}
		s, ok := refVal.(string)
		if !ok || strings.TrimSpace(s) == "" {
			errs = append(errs, fmt.Errorf("%s key %q %s reference must be a non-empty string", path, key, lk))
		}
	}
	return errs
}

func isSecretConfigKey(key string) bool {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	for _, kw := range configSecretKeys {
		if strings.Contains(key, kw) {
			return true
		}
	}
	return false
}
