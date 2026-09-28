package main

import (
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "pgregory.net/rapid"
)

func TestRapidProductionDependencyGuard(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "github.com/Adyen/agent-marketplace/cmd/agent-marketplace")
	modcache := os.Getenv("GOMODCACHE")
	if modcache == "" && build.Default.GOPATH != "" {
		modcache = filepath.Join(filepath.SplitList(build.Default.GOPATH)[0], "pkg", "mod")
	}
	if modcache != "" {
		cmd.Env = append(os.Environ(), "GOMODCACHE="+modcache)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	deps := string(out)
	if !strings.Contains(deps, "github.com/spf13/cobra") {
		t.Fatalf("non-vacuity control failed: github.com/spf13/cobra missing from production dependency graph:\n%s", deps)
	}
	if strings.Contains(deps, "pgregory.net/rapid") {
		t.Fatalf("pgregory.net/rapid must stay test-only; it appears in the production dependency graph:\n%s", deps)
	}
}
