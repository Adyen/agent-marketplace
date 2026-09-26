//go:build e2e

package dependencytxn

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CrashExitCode is the status an e2e build exits with when
// AGENT_MARKETPLACE_E2E_CRASH_AT names the fault point being reached.
const CrashExitCode = 137

// AGENT_MARKETPLACE_E2E_CRASH_AT=<stage>:<index> (for example
// after-publish:0) kills the process at that fault point, the way a SIGKILL
// or power loss would: no deferred cleanup, no returned error.
func init() {
	spec := os.Getenv("AGENT_MARKETPLACE_E2E_CRASH_AT")
	if spec == "" {
		return
	}
	stage, rawIndex, ok := strings.Cut(spec, ":")
	index, err := strconv.Atoi(rawIndex)
	if !ok || err != nil || stage == "" {
		fmt.Fprintf(os.Stderr, "invalid AGENT_MARKETPLACE_E2E_CRASH_AT %q; want <stage>:<index>\n", spec)
		os.Exit(2)
	}
	defaultFault = func(point FaultPoint) error {
		if string(point.Stage) == stage && point.Index == index {
			os.Exit(CrashExitCode)
		}
		return nil
	}
}
