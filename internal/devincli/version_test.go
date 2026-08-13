package devincli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestDetectClassifiesEveryProbeOutcome pins the whole detection contract from
// work package D11: below 3000.3 warns, at or above is compatible, and every
// failure mode is a Status rather than an error. Detect returns no error at all,
// so "never let doctor block or crash because a probe misbehaved" is a property
// of the signature.
func TestDetectClassifiesEveryProbeOutcome(t *testing.T) {
	notFound := func(string) (string, error) { return "", exec.ErrNotFound }
	found := func(string) (string, error) { return "/usr/local/bin/devin", nil }
	output := func(text string) func(context.Context, string) ([]byte, error) {
		return func(context.Context, string) ([]byte, error) { return []byte(text), nil }
	}

	tests := []struct {
		name        string
		lookPath    func(string) (string, error)
		run         func(context.Context, string) ([]byte, error)
		wantStatus  Status
		wantVersion string
		wantErr     bool
	}{
		{
			name:       "not installed",
			lookPath:   notFound,
			run:        output("unused"),
			wantStatus: StatusNotInstalled,
		},
		{
			name:       "empty path from lookup is treated as not installed",
			lookPath:   func(string) (string, error) { return "   ", nil },
			run:        output("unused"),
			wantStatus: StatusNotInstalled,
		},
		{
			name:        "evidence snapshot banner is supported",
			lookPath:    found,
			run:         output("devin 3000.3.27 (0becb483)\n"),
			wantStatus:  StatusSupported,
			wantVersion: "3000.3.27",
		},
		{
			name:        "exactly the floor is supported",
			lookPath:    found,
			run:         output("devin 3000.3\n"),
			wantStatus:  StatusSupported,
			wantVersion: "3000.3",
		},
		{
			name:        "a later minor is supported",
			lookPath:    found,
			run:         output("devin 3000.4.0\n"),
			wantStatus:  StatusSupported,
			wantVersion: "3000.4.0",
		},
		{
			name:        "a later major is supported",
			lookPath:    found,
			run:         output("devin 3001.0.1\n"),
			wantStatus:  StatusSupported,
			wantVersion: "3001.0.1",
		},
		{
			name:        "the prior runtime probe version is below the floor",
			lookPath:    found,
			run:         output("devin 3000.2.17 (deadbeef)\n"),
			wantStatus:  StatusUnsupported,
			wantVersion: "3000.2.17",
		},
		{
			name:        "an older major is below the floor",
			lookPath:    found,
			run:         output("devin 2999.9.9\n"),
			wantStatus:  StatusUnsupported,
			wantVersion: "2999.9.9",
		},
		{
			name:       "unparseable output is unknown, not assumed compatible",
			lookPath:   found,
			run:        output("devin (dev build)\n"),
			wantStatus: StatusUnknown,
			wantErr:    true,
		},
		{
			name:       "empty output is unknown",
			lookPath:   found,
			run:        output(""),
			wantStatus: StatusUnknown,
			wantErr:    true,
		},
		{
			name:     "a failing probe is unknown",
			lookPath: found,
			run: func(context.Context, string) ([]byte, error) {
				return []byte("boom"), errors.New("exit status 1")
			},
			wantStatus: StatusUnknown,
			wantErr:    true,
		},
		{
			name:     "a probe that outlives its timeout is unknown",
			lookPath: found,
			run: func(ctx context.Context, _ string) ([]byte, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			wantStatus: StatusUnknown,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probe := Detect(context.Background(),
				WithLookPath(tc.lookPath), WithRunner(tc.run), WithTimeout(50*time.Millisecond))
			if probe.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (err=%v)", probe.Status, tc.wantStatus, probe.Err)
			}
			if tc.wantVersion != "" && probe.Version.String() != tc.wantVersion {
				t.Fatalf("Version = %q, want %q", probe.Version.String(), tc.wantVersion)
			}
			if tc.wantErr && probe.Err == nil {
				t.Fatal("expected an advisory Err for an unknown version")
			}
			if !tc.wantErr && probe.Err != nil {
				t.Fatalf("unexpected advisory Err: %v", probe.Err)
			}
			if tc.wantStatus == StatusNotInstalled && probe.Installed() {
				t.Fatal("Installed() must be false when nothing was found")
			}
		})
	}
}

// TestDetectBoundsRunawayOutput pins that a binary printing far more than a
// version banner cannot flood a log line or a doctor report.
func TestDetectBoundsRunawayOutput(t *testing.T) {
	flood := make([]byte, 4*maxProbeOutput)
	for i := range flood {
		flood[i] = 'x'
	}
	probe := Detect(context.Background(),
		WithLookPath(func(string) (string, error) { return "/bin/devin", nil }),
		WithRunner(func(context.Context, string) ([]byte, error) { return flood, nil }))
	if probe.Status != StatusUnknown {
		t.Fatalf("Status = %q, want %q", probe.Status, StatusUnknown)
	}
	if len([]rune(probe.Reported)) > 120 {
		t.Fatalf("Reported excerpt is %d runes; it must stay bounded", len([]rune(probe.Reported)))
	}
}

// TestDetectAgainstRealProcesses exercises the production runner — the real
// exec path, a real non-zero exit, and a real hang — rather than an injected
// function, so the timeout and the exit handling are proven end to end.
func TestDetectAgainstRealProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binaries are POSIX shell scripts")
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		// #nosec G306 -- these fixtures are fake CLI binaries in t.TempDir() and must be executable.
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	supported := write("devin-supported", "echo 'devin 3000.3.27 (0becb483)'")
	failing := write("devin-failing", "echo 'crash' >&2; exit 3")
	hanging := write("devin-hanging", "sleep 30")

	tests := []struct {
		name       string
		path       string
		wantStatus Status
		// timeout is generous for the two cases that are expected to COMPLETE,
		// because fork+exec of a shell script on a loaded machine is not
		// instant and a tight bound there would only buy flakiness. The hanging
		// case uses a short one because it can only ever expire.
		timeout time.Duration
	}{
		{name: "real supported binary", path: supported, wantStatus: StatusSupported, timeout: 30 * time.Second},
		{name: "real failing binary", path: failing, wantStatus: StatusUnknown, timeout: 30 * time.Second},
		{name: "real hanging binary", path: hanging, wantStatus: StatusUnknown, timeout: 300 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			probe := Detect(context.Background(),
				WithLookPath(func(string) (string, error) { return tc.path, nil }),
				WithTimeout(tc.timeout))
			if probe.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (err=%v, reported=%q)", probe.Status, tc.wantStatus, probe.Err, probe.Reported)
			}
			// The hanging script sleeps for 30 seconds. Returning in a fraction
			// of that proves the timeout AND the WaitDelay really bound the
			// probe, including the grandchild that inherited the output pipe.
			if elapsed := time.Since(start); elapsed > tc.timeout+5*time.Second {
				t.Fatalf("probe took %s with a %s timeout; it was not bounded", elapsed, tc.timeout)
			}
		})
	}
}

func TestVersionAtLeastFloorAndString(t *testing.T) {
	tests := []struct {
		version Version
		want    bool
		render  string
	}{
		{Version{FloorMajor, FloorMinor, -1}, true, "3000.3"},
		{Version{FloorMajor, FloorMinor, 0}, true, "3000.3.0"},
		{Version{FloorMajor, FloorMinor - 1, 99}, false, "3000.2.99"},
		{Version{FloorMajor - 1, 99, 99}, false, "2999.99.99"},
		{Version{FloorMajor + 1, 0, 0}, true, "3001.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.render, func(t *testing.T) {
			if got := tc.version.AtLeastFloor(); got != tc.want {
				t.Fatalf("AtLeastFloor() = %t, want %t", got, tc.want)
			}
			if got := tc.version.String(); got != tc.render {
				t.Fatalf("String() = %q, want %q", got, tc.render)
			}
		})
	}
}
