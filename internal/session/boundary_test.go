package session_test

import (
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The interactive-session core must stay usable by non-TUI surfaces: no
// direct or transitive dependency on Bubble Tea, Lipgloss, Huh, Bubbles, or
// any other charm.land / github.com/charmbracelet package may appear in its
// import graph.
func TestSessionCoreHasNoCharmDependency(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	cmd := exec.Command("go", "list", "-mod=readonly", "-deps", "github.com/Adyen/agent-marketplace/internal/session")
	cmd.Dir = filepath.Dir(thisFile)
	modcache := os.Getenv("GOMODCACHE")
	if modcache == "" && build.Default.GOPATH != "" {
		modcache = filepath.Join(filepath.SplitList(build.Default.GOPATH)[0], "pkg", "mod")
	}
	if modcache != "" {
		cmd.Env = append(os.Environ(), "GOMODCACHE="+modcache)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -mod=readonly -deps: %v\n%s", err, output)
	}

	var charmPackages []string
	foundSession, foundPipeline := false, false
	for _, dep := range strings.Fields(string(output)) {
		if dep == "github.com/Adyen/agent-marketplace/internal/session" {
			foundSession = true
		}
		if dep == "github.com/Adyen/agent-marketplace/internal/pipeline" {
			foundPipeline = true
		}
		if strings.HasPrefix(dep, "charm.land/") || strings.HasPrefix(dep, "github.com/charmbracelet/") {
			charmPackages = append(charmPackages, dep)
		}
	}
	if !foundSession || !foundPipeline {
		t.Fatalf("non-vacuity controls missing from session dependency graph: session=%t pipeline=%t\n%s", foundSession, foundPipeline, output)
	}
	if len(charmPackages) > 0 {
		t.Fatalf("internal/session transitively imports Charm packages: %v", charmPackages)
	}
}
