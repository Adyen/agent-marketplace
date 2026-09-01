package e2e_test

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func waitForMarketplaceTUI(t *testing.T, output *synchronizedBuffer, condition func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition(output.String()) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Marketplace TUI:\n%s", output.String())
}

func TestMarketplaceTUIToggleCopiesExactPinToLocalScope(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("TUI exact pin", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	digest := fixture.resourceDigest(t, "skill", "skill-one")
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace install before TUI failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}

	cmd := exec.Command(consumer.binary, "tui")
	cmd.Dir = consumer.workspace
	cmd.Env = append(consumer.subprocessEnv(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatalf("start Marketplace TUI: %v", err)
	}
	defer terminal.Close()
	var output synchronizedBuffer
	go func() { _, _ = io.Copy(&output, terminal) }()
	if _, err := terminal.Write([]byte("\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;1R")); err != nil {
		t.Fatal(err)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool { return strings.Contains(value, "skill-one") })
	for _, key := range []byte("jjj ") {
		if _, err := terminal.Write([]byte{key}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool { return strings.Contains(value, "use installation scopes") })
	if _, err := terminal.Write([]byte("l")); err != nil {
		t.Fatal(err)
	}
	waitForMarketplaceTUI(t, &output, func(string) bool { return consumer.CheckFileContains("/workspace/.agents/config.local.yaml", digest) })
	config := consumer.ReadFile("/workspace/.agents/config.local.yaml")
	for _, value := range []string{digest, "registry_id: behavior-source-id", "kind: skill", "name: skill-one", "direct: true"} {
		if !strings.Contains(config, value) {
			t.Fatalf("local Marketplace pin missing %q:\n%s", value, config)
		}
	}
	if strings.Contains(config, "selected_by:") {
		t.Fatalf("direct TUI scope copy retained collection selectors:\n%s", config)
	}
	for _, key := range []byte(" l") {
		if _, err := terminal.Write([]byte{key}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	waitForMarketplaceTUI(t, &output, func(string) bool { return !consumer.CheckFileContains("/workspace/.agents/config.local.yaml", digest) })
	if !consumer.CheckFileContains("/workspace/.agents/config.yaml", digest) {
		t.Fatal("removing the local TUI pin changed the project pin")
	}
	if _, err := terminal.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("Marketplace TUI did not exit after Ctrl-C")
	}
}
