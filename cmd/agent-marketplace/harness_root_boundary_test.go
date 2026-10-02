package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/devincli"
)

// spec: materialization/doctor-continues-diagnosing-an-unsafe-native-root
func TestDoctorContinuesDiagnosingUnsafeNativeRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _ := devinDoctorEnv(t)
	root := filepath.Join(repo, ".codex")
	const sentinel = "not a directory; preserve this payload"
	if err := os.WriteFile(root, []byte(sentinel), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runDoctor(doctorOptions{out: &output, in: strings.NewReader(""), isTTY: false, probe: staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled})})
	if err != nil {
		t.Fatalf("diagnostic-only doctor aborted: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "unresolvable:") || !strings.Contains(output.String(), "factory repo=") {
		t.Fatalf("doctor omitted unsafe root or independent target: %s", output.String())
	}
	if strings.Contains(output.String(), sentinel) {
		t.Fatal("doctor printed root payload")
	}
	if data, err := os.ReadFile(root); err != nil || string(data) != sentinel {
		t.Fatalf("doctor changed root bytes: %q %v", data, err)
	}
	if after, err := os.Stat(root); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("doctor changed root metadata: %v", err)
	}
}
