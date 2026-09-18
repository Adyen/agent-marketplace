//go:build unix

package analyze

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// detachedHelperMarker selects the helper entrypoint inside the re-executed
// test binary. It travels through argv because the runner environment is an
// empty allowlist, so marker environment variables cannot reach the child.
const detachedHelperMarker = "am-detached-pipe-holder"

// TestRunnerDetachedHelper is the runner-side helper process. When the test
// binary is re-executed by the runner script with the marker argument, it
// escapes the runner's process group via setsid and then sleeps while holding
// its inherited stdout/stderr pipes open. Without the marker it skips so the
// normal test run is unaffected.
func TestRunnerDetachedHelper(t *testing.T) {
	detached := false
	for _, a := range os.Args[1:] {
		if a == detachedHelperMarker {
			detached = true
		}
	}
	if !detached {
		t.Skip("helper entrypoint for TestRunnerTimeoutDetachedDescendantHoldingPipes")
	}
	if _, err := syscall.Setsid(); err != nil {
		os.Exit(2)
	}
	// Long enough to outlive the runner timeout plus WaitDelay; the process is
	// orphaned but exits on its own.
	time.Sleep(10 * time.Second)
}

// TestRunnerTimeoutDetachedDescendantHoldingPipes proves Analyze returns near
// the configured timeout even when a descendant escapes the process group
// (setsid) and survives the group kill while holding the inherited pipes.
// WaitDelay, not the group kill, is what unblocks Wait's pipe drain here.
func TestRunnerTimeoutDetachedDescendantHoldingPipes(t *testing.T) {
	script, digest := newRunnerScript(t, fmt.Sprintf(`#!/bin/sh
read -r _
# Descendant escapes the process group and holds the inherited pipes.
"%s" -test.run='^TestRunnerDetachedHelper$' %s &
sleep 60
`, os.Args[0], detachedHelperMarker))

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              300 * time.Millisecond,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	start := time.Now()
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q", err.Error())
	}
	// With the bounded WaitDelay the call returns shortly after the timeout
	// (timeout + runnerWaitDelay); without it Wait blocks until the escaped
	// helper exits (~10s), so the bound stays deterministic on both sides.
	if elapsed > 8*time.Second {
		t.Fatalf("Analyze blocked on escaped descendant holding pipes: %v", elapsed)
	}
}
