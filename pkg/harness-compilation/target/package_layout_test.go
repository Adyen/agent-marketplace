package target

import (
	"strings"
	"testing"
)

func TestValidatePackageLayouts(t *testing.T) {
	if err := ValidatePackageLayouts(); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePackageLayoutRejectsMalformed(t *testing.T) {
	valid := PackageLayout{
		ManifestPath: ".claude-plugin/plugin.json",
		Components:   map[string]string{"skills": "skills", "agents": "agents"},
	}
	for _, tc := range []struct {
		name   string
		layout PackageLayout
		format string
		want   string
	}{
		{name: "valid", layout: valid, format: "claude-plugin"},
		{
			name:   "unclean manifest path",
			layout: PackageLayout{ManifestPath: "a//plugin.json"},
			format: "x-plugin",
			want:   "manifest",
		},
		{
			name:   "manifest path names the package root",
			layout: PackageLayout{ManifestPath: "."},
			format: "x-plugin",
			want:   "manifest",
		},
		{
			name:   "manifest path is not a JSON file",
			layout: PackageLayout{ManifestPath: ".claude-plugin/plugin"},
			format: "x-plugin",
			want:   "JSON",
		},
		{
			name: "component claims the package root",
			layout: PackageLayout{
				ManifestPath: "plugin.json",
				Components:   map[string]string{"skills": "."},
			},
			format: "x-plugin",
			want:   "package root",
		},
		{
			name: "nested component directories",
			layout: PackageLayout{
				ManifestPath: "plugin.json",
				Components:   map[string]string{"skills": "skills", "extra": "skills/more"},
			},
			format: "x-plugin",
			want:   "nest",
		},
		{
			name: "component inside the manifest directory",
			layout: PackageLayout{
				ManifestPath: ".claude-plugin/plugin.json",
				Components:   map[string]string{"skills": ".claude-plugin/skills"},
			},
			format: "x-plugin",
			want:   "manifest",
		},
		{
			name:   "catalog root not expressed in parent elements",
			layout: PackageLayout{ManifestPath: ".claude-plugin/marketplace.json", CatalogRoot: "marketplace"},
			format: "x-marketplace",
			want:   "catalog root",
		},
		{
			name:   "catalog root claims the manifest directory",
			layout: PackageLayout{ManifestPath: ".claude-plugin/marketplace.json", CatalogRoot: "."},
			format: "x-marketplace",
			want:   "catalog root",
		},
		{
			name:   "invalid format identifier",
			layout: valid,
			format: "No Such Format",
			want:   "format",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePackageLayout(tc.format, tc.layout)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("valid layout rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("invalid layout accepted; want failure containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

func TestLookupPackageLayoutReturnsCopy(t *testing.T) {
	layout, ok := LookupPackageLayout("claude-plugin")
	if !ok {
		t.Fatal("claude-plugin package layout not registered")
	}
	layout.Components["skills"] = "mutated"
	again, _ := LookupPackageLayout("claude-plugin")
	if again.Components["skills"] != "skills" {
		t.Fatal("LookupPackageLayout exposes the shared declaration")
	}
}
