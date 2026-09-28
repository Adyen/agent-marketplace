package e2e_test

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// binaryPath holds the path to the built agent-marketplace binary,
// set once by TestMain.
var binaryPath string

func TestMain(m *testing.M) {
	flag.Parse()
	parallelSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "test.parallel" {
			parallelSet = true
		}
	})
	if !parallelSet {
		if err := flag.Set("test.parallel", strconv.Itoa(e2eParallelism())); err != nil {
			log.Fatal(err)
		}
	}
	// Resolve project root (one level up from e2e/).
	root, err := filepath.Abs("..")
	if err != nil {
		log.Fatalf("e2e: resolve project root: %v", err)
	}

	// Build the binary for the native platform. The e2e build tag enables
	// the test-only AGENT_MARKETPLACE_V3_ORG_CONFIG override used by the
	// sandbox framework to isolate org config paths.
	binaryDir, err := os.MkdirTemp("", "agent-marketplace-e2e-")
	if err != nil {
		log.Fatalf("e2e: create binary directory: %v", err)
	}
	binaryPath = filepath.Join(binaryDir, "agent-marketplace-e2e")
	log.Println("e2e: building agent-marketplace binary ...")
	build := exec.Command("go", "build",
		"-tags", "e2e",
		"-ldflags", "-X main.version=e2e-test",
		"-o", binaryPath,
		"./cmd/agent-marketplace",
	)
	build.Dir = root
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		_ = os.RemoveAll(binaryDir)
		log.Fatalf("e2e: build binary: %v", err)
	}

	e2eInvocationDir, err = os.MkdirTemp("", "agent-marketplace-e2e-invocations-")
	if err != nil {
		_ = os.RemoveAll(binaryDir)
		log.Fatalf("e2e: create invocation directory: %v", err)
	}

	code := m.Run()
	if code == 0 {
		if err := checkCommandCoverage(binaryPath); err != nil {
			log.Printf("e2e: %v", err)
			code = 1
		}
	}
	if err := os.RemoveAll(e2eInvocationDir); err != nil {
		slog.Error("e2e: remove invocation directory", "path", e2eInvocationDir, "err", err)
		code = 1
	}
	if err := os.RemoveAll(binaryDir); err != nil {
		slog.Error("e2e: remove binary", "path", binaryPath, "err", err)
		code = 1
	}
	if err := cleanupShadowRoot(); err != nil {
		slog.Error("e2e: clean up shadow root", "err", err)
		code = 1
	}
	os.Exit(code)
}

// buildRepoScript returns a shell script that creates a bare git repo at
// reposDir/<name>.git with a working clone, writes the provided files,
// commits, and pushes. The files map is path -> content (relative to the
// working clone root).
func buildRepoScript(reposDir, name string, files map[string]string) string {
	barePath := filepath.Join(reposDir, name+".git")
	workPath := filepath.Join(reposDir, "work-"+name)
	script := fmt.Sprintf(`set -e
mkdir -p %q
git init --bare --initial-branch=main %q
git -c init.defaultBranch=main clone %q %q
cd %q
`, reposDir, barePath, barePath, workPath, workPath)

	for path, content := range files {
		dir := filepath.Dir(path)
		if dir != "." {
			script += fmt.Sprintf("mkdir -p %q\n", dir)
		}
		script += fmt.Sprintf("cat > %q << 'SKILLEOF'\n%s\nSKILLEOF\n", path, content)
	}

	script += fmt.Sprintf(`git add .
git -c user.name='E2E Test' -c user.email='e2e@test.local' commit -m "seed %s"
git branch -m main
git push origin main
rm -rf %q
`, name, workPath)

	return script
}
