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
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\nprintf '%s\\000' \"$@\"\nprintf '\\n'\n"), 0o700); err != nil {
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
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("build invocation: %v\n%s", err, output)
			}
			records := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
			if len(records) != tc.builds {
				t.Fatalf("got %d build invocations, want %d: %q", len(records), tc.builds, output)
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
