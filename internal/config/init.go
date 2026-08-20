package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

// ErrAlreadyInitialized is returned by InitializeUserPersonal when the user
// personal config file already exists. Callers can treat this as a no-op
// when init is idempotent.
var ErrAlreadyInitialized = errors.New("user personal config already initialized")

// SupportedHarnessTargets returns the canonical list of known harness target
// IDs in a stable order.
func SupportedHarnessTargets() []string {
	return targetmeta.CompilerNames()
}

// IsSupportedHarness reports whether id is a known harness target.
func IsSupportedHarness(id string) bool {
	return targetmeta.IsCompilerTarget(id)
}

// UserPersonalConfigPath returns the path to the user-personal config file.
func UserPersonalConfigPath() (string, error) {
	return GlobalConfigPath()
}

// UserPersonalConfigExists reports whether the user-personal config file
// already exists.
func UserPersonalConfigExists() (bool, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat user personal config %q: %w", path, err)
}

// InitializeUserPersonal creates the user personal config file with the
// provided harnesses if it does not already exist. The written config uses
// the canonical apiVersion agent-marketplace/v2 and contains only the
// apiVersion and harnesses fields. It returns ErrAlreadyInitialized if the file exists.
func InitializeUserPersonal(harnesses []string) (string, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return "", err
	}
	return InitializeUserPersonalAtPath(path, harnesses)
}

// PersistUserPersonalHarnesses resolves the authoritative user config path and
// persists a strict-v3 target selection. Unlike the legacy explicit init API,
// this mutation boundary upgrades a valid v2 file before editing it.
func PersistUserPersonalHarnesses(harnesses []string) (string, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return "", err
	}
	return PersistUserPersonalHarnessesAtPath(path, harnesses)
}

// PersistUserPersonalHarnessesAtPath makes the requested target set durable
// without treating mere file existence as success. Missing configs are created
// as minimal v3, valid v3 configs are edited in place, and valid v2 configs are
// migrated transactionally before the requested set is verified. Invalid, v1,
// and multi-document inputs are rejected before any write.
func PersistUserPersonalHarnessesAtPath(path string, harnesses []string) (string, error) {
	harnesses = normalizedUniqueValues(harnesses)
	if len(harnesses) == 0 {
		return "", fmt.Errorf("harnesses list must not be empty")
	}
	if err := ValidateHarnessIDs(harnesses); err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := SetHarnessesInConfig(path, harnesses); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("read user personal config %q: %w", path, err)
	default:
		if err := requireSingleYAMLDocument(data, fmt.Sprintf("user personal config %q", path)); err != nil {
			return "", err
		}
		switch PeekAPIVersion(data) {
		case APIVersionV3:
			if _, errs := loadStrictUserV3Config(data, false); len(errs) > 0 {
				return "", fmt.Errorf("validate user personal config %q: %w", path, errors.Join(errs...))
			}
			if err := SetHarnessesInConfig(path, harnesses); err != nil {
				return "", err
			}
		case APIVersionV2:
			var existing Config
			if err := yaml.Unmarshal(data, &existing); err != nil {
				return "", fmt.Errorf("parse user personal config %q: %w", path, err)
			}
			normalizeConfigHarnessTargets(&existing)
			var validationErrs []error
			if err := existing.validate(false, true, userLayer.name()); err != nil {
				validationErrs = append(validationErrs, err)
			}
			validationErrs = append(validationErrs, validateLogging(existing.Logging)...)
			if err := errors.Join(validationErrs...); err != nil {
				return "", fmt.Errorf("validate user personal config %q: %w", path, err)
			}
			if _, err := MigrateConfigFiles(MigratePaths{User: path}, harnesses); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("user personal config %q is v1 or has no supported apiVersion; run migration with explicit remediation before saving targets", path)
		}
	}

	persisted, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("verify user personal config %q: %w", path, err)
	}
	cfg, errs := loadStrictUserV3Config(persisted, true)
	if len(errs) > 0 {
		return "", fmt.Errorf("verify user personal config %q: %w", path, errors.Join(errs...))
	}
	if !stringSlicesEqual(cfg.Harnesses, harnesses) {
		return "", fmt.Errorf("requested harnesses were not persisted in %q: requested %v, found %v", path, harnesses, cfg.Harnesses)
	}
	return path, nil
}

func InitializeUserPersonalAtPath(path string, harnesses []string) (string, error) {
	if len(harnesses) == 0 {
		return "", fmt.Errorf("harnesses list must not be empty")
	}
	harnesses = normalizedUniqueValues(harnesses)
	if len(harnesses) == 0 {
		return "", fmt.Errorf("harnesses list must not be empty")
	}
	if err := ValidateHarnessIDs(harnesses); err != nil {
		return "", err
	}

	slog.Info("initializing user personal config", "path", path, "harnesses", harnesses)

	harnessNodes := make([]*yaml.Node, len(harnesses))
	for i, h := range harnesses {
		harnessNodes[i] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: h}
	}

	doc := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{
			{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apiVersion"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV2},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "harnesses"},
					{Kind: yaml.SequenceNode, Tag: "!!seq", Content: harnessNodes},
				},
			},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create config directory for %q: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, defaultConfigMode)
	if err != nil {
		if os.IsExist(err) {
			slog.Warn("user personal config already initialized", "path", path)
			return "", ErrAlreadyInitialized
		}
		return "", fmt.Errorf("create user personal config %q: %w", path, err)
	}

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("encode user personal config %q: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("close yaml encoder for %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close user personal config %q: %w", path, err)
	}

	slog.Info("user personal config initialized", "path", path, "harnesses", harnesses)
	return path, nil
}

// ValidateHarnessIDs returns an error if any harness ID is not a known
// target harness. Empty values are rejected.
func ValidateHarnessIDs(harnesses []string) error {
	for _, h := range harnesses {
		h = strings.TrimSpace(h)
		if h == "" {
			return fmt.Errorf("harness name must not be empty")
		}
		if !IsSupportedHarness(h) {
			return fmt.Errorf("unknown harness %q; supported values: %s", h, strings.Join(SupportedHarnessTargets(), ", "))
		}
	}
	return nil
}
