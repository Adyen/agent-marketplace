package harness

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// Destination is one independently rooted native target layout.
type Destination struct {
	ID                   string
	Root                 string
	Prefix               string
	InstallationEvidence bool
	Info                 TargetInfo
}

type resolvedLayoutProvider func(machineScope bool, repoRoot string) ([]Destination, error)

// RouteArtifact strips the destination's native prefix after validating that
// the artifact remains within the destination root.
func (d Destination) RouteArtifact(path string) (string, error) {
	path = filepath.ToSlash(path)
	if err := ValidateRelPath(path); err != nil {
		return "", err
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
// Existing targets retain one default destination; partitioned targets resolve
// their named native roots through a registered application layout provider.
func ResolveDestinations(target hcc.Target, machineScope bool, repoRoot string) ([]Destination, error) {
	slog.Debug("resolve harness destinations start", "target", target, "machine_scope", machineScope)
	if provider, ok := resolvedLayoutProviders[target]; ok {
		destinations, err := provider(machineScope, repoRoot)
		if err != nil {
			return nil, err
		}
		slog.Debug("resolve harness destinations complete", "target", target, "machine_scope", machineScope, "count", len(destinations))
		return destinations, nil
	}

	info, ok := TargetInfoMap[target]
	if !ok {
		return nil, fmt.Errorf("unknown harness target %q", target)
	}
	root, err := canonicalRoot(info.Roots.For(machineScope, repoRoot))
	if err != nil {
		return nil, err
	}
	return []Destination{{ID: "default", Root: root, InstallationEvidence: true, Info: info}}, nil
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
