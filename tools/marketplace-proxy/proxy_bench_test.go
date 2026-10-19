package marketplaceproxy_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// BenchmarkLegacyRedirect includes local HTTP I/O and one amortized Docker exec,
// but excludes container startup. Go allocation counts describe the driver,
// not Nginx. AM_MARKETPLACE_PROXY_BENCH_CONFIG can select a pre-change profile.
func BenchmarkLegacyRedirect(b *testing.B) {
	image := os.Getenv("AM_MARKETPLACE_PROXY_IMAGE")
	if image == "" {
		b.Skip("the deployment gate supplies the pinned Nginx image")
	}
	profile := os.Getenv("AM_MARKETPLACE_PROXY_BENCH_CONFIG")
	if profile == "" {
		profile = "deploy/compose/sqlite-filesystem/nginx.conf"
	}
	container := startProxy(b, image, profile, true)
	config := strings.Repeat("url = \"http://127.0.0.1:8080/marketplace/admin?next=%2Fadmin\"\noutput = \"/dev/null\"\n", b.N)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", "exec", "--interactive", container,
		"curl", "--silent", "--show-error", "--fail", "--max-time", "2",
		"--write-out", "%{http_code}\n", "--config", "-")
	command.Stdin = strings.NewReader(config)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	b.ResetTimer()
	output, err := command.Output()
	b.StopTimer()
	if err != nil {
		b.Fatalf("redirect requests: %v\n%s", err, &stderr)
	}
	if got := strings.Count(string(output), "308\n"); got != b.N {
		b.Fatalf("successful redirects = %d, want %d", got, b.N)
	}
}
