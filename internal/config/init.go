package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"gopkg.in/yaml.v3"
)

// ErrAlreadyInitialized is returned by InitializeUserPersonal when the user
// personal config file already exists. Callers can treat this as a no-op
// when init is idempotent.
var ErrAlreadyInitialized = errors.New("user personal config already initialized")

// SupportedHarnessTargets returns the canonical list of known harness target
// IDs in a stable order.
func SupportedHarnessTargets() []string {
	return []string{
		string(hcc.TargetFactory),
		string(hcc.TargetClaude),
		string(hcc.TargetCursor),
		string(hcc.TargetWindsurf),
		string(hcc.TargetGemini),
	}
}

// IsSupportedHarness reports whether id is a known harness target.
func IsSupportedHarness(id string) bool {
	switch hcc.Target(id) {
	case hcc.TargetFactory, hcc.TargetClaude, hcc.TargetCursor, hcc.TargetWindsurf, hcc.TargetGemini:
		return true
	}
	return false
}

// DetectInstalledHarnesses scans the user's home directory for harness
// installation roots and returns the IDs of those that appear to exist,
// sorted. It is a best-effort hint; an empty result means no harness
// installation roots were detected.
func DetectInstalledHarnesses() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return DetectInstalledHarnessesInHome(homeDir)
}

// DetectInstalledHarnessesInHome is the testable version of
// DetectInstalledHarnesses: it looks for machine-scoped harness install
// roots under the supplied home directory.
func DetectInstalledHarnessesInHome(homeDir string) ([]string, error) {
	roots := makeInstallRootsForHome(homeDir)
	var detected []string
	for id, root := range roots {
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat harness root %q: %w", root, err)
		}
		if info.IsDir() {
			detected = append(detected, string(id))
		}
	}
	sort.Strings(detected)
	return detected, nil
}

func makeInstallRootsForHome(homeDir string) map[hcc.Target]string {
	if homeDir == "" {
		return nil
	}
	return map[hcc.Target]string{
		hcc.TargetClaude:   filepath.Join(homeDir, ".claude"),
		hcc.TargetFactory:  filepath.Join(homeDir, ".factory"),
		hcc.TargetCursor:   filepath.Join(homeDir, ".cursor"),
		hcc.TargetWindsurf: filepath.Join(homeDir, ".codeium", "windsurf"),
		hcc.TargetGemini:   filepath.Join(homeDir, ".gemini"),
	}
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
// apiVersion agent-marketplace/v3 and contains only the apiVersion and
// harnesses fields. It returns ErrAlreadyInitialized if the file exists.
func InitializeUserPersonal(harnesses []string) (string, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return "", err
	}
	return InitializeUserPersonalAtPath(path, harnesses)
}

// InitializeUserPersonalAtPath is the testable core of InitializeUserPersonal.
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
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV3},
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
