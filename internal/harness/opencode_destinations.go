package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

func openCodeMachineRoot(home string) (string, error) {
	if custom := os.Getenv("OPENCODE_CONFIG_DIR"); custom != "" {
		if !filepath.IsAbs(custom) {
			return "", fmt.Errorf("OPENCODE_CONFIG_DIR must be absolute")
		}
		return filepath.Clean(custom), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("XDG_CONFIG_HOME must be absolute")
	}
	return filepath.Join(base, "opencode"), nil
}

func resolveOpenCodeDestinations(adapter InstallationAdapter, machine bool, repo, home string) ([]Destination, error) {
	if home == "" && (machine || os.Getenv("OPENCODE_CONFIG_DIR") != "" || os.Getenv("OPENCODE_CONFIG") != "") {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	root := filepath.Join(repo, ".opencode")
	if machine {
		var err error
		root, err = openCodeMachineRoot(home)
		if err != nil {
			return nil, err
		}
	}
	if custom := os.Getenv("OPENCODE_CONFIG_DIR"); custom != "" {
		if !filepath.IsAbs(custom) {
			return nil, fmt.Errorf("OPENCODE_CONFIG_DIR must be absolute")
		}
		root = filepath.Clean(custom)
	}
	root, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}
	if err := ValidateDestinationRoot(machine || os.Getenv("OPENCODE_CONFIG_DIR") != "", repo, home, root); err != nil {
		return nil, err
	}
	info := adapter.info
	info.AggregateFiles = AggregateFileSet{}
	base := Destination{ID: "default", Root: root, Info: info, InstallationEvidence: true}
	configRoot := repo
	if machine {
		configRoot = root
	}
	config := filepath.Join(configRoot, "opencode.json")
	if _, err := os.Lstat(filepath.Join(configRoot, "opencode.jsonc")); err == nil {
		config = filepath.Join(configRoot, "opencode.jsonc")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// A config inside the native directory is loaded after workspace config.
	if !machine {
		for _, name := range []string{"opencode.json", "opencode.jsonc"} {
			candidate := filepath.Join(root, name)
			if _, err := os.Lstat(candidate); err == nil {
				config = candidate
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	if custom := os.Getenv("OPENCODE_CONFIG"); custom != "" {
		if !filepath.IsAbs(custom) {
			return nil, fmt.Errorf("OPENCODE_CONFIG must be absolute")
		}
		config = filepath.Clean(custom)
	}
	configRoot = filepath.Dir(config)
	if err := ValidateDestinationRoot(machine || os.Getenv("OPENCODE_CONFIG") != "" || os.Getenv("OPENCODE_CONFIG_DIR") != "", repo, home, configRoot); err != nil {
		return nil, err
	}
	rel := filepath.Base(config)
	if err := ValidateRelPath(rel); err != nil {
		return nil, err
	}
	aggregateInfo := TargetInfo{OwnershipReconciledAggregates: true, AggregateFiles: AggregateFileSet{Project: []string{rel}, User: []string{rel}}}
	aggregate := Destination{ID: "mcp", Root: configRoot, AggregateFile: rel, AggregateRoutes: []AggregateRoute{{ArtifactPath: "opencode.json", Container: "mcp"}}, Info: aggregateInfo}
	return []Destination{base, aggregate}, nil
}
