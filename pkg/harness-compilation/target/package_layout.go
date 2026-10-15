package target

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// PackageLayout describes the filesystem shape of one plugin package or
// plugin catalog format. Like NativeLayout it is topology, not policy: it
// declares where the manifest lives and which root-relative directories hold
// member components, so package decoders and projections need no per-format
// path tables.
type PackageLayout struct {
	// ManifestPath is the package manifest path relative to the package
	// root (".claude-plugin/plugin.json", "plugin.json"). It names a file,
	// never the package root itself and never a declared component
	// directory.
	ManifestPath string
	// Components maps a member component to the root-relative directory
	// that holds it ("skills", "agents"). Directories are pairwise
	// element-boundary disjoint and none may claim the package root.
	Components map[string]string
	// CatalogRoot is declared only by catalog (marketplace) formats: the
	// path from the manifest's directory to the catalog root that relative
	// entry sources resolve against, expressed in ".." elements
	// (".claude-plugin/marketplace.json" entries resolve against "..",
	// ".agents/plugins/marketplace.json" entries against "../.."). An empty
	// value marks a package format.
	CatalogRoot string
}

// packageLayouts is the validated topology for the package formats the
// pluginpolicy family recognizes. Format identifiers here mirror the
// shipped plugin catalogs; the catalog check keeps the two sets equal.
var packageLayouts = map[string]PackageLayout{
	"claude-plugin": {
		ManifestPath: ".claude-plugin/plugin.json",
		Components:   map[string]string{"skills": "skills", "commands": "commands", "agents": "agents", "hooks": "hooks"},
	},
	"factory-plugin": {
		ManifestPath: ".factory-plugin/plugin.json",
		Components:   map[string]string{"skills": "skills", "commands": "commands", "droids": "droids", "output-styles": "output-styles", "hooks": "hooks"},
	},
	"codex-plugin": {
		ManifestPath: ".codex-plugin/plugin.json",
		Components:   map[string]string{"skills": "skills", "hooks": "hooks"},
	},
	"agent-plugin": {
		ManifestPath: "plugin.json",
		Components:   map[string]string{"skills": "skills"},
	},
	"claude-marketplace": {
		ManifestPath: ".claude-plugin/marketplace.json",
		CatalogRoot:  "..",
	},
	"factory-marketplace": {
		ManifestPath: ".factory-plugin/marketplace.json",
		CatalogRoot:  "..",
	},
	"codex-marketplace": {
		ManifestPath: ".agents/plugins/marketplace.json",
		CatalogRoot:  "../..",
	},
}

var packageComponentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// LookupPackageLayout returns the package layout declared for format.
func LookupPackageLayout(format string) (PackageLayout, bool) {
	layout, ok := packageLayouts[format]
	if !ok {
		return PackageLayout{}, false
	}
	layout.Components = maps.Clone(layout.Components)
	return layout, true
}

// PackageLayouts returns every declared package layout keyed by format.
func PackageLayouts() map[string]PackageLayout {
	out := make(map[string]PackageLayout, len(packageLayouts))
	for format := range packageLayouts {
		out[format], _ = LookupPackageLayout(format)
	}
	return out
}

// ValidatePackageLayouts validates every declared package layout.
// hcc.ValidateTargetRegistrations calls it so an invalid declaration fails
// at the registration boundary rather than at the first consumer lookup.
func ValidatePackageLayouts() error {
	var errs []error
	for _, format := range slices.Sorted(maps.Keys(packageLayouts)) {
		if err := ValidatePackageLayout(format, packageLayouts[format]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ValidatePackageLayout rejects a malformed package-layout declaration:
// an unclean manifest path, a component that claims the package root, or
// directories that nest inside each other or the manifest's directory.
func ValidatePackageLayout(format string, layout PackageLayout) error {
	var errs []error
	if !packageComponentName.MatchString(format) {
		errs = append(errs, fmt.Errorf("package layout declared for invalid format %q", format))
	}
	if !fs.ValidPath(layout.ManifestPath) || layout.ManifestPath == "." {
		errs = append(errs, fmt.Errorf("package format %q has invalid manifest path %q", format, layout.ManifestPath))
	} else if !strings.HasSuffix(layout.ManifestPath, ".json") {
		errs = append(errs, fmt.Errorf("package format %q manifest path %q does not name a JSON file", format, layout.ManifestPath))
	}
	manifestDir := path.Dir(layout.ManifestPath)
	componentDirs := make(map[string]string, len(layout.Components))
	for _, component := range slices.Sorted(maps.Keys(layout.Components)) {
		dir := layout.Components[component]
		if !packageComponentName.MatchString(component) {
			errs = append(errs, fmt.Errorf("package format %q has invalid component name %q", format, component))
		}
		if !fs.ValidPath(dir) {
			errs = append(errs, fmt.Errorf("package format %q component %q has invalid directory %q", format, component, dir))
			continue
		}
		if dir == "." {
			errs = append(errs, fmt.Errorf("package format %q component %q claims the package root; a component must name a subdirectory", format, component))
			continue
		}
		for other, otherDir := range componentDirs {
			if isPathPrefix(otherDir, dir) || isPathPrefix(dir, otherDir) {
				errs = append(errs, fmt.Errorf("package format %q components %q and %q declare directories %q and %q that nest inside each other", format, other, component, otherDir, dir))
			}
		}
		componentDirs[component] = dir
		if manifestDir != "." && (dir == manifestDir || isPathPrefix(manifestDir, dir) || isPathPrefix(dir, manifestDir)) {
			errs = append(errs, fmt.Errorf("package format %q component %q directory %q overlaps the manifest directory %q", format, component, dir, manifestDir))
		}
	}
	if layout.CatalogRoot != "" {
		for _, element := range strings.Split(layout.CatalogRoot, "/") {
			if element != ".." {
				errs = append(errs, fmt.Errorf("package format %q declares catalog root %q; a catalog root is expressed in \"..\" elements above the manifest directory", format, layout.CatalogRoot))
				break
			}
		}
	}
	return errors.Join(errs...)
}
