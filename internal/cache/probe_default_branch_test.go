package cache

import (
	"context"
	"testing"
)

// A credential helper in the current directory's repository configuration
// authenticates plain ls-remote, but must not authenticate the reachability
// probe, or a private repository would look anonymously reachable.
//
// spec: resource-installation/fallback-probe-ignores-credential-helpers
func TestProbeDefaultBranchIgnoresLocalCredentialHelper(t *testing.T) {
	isolatedGitAuth(t)
	f := newGitAuthServer(t)
	t.Setenv("GIT_SSL_CAINFO", f.ca)

	workspace := t.TempDir()
	gitTest(t, workspace, "init", "-q")
	gitTest(t, workspace, "config", "credential.helper", "!f() { echo username=fixture-user; echo password=synthetic-auth-secret; }; f")
	t.Chdir(workspace)

	// Control: the local helper does authenticate ordinary ls-remote.
	_, _ = DefaultBranch(f.url)
	if f.accepted.Load() == 0 {
		t.Fatal("control: DefaultBranch did not authenticate through the local credential helper")
	}

	accepted := f.accepted.Load()
	if branch, _, err := ProbeDefaultBranch(context.Background(), f.url); err == nil {
		t.Fatalf("ProbeDefaultBranch reached an authenticated remote (branch %q)", branch)
	}
	if got := f.accepted.Load(); got != accepted {
		t.Fatalf("ProbeDefaultBranch authenticated %d request(s) through the local credential helper", got-accepted)
	}
	if f.challenged.Load() == 0 {
		t.Fatal("ProbeDefaultBranch never reached the server")
	}

	challenged := f.challenged.Load()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ProbeDefaultBranch(cancelled, f.url); err == nil {
		t.Fatal("ProbeDefaultBranch ignored a cancelled context")
	}
	if got := f.challenged.Load(); got != challenged {
		t.Fatalf("ProbeDefaultBranch with a cancelled context reached the server %d time(s)", got-challenged)
	}
}
