package marketplaceproxy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// spec: site-deployment-profiles/marketplace-documentation-hosting-is-disabled-by-default
// spec: site-deployment-profiles/marketplace-documentation-hosting-is-explicitly-enabled
func TestMarketplaceComposeDocs(t *testing.T) {
	if os.Getenv("AM_MARKETPLACE_PROXY_IMAGE") == "" {
		t.Skip("the deployment gate requires Docker Compose")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"compose.yaml", "deploy/compose/postgres-s3/compose.yaml"} {
		for _, enabled := range []bool{false, true} {
			t.Run(base+"/docs="+strconv.FormatBool(enabled), func(t *testing.T) {
				path := filepath.Join(root, base)
				args := []string{"compose", "--env-file", "/dev/null", "-f", path}
				if enabled {
					args = append(args, "-f", filepath.Join(filepath.Dir(path), "compose.docs.yaml"))
				}
				args = append(args, "config", "--format", "json")
				var config struct {
					Services map[string]struct {
						Environment map[string]string
						DependsOn   map[string]struct {
							Condition string
						} `json:"depends_on"`
						Volumes []struct {
							Source   string
							Target   string
							ReadOnly bool `json:"read_only"`
						}
						Build struct {
							Context string
							Args    map[string]string
						}
					}
				}
				if err := json.Unmarshal([]byte(runDocker(t, args...)), &config); err != nil {
					t.Fatal(err)
				}
				docs, hasDocs := config.Services["docs"]
				if hasDocs != enabled {
					t.Fatalf("documentation service present = %t, want %t", hasDocs, enabled)
				}
				web := config.Services["web"]
				if got := web.Environment["NUXT_PUBLIC_DOCS_ENABLED"]; got != strconv.FormatBool(enabled) {
					t.Errorf("web docs enabled = %q, want %t", got, enabled)
				}
				proxy := config.Services["proxy"]
				dependency, dependsOnDocs := proxy.DependsOn["docs"]
				if dependsOnDocs != enabled || enabled && dependency.Condition != "service_healthy" {
					t.Errorf("proxy docs dependency = %+v (present %t), want healthy dependency only when enabled", dependency, dependsOnDocs)
				}
				filename := "docs-disabled.conf"
				if enabled {
					filename = "docs-enabled.conf"
					if docs.Build.Context != filepath.Join(root, "site") || docs.Build.Args["SITE_PROFILE"] != "docs" || docs.Build.Args["SITE_BASE"] != "/docs/" {
						t.Errorf("documentation build does not use the shared docs-only source: %+v", docs.Build)
					}
				}
				mounts := 0
				for _, volume := range proxy.Volumes {
					if volume.Target == "/etc/nginx/marketplace-docs.conf" {
						mounts++
						if volume.Source != filepath.Join(root, "deploy/compose/shared", filename) || !volume.ReadOnly {
							t.Errorf("docs route mount = %+v, want read-only %s", volume, filename)
						}
					}
				}
				if mounts != 1 {
					t.Errorf("proxy has %d documentation route mounts, want exactly one", mounts)
				}
			})
		}
	}
}
