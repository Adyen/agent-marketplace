package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareRemoteFixturePushStartsNoAutoMaintenance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, working := createBareRepoWithWorkingClone(t, "origin.git")
	writeSkill(t, working, "a", "alpha", nil)
	runGit(t, working, "add", ".")
	runGit(t, working, "commit", "-m", "seed")
	trace := filepath.Join(t.TempDir(), "push.trace")
	cmd := exec.Command("git", "push", "origin", "main")
	cmd.Dir = working
	cmd.Env = append(os.Environ(), "GIT_TRACE="+trace)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "receive-pack") {
		t.Fatalf("push trace did not record the fixture remote's receive-pack:\n%s", data)
	}
	if strings.Contains(string(data), "maintenance run") {
		t.Fatalf("push into the fixture bare remote started auto maintenance that can outlive the test:\n%s", data)
	}
}
