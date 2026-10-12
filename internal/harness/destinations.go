package harness

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Destination is one independently rooted native target layout.
type Destination struct {
	ID                   string
	Root                 string
	Prefix               string
	InstallationEvidence bool
	AggregateRoutes      []AggregateRoute
	AggregateFile        string
	Info                 TargetInfo
}

// RouteArtifact strips the destination's native prefix after validating that
// the artifact remains within the destination root.
func (d Destination) RouteArtifact(path string) (string, error) {
	path = filepath.ToSlash(path)
	if err := ValidateRelPath(path); err != nil {
		return "", err
	}
	if len(d.AggregateRoutes) > 0 {
		for _, route := range d.AggregateRoutes {
			if route.ArtifactPath == path {
				if err := ValidateRelPath(d.AggregateFile); err != nil || d.AggregateFile == "" {
					return "", fmt.Errorf("invalid native aggregate destination")
				}
				return d.AggregateFile, nil
			}
		}
		return "", fmt.Errorf("artifact %q is not an allowed native aggregate", path)
	}
	if d.Prefix == "" {
		return path, nil
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(d.Prefix), "/") + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("artifact %q is outside destination prefix %q", path, d.Prefix)
	}
	rel := strings.TrimPrefix(path, prefix)
	if err := ValidateRelPath(rel); err != nil || rel == "" {
		return "", fmt.Errorf("artifact %q has invalid destination path", path)
	}
	return rel, nil
}

// ResolveDestinations returns the native output roots for a target and scope.
// Legacy targets retain one default destination; migrated targets resolve
// through the application-owned installation adapter registry, regardless of
// whether their native layout uses one root or multiple partitions.
func ResolveDestinations(target hcc.Target, machineScope bool, repoRoot string) ([]Destination, error) {
	homeDir := ""
	if machineScope {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			slog.Error("resolve harness destinations failed", "target", target, "machine_scope", machineScope, "reason", "home_directory")
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	return resolveDestinations(target, machineScope, repoRoot, homeDir)
}

// ResolveMachineDestinationsForHome resolves the same native machine layout as
// ResolveDestinations while allowing first-run detection to inspect an
// explicit home directory without mutating process environment.
func ResolveMachineDestinationsForHome(target hcc.Target, homeDir string) ([]Destination, error) {
	if homeDir == "" || !filepath.IsAbs(homeDir) {
		slog.Error("resolve harness destinations failed", "target", target, "machine_scope", true, "reason", "invalid_home_directory")
		return nil, fmt.Errorf("home directory must be absolute")
	}
	return resolveDestinations(target, true, "", homeDir)
}

// DetectInstalled scans resolved machine destinations for target-specific
// installation evidence and returns matching compiler target IDs.
func DetectInstalled(homeDir string) ([]string, error) {
	slog.Debug("detect installed harnesses start")
	var detected []string
	for _, definition := range targetmeta.Definitions() {
		if !definition.Compiler {
			continue
		}
		target := hcc.Target(definition.Target)
		destinations, err := ResolveMachineDestinationsForHome(target, homeDir)
		if err != nil {
			slog.Error("resolve harness detection roots", "target", target, "error", err)
			return nil, fmt.Errorf("resolve %s detection roots: %w", target, err)
		}
		for _, destination := range destinations {
			if !destination.InstallationEvidence {
				continue
			}
			info, err := os.Stat(destination.Root)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				slog.Error("inspect harness detection root failed", "target", target, "destination", destination.ID, "reason", "stat_failed")
				return nil, fmt.Errorf("stat harness root %q: %w", destination.Root, err)
			}
			if info.IsDir() {
				detected = append(detected, string(target))
				break
			}
		}
	}
	sort.Strings(detected)
	slog.Debug("detect installed harnesses complete", "targets", detected)
	return detected, nil
}

// DetectInstalledInCurrentHome resolves the current user's home before
// scanning its machine destinations.
func DetectInstalledInCurrentHome() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Error("detect installed harnesses failed", "reason", "home_directory")
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return DetectInstalled(homeDir)
}

func resolveDestinations(target hcc.Target, machineScope bool, repoRoot, homeDir string) (destinations []Destination, err error) {
	slog.Debug("resolve harness destinations start", "target", target, "machine_scope", machineScope)
	defer func() {
		if err != nil {
			slog.Error("resolve harness destinations failed", "target", target, "machine_scope", machineScope, "reason", "resolution_failed")
			return
		}
		slog.Debug("resolve harness destinations complete", "target", target, "machine_scope", machineScope, "count", len(destinations))
	}()
	adapters, adapterErr := NewInstallationAdapterRegistry()
	if adapterErr != nil {
		return nil, adapterErr
	}
	if adapter, ok := adapters.Lookup(target); ok {
		return adapter.Resolve(machineScope, repoRoot, homeDir)
	}
	definition, declared := targetmeta.Lookup(string(target))
	info, configured := TargetInfoMap[target]
	if !declared || !configured {
		return nil, fmt.Errorf("unknown harness target %q", target)
	}
	if definition.PartitionedRoots {
		return nil, fmt.Errorf("target %q has no installation adapter", target)
	}
	info.Roots = Roots{Repo: definition.RepoRoot, Machine: filepath.Join(homeDir, definition.UserRoot)}
	root, err := canonicalRoot(info.Roots.For(machineScope, repoRoot))
	if err != nil {
		return nil, err
	}
	if err := ValidateDestinationRoot(machineScope, repoRoot, homeDir, root); err != nil {
		return nil, fmt.Errorf("harness destination root: %w", err)
	}
	return nativeAggregateDestinations(target, machineScope, repoRoot, homeDir, Destination{ID: "default", Root: root, InstallationEvidence: true, Info: info})
}

func canonicalRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty harness destination root")
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve harness destination %q: %w", path, err)
	}
	return filepath.Clean(root), nil
}
