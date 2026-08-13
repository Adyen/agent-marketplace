package e2e_test

import (
	"strings"
	"testing"
)

func TestResolvePathRejectsTraversal(t *testing.T) {
	env := &Env{
		home:      "/sandbox/home",
		workspace: "/sandbox/workspace",
		repos:     "/sandbox/repos",
		etc:       "/sandbox/etc/agent-marketplace",
	}

	for _, tc := range []struct {
		path   string
		prefix string
	}{
		{path: "/root/../outside", prefix: "/root"},
		{path: "/workspace/subdir/../../outside", prefix: "/workspace"},
		{path: "/repos/../outside", prefix: "/repos"},
		{path: "/etc/agent-marketplace/../../outside", prefix: "/etc/agent-marketplace"},
	} {
		_, err := env.resolvePath(tc.path)
		if err == nil {
			t.Errorf("resolvePath(%q) returned nil error", tc.path)
			continue
		}
		if !strings.Contains(err.Error(), tc.prefix) {
			t.Errorf("resolvePath(%q) error = %v, want mapping context %q", tc.path, err, tc.prefix)
		}
	}
}
