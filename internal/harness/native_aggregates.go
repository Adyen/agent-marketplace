package harness

import (
	"path/filepath"
	"slices"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type AggregateRoute struct {
	ArtifactPath string
	Container    string
}

type NativeAggregateInstall struct {
	ID          string
	ProjectPath string
	UserPath    string
	ScopeRoot   bool
	Routes      []AggregateRoute
}

var nativeAggregateInstalls = map[hcc.Target][]NativeAggregateInstall{
	hcc.TargetClaude:   {{ID: "mcp", ProjectPath: ".mcp.json", UserPath: ".claude.json", ScopeRoot: true, Routes: []AggregateRoute{{ArtifactPath: ".mcp.json", Container: "mcpServers"}}}},
	hcc.TargetGemini:   {{ID: "settings", ProjectPath: "settings.json", UserPath: "settings.json", Routes: []AggregateRoute{{ArtifactPath: "gemini-extension.json", Container: "mcpServers"}, {ArtifactPath: ".gemini/settings.json", Container: "hooks"}}}},
	hcc.TargetWindsurf: {{ID: "hooks", ProjectPath: ".windsurf/hooks.json", UserPath: "hooks.json", Routes: []AggregateRoute{{ArtifactPath: ".windsurf/hooks.json", Container: "hooks"}}}},
}

func NativeAggregateBackupFiles(target hcc.Target) (map[string]string, error) {
	files := make(map[string]string)
	if len(nativeAggregateInstalls[target]) == 0 {
		return files, nil
	}
	destinations, err := ResolveDestinations(target, true, ".")
	if err != nil {
		return nil, err
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) == 0 || destination.Root == destinations[0].Root {
			continue
		}
		for _, rel := range destination.Info.AggregateFilesForScope(true) {
			files[filepath.ToSlash(filepath.Join(".agent-marketplace-native", destination.ID, rel))] = filepath.Join(destination.Root, rel)
		}
	}
	return files, nil
}

func NativeAggregateInstalls(target hcc.Target) []NativeAggregateInstall {
	installs := slices.Clone(nativeAggregateInstalls[target])
	for i := range installs {
		installs[i].Routes = slices.Clone(installs[i].Routes)
	}
	return installs
}

func (n NativeAggregateInstall) Path(machineScope bool) string {
	if machineScope {
		return n.UserPath
	}
	return n.ProjectPath
}

func nativeAggregateDestinations(target hcc.Target, machineScope bool, repoRoot, homeDir string, base Destination) ([]Destination, error) {
	destinations := []Destination{base}
	for _, install := range nativeAggregateInstalls[target] {
		root := base.Root
		if install.ScopeRoot {
			root = repoRoot
			if machineScope {
				root = homeDir
			}
		}
		var err error
		root, err = canonicalRoot(root)
		if err != nil {
			return nil, err
		}
		rel := install.Path(machineScope)
		if err := ValidateRelPath(rel); err != nil {
			return nil, err
		}
		scope := "repo-"
		if machineScope {
			scope = "machine-"
		}
		destinations = append(destinations, Destination{ID: scope + install.ID, Root: root, AggregateRoutes: slices.Clone(install.Routes), AggregateFile: rel, Info: TargetInfo{AggregateFiles: AggregateFileSet{Project: []string{install.ProjectPath}, User: []string{install.UserPath}}}})
		for _, route := range install.Routes {
			old := route.ArtifactPath
			if prefix := base.Info.ArtifactRootPrefix; prefix != "" {
				old = strings.TrimPrefix(old, prefix+"/")
			}
			destinations[0].Info.AggregateFiles.Project = slices.DeleteFunc(slices.Clone(destinations[0].Info.AggregateFiles.Project), func(s string) bool { return s == old || (!install.ScopeRoot && s == install.ProjectPath) })
			destinations[0].Info.AggregateFiles.User = slices.DeleteFunc(slices.Clone(destinations[0].Info.AggregateFiles.User), func(s string) bool { return s == old || (!install.ScopeRoot && s == install.UserPath) })
		}
	}
	return destinations, nil
}
