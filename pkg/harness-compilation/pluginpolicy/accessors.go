package pluginpolicy

import (
	"fmt"
	"sync"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// ManifestFieldRule is the catalog rule for one manifest field: whether it
// is required, its declared type, and the outcome when its value is invalid
// (InvalidRefusePackage or InvalidIgnoreWithWarning).
type ManifestFieldRule struct {
	Required bool
	Type     string
	Invalid  string
}

// PackageFormat is the read view of one shipped format catalog: its role,
// recognized versions, manifest field rules, component discovery rules, and
// dependency posture. The catalogs remain the only source of these rules.
type PackageFormat struct {
	Role           string
	Versions       []string
	ManifestFields map[string]ManifestFieldRule
	Components     map[string]string
	Dependencies   string
}

var shippedRegistry = sync.OnceValues(func() (*registry, error) {
	return loadCatalogs(catalogFiles, "formats/*.yaml", "catalogs/*.yaml", nil)
})

// LookupPackageFormat returns the shipped catalog data for format. ok is
// false when no catalog declares the format.
func LookupPackageFormat(format string) (PackageFormat, bool, error) {
	r, err := shippedRegistry()
	if err != nil {
		return PackageFormat{}, false, err
	}
	c, ok := r.formats[format]
	if !ok {
		return PackageFormat{}, false, nil
	}
	fields := make(map[string]ManifestFieldRule, len(c.Manifest.Fields))
	for name, f := range c.Manifest.Fields {
		fields[name] = ManifestFieldRule(f)
	}
	components := make(map[string]string, len(c.Components))
	for name, rule := range c.Components {
		components[name] = rule
	}
	return PackageFormat{
		Role:           c.Role,
		Versions:       append([]string(nil), c.Versions...),
		ManifestFields: fields,
		Components:     components,
		Dependencies:   c.Dependencies,
	}, true, nil
}

// PluginFieldPolicies returns every plugin-surface field policy the harness
// catalog declares for kind, keyed by field name, so a projection can apply
// them without knowing the field vocabulary in advance.
func PluginFieldPolicies(t target.Target, kind string) (map[string]struct{ Outcome, Code string }, error) {
	r, err := shippedRegistry()
	if err != nil {
		return nil, err
	}
	c, ok := r.catalogs[t]
	if !ok {
		return nil, fmt.Errorf("pluginpolicy: no harness catalog for target %q", t)
	}
	out := map[string]struct{ Outcome, Code string }{}
	for field, o := range c.Surfaces.Plugin[kind] {
		out[field] = struct{ Outcome, Code string }{Outcome: o.Outcome, Code: o.Code}
	}
	return out, nil
}

// Accepts reports whether the harness catalog for t lists format among the
// package formats it accepts.
func Accepts(t target.Target, format string) (bool, error) {
	r, err := shippedRegistry()
	if err != nil {
		return false, err
	}
	c, ok := r.catalogs[t]
	if !ok {
		return false, fmt.Errorf("pluginpolicy: no harness catalog for target %q", t)
	}
	for _, accepted := range c.Accepts {
		if accepted == format {
			return true, nil
		}
	}
	return false, nil
}
