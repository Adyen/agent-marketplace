package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildFlags(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		target    string
		release   bool
		telemetry bool
		builds    int
	}{
		{target: "build", builds: 1},
		{target: "release", release: true, builds: 4},
		{target: "darwin-arm64", release: true, builds: 1},
		{target: "darwin-amd64", release: true, builds: 1},
		{target: "linux-amd64", release: true, builds: 1},
		{target: "linux-arm64", release: true, builds: 1},
		{target: "build-release", release: true, telemetry: true, builds: 1},
		{target: "script", release: true, telemetry: true, builds: 1},
	} {
		t.Run(tc.target, func(t *testing.T) {
			dir := t.TempDir()
			fakeGo := "#!/bin/sh\nprintf '%s\\000' \"$@\"\nprintf '\\n'\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then\n    shift\n    printf 'binary' > \"$1\"\n    break\n  fi\n  shift\ndone\nprintf '%s\\n' 'build tool warning' >&2\n"
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGo), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("make", "-s", "-f", filepath.Join(root, "Makefile"), tc.target)
			if tc.target == "script" {
				cmd = exec.Command("bash", filepath.Join(root, "scripts", "build-release.sh"))
			}
			cmd.Dir = dir
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"HOME=" + dir,
				"VERSION=v1.2.3",
				"AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT=https://telemetry.example.invalid",
				"AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL=https://telemetry.example.invalid/retention",
			}
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("build invocation: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
			}
			if got := strings.Count(stderr.String(), "build tool warning\n"); got != tc.builds {
				t.Fatalf("got %d tool warnings, want %d: %q", got, tc.builds, stderr.String())
			}
			output := stdout.String()
			records := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
			if len(records) != tc.builds {
				t.Fatalf("got %d build invocations, want %d: stdout %q, stderr %q", len(records), tc.builds, output, stderr.String())
			}
			for _, record := range records {
				args := strings.Split(strings.TrimSuffix(record, "\x00"), "\x00")
				if args[0] != "build" || args[len(args)-1] != "./cmd/agent-marketplace" {
					t.Fatalf("unexpected build command: %q", args)
				}
				if slices.Contains(args, "-trimpath") != tc.release {
					t.Errorf("trimpath: release=%v, args=%q", tc.release, args)
				}
				i := slices.Index(args, "-ldflags")
				if i < 0 || i+1 >= len(args) {
					t.Fatalf("missing linker flags: %q", args)
				}
				flags := strings.Fields(args[i+1])
				for _, flag := range []string{"-s", "-w"} {
					if slices.Contains(flags, flag) != tc.release {
						t.Errorf("%s: release=%v, flags=%q", flag, tc.release, flags)
					}
				}
				want := []string{"main.version=v1.2.3"}
				if tc.telemetry {
					want = append(want,
						"github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectEndpoint=https://telemetry.example.invalid",
						"github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectRetentionURL=https://telemetry.example.invalid/retention")
				}
				for _, value := range want {
					if i := slices.Index(flags, value); i < 1 || flags[i-1] != "-X" {
						t.Errorf("missing linker injection %q: %q", value, flags)
					}
				}
			}
		})
	}
}

// spec: install-script/release-build-writes-checksums-for-the-four-binaries
func TestReleaseWritesChecksums(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	if err := os.MkdirAll(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "stale-asset"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeGo := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then\n    shift\n    printf 'release binary %s\\n' \"$1\" > \"$1\"\n    exit 0\n  fi\n  shift\ndone\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGo), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "-s", "-f", filepath.Join(root, "Makefile"), "release")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "VERSION=v1.2.3", "DIST_DIR=" + dist}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make release: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	checksums, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("read SHA256SUMS: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(checksums)), "\n")
	want := []string{
		"agent-marketplace-v1.2.3-darwin-arm64",
		"agent-marketplace-v1.2.3-darwin-amd64",
		"agent-marketplace-v1.2.3-linux-amd64",
		"agent-marketplace-v1.2.3-linux-arm64",
	}
	if len(lines) != len(want) {
		t.Fatalf("SHA256SUMS has %d entries, want %d: %q", len(lines), len(want), checksums)
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != want[i] {
			t.Errorf("SHA256SUMS line %d = %q, want bare entry for %s", i+1, line, want[i])
		}
	}
	checker := "sha256sum"
	args := []string{"--check", filepath.Join(dist, "SHA256SUMS")}
	if _, err := exec.LookPath(checker); err != nil {
		checker = "shasum"
		args = []string{"-a", "256", "-c", filepath.Join(dist, "SHA256SUMS")}
	}
	check := exec.Command(checker, args...)
	check.Dir = dist
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("%s checksum verification: %v\n%s", checker, err, output)
	}
}
