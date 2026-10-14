package marketplaceproxy_test

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spec: site-deployment-profiles/legacy-marketplace-links-reach-the-root-mounted-interface
// spec: site-deployment-profiles/unsafe-legacy-browser-paths-never-redirect
func TestMarketplaceNginxRedirects(t *testing.T) {
	image := os.Getenv("AM_MARKETPLACE_PROXY_IMAGE")
	if image == "" {
		t.Skip("the deployment gate supplies the pinned Nginx image")
	}
	for _, profile := range []string{
		"deploy/compose/sqlite-filesystem/nginx.conf",
		"deploy/compose/postgres-s3/nginx.conf",
		"deploy/compose/sqlite-filesystem/nginx-operator.local.conf",
	} {
		t.Run(profile, func(t *testing.T) {
			container := startProxy(t, image, profile)

			for _, tc := range []struct {
				path     string
				status   int
				location string
			}{
				{"/marketplace", 308, "/"},
				{"/marketplace/", 308, "/"},
				{"/marketplace?next=%2Fadmin", 308, "/?next=%2Fadmin"},
				{"/marketplace/admin?next=%2Fadmin", 308, "/admin?next=%2Fadmin"},
				{"/marketplace/r/team/skill/name?view=compact", 308, "/r/team/skill/name?view=compact"},
				{"/marketplace/admin?name=%5Ckept", 308, "/admin?name=%5Ckept"},
				{"/marketplace/%5Cevil.example/path?next=%2Fadmin", 400, ""},
				{"/marketplace/%5cevil.example/path", 400, ""},
				{"/marketplace/%5C%5Cevil.example/path", 400, ""},
				{"/marketplace/%2F%5Cevil.example/path", 400, ""},
				{"/marketplace/admin%5Csettings", 400, ""},
				{"/marketplace/\\evil.example/path", 400, ""},
				{"/marketplace/%09/evil.example/path", 400, ""},
				{"/marketplace/%0A/evil.example/path", 400, ""},
				{"/marketplace/admin%0A/settings", 400, ""},
				{"/marketplace/%09%0A/evil.example/path", 400, ""},
				{"/marketplace/%0D/evil.example/path", 400, ""},
				{"/marketplace/%09%2Fevil.example/path", 400, ""},
				{"/marketplace/%00/evil.example/path", 400, ""},
				{"/marketplace/%1F/evil.example/path", 400, ""},
				{"/marketplace/%7F/evil.example/path", 400, ""},
				{"/marketplace/admin?name=%09%0A%0Dkept", 308, "/admin?name=%09%0A%0Dkept"},
				{"/docs", 308, "/docs/"},
				{"/readyz", 404, ""},
				{"/metrics", 404, ""},
			} {
				t.Run(tc.path, func(t *testing.T) {
					response := proxyResponse(t, container, tc.path)
					if response.StatusCode != tc.status || response.Header.Get("Location") != tc.location {
						t.Errorf("response = %d Location %q, want %d Location %q",
							response.StatusCode, response.Header.Get("Location"), tc.status, tc.location)
					}
				})
			}
			for _, tc := range []struct {
				path string
				body string
			}{
				{"/", "web"},
				{"/docs/", "docs"},
			} {
				if got := runDocker(t, "exec", container, "curl", "--silent", "--show-error",
					"--fail", "--max-time", "2", "http://127.0.0.1:8080"+tc.path); got != tc.body {
					t.Errorf("%s owner = %q, want %q", tc.path, got, tc.body)
				}
			}
			if strings.HasSuffix(profile, "/nginx.conf") {
				for _, route := range []string{"/api/v1/instance", "/.well-known/agent-marketplace.json", "/healthz"} {
					if got := runDocker(t, "exec", container, "curl", "--silent", "--show-error",
						"--fail", "--max-time", "2", "http://127.0.0.1:8080"+route); got != "api" {
						t.Errorf("%s owner = %q, want api", route, got)
					}
				}
			}
		})
	}
}

func proxyResponse(t *testing.T, container, path string) *http.Response {
	t.Helper()
	output := runDocker(t, "exec", container, "curl", "--silent", "--show-error",
		"--path-as-is", "--max-time", "2", "--dump-header", "-", "--output", "/dev/null",
		"http://127.0.0.1:8080"+path)
	// Curl discards the body; tell the header parser not to expect one.
	response, err := http.ReadResponse(bufio.NewReader(strings.NewReader(output)), &http.Request{Method: http.MethodHead})
	if err != nil {
		t.Fatalf("parse proxy response: %v\n%s", err, output)
	}
	return response
}

func startProxy(t testing.TB, image, profile string) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(profile) {
		profile = filepath.Join(root, profile)
	}
	stubs := filepath.Join(t.TempDir(), "stubs.conf")
	if err := os.WriteFile(stubs, []byte(`
server { listen 127.0.0.2:8080; return 200 "api"; }
server { listen 127.0.0.3:8080; return 200 "docs"; }
server { listen 127.0.0.4:3000; return 200 "web"; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	container := strings.TrimSpace(runDocker(t, "run", "--detach", "--rm",
		"--network", "none", "--read-only", "--user", "101:101",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--tmpfs", "/var/cache/nginx:uid=101,gid=101",
		"--tmpfs", "/var/run:uid=101,gid=101",
		"--tmpfs", "/tmp:uid=101,gid=101,mode=1777",
		"--add-host", "app:127.0.0.2", "--add-host", "api:127.0.0.2",
		"--add-host", "docs:127.0.0.3", "--add-host", "web:127.0.0.4",
		"--mount", "type=bind,src="+profile+",dst=/etc/nginx/conf.d/default.conf,readonly",
		"--mount", "type=bind,src="+filepath.Join(root, "deploy/compose/shared/proxy-headers.conf")+",dst=/etc/nginx/proxy-headers.conf,readonly",
		"--mount", "type=bind,src="+stubs+",dst=/etc/nginx/conf.d/stubs.conf,readonly",
		"--entrypoint", "nginx", image, "-g", "daemon off;"))
	t.Cleanup(func() {
		runDocker(t, "rm", "--force", container)
	})
	// Wait for a positive routing control, not merely container creation.
	runDocker(t, "exec", container, "curl", "--silent", "--show-error",
		"--fail", "--retry", "10", "--retry-connrefused", "--retry-delay", "1",
		"--max-time", "2", "http://127.0.0.1:8080/")
	return container
}

func runDocker(t testing.TB, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("docker %v: %v\n%s\n%s", args, err, output, &stderr)
	}
	return string(output)
}
