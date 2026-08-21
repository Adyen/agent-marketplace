//go:build unix

package cache

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestStrayGitAnchorEnvDoesNotHijackProcess guards the re-exec hook. It lives in
// an init(), so a stray environment variable — inherited, exported by mistake, or
// set deliberately — must not be able to turn an ordinary invocation of any binary
// that links this package into a git exec.
func TestStrayGitAnchorEnvDoesNotHijackProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTruncateGitErrorPreservesUTF8AndCharacterLimit$", "-test.v")
	cmd.Env = append(os.Environ(), gitAnchorEnv+"="+gitAnchorValue)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("re-running the test binary with a stray anchor variable failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "PASS") {
		t.Fatalf("expected the test binary to run normally, got:\n%s", output)
	}
}
