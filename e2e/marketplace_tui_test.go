package e2e_test

import (
	"bytes"
	"io"
	"os"
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

func waitForMarketplaceTUIRow(t *testing.T, terminal *os.File, output *synchronizedBuffer, prefix string) {
	t.Helper()
	boundary := len(output.String())
	cols := uint16(160)
	waitForMarketplaceTUI(t, output, func(value string) bool {
		emitted := value[boundary:]
		if strings.Contains(emitted, prefix) && strings.Contains(emitted, "/skill-one") {
			return true
		}
		cols = 319 - cols
		if err := pty.Setsize(terminal, &pty.Winsize{Rows: 40, Cols: cols}); err != nil {
			t.Fatalf("resize Marketplace TUI: %v", err)
		}
		return false
	})
}

func TestMarketplaceTUIDiscoversAndInstallsExactPin(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("TUI discovery install", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	evidence := assertMarketplaceArtifactIntegrity(t, fixture.server.URL, "behavior-marketplace", "behavior-source", "behavior-source-id", "skill", "skill-one")
	digest := evidence.ContentDigest
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
	waitForMarketplaceTUI(t, &output, func(value string) bool {
		return strings.Contains(value, "skill-one") && strings.Contains(value, "Available")
	})
	time.Sleep(500 * time.Millisecond)
	for _, key := range []byte("/skill-one\r ") {
		if _, err := terminal.Write([]byte{key}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool { return strings.Contains(value, "use installation scopes") })
	if _, err := terminal.Write([]byte("p")); err != nil {
		t.Fatal(err)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool {
		return (consumer.CheckFileContains("/workspace/.agents/config.yaml", "behavior-source/skill/skill-one") &&
			consumer.CheckFileContains("/workspace/.agents/lock.yaml", digest)) || strings.Contains(value, "Install this exact verified revision?")
	})
	if strings.Contains(output.String(), "Install this exact verified revision?") {
		if _, err := terminal.Write([]byte("y")); err != nil {
			t.Fatal(err)
		}
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool {
		return consumer.CheckFileContains("/workspace/.agents/config.yaml", "behavior-source/skill/skill-one") &&
			consumer.CheckFileContains("/workspace/.agents/lock.yaml", digest) && strings.Contains(value, "enabled")
	})
	config := consumer.ReadFile("/workspace/.agents/config.yaml")
	for _, value := range []string{"apiVersion: agent-marketplace/v4", "behavior-source/skill/skill-one"} {
		if !strings.Contains(config, value) {
			t.Fatalf("project Marketplace intent missing %q:\n%s", value, config)
		}
	}
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", evidence)
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "TUI discovery install")
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

func TestMarketplaceTUIToggleCopiesExactPinToLocalScope(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("TUI exact pin", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	evidence := assertMarketplaceArtifactIntegrity(t, fixture.server.URL, "behavior-marketplace", "behavior-source", "behavior-source-id", "skill", "skill-one")
	digest := evidence.ContentDigest
	result := consumer.RunAM("install", "--scope", "repo", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace install before TUI failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.agents/config.yaml", "behavior-source/skill/skill-one")
	projectConfig := consumer.ReadFile("/workspace/.agents/config.yaml")
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", evidence)
	projectLock := consumer.ReadFile("/workspace/.agents/lock.yaml")

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
	for _, key := range []byte("/skill-one\r ") {
		if _, err := terminal.Write([]byte{key}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool {
		return strings.Contains(value, "\"skill-one\"") && strings.Contains(value, "use installation scopes")
	})
	waitForMarketplaceTUI(t, &output, func(value string) bool {
		return strings.Contains(value, "P   skill ") && strings.Contains(value, "/skill-one")
	})
	if _, err := terminal.Write([]byte("l")); err != nil {
		t.Fatal(err)
	}
	waitForMarketplaceTUI(t, &output, func(string) bool {
		return consumer.CheckFileContains("/workspace/.agents/config.local.yaml", "behavior-source/skill/skill-one") &&
			consumer.CheckFileContains("/workspace/.agents/lock.local.yaml", digest)
	})
	config := consumer.ReadFile("/workspace/.agents/config.local.yaml")
	for _, value := range []string{"apiVersion: agent-marketplace/v4", "marketplace:", fixture.server.URL, "behavior-source/skill/skill-one"} {
		if !strings.Contains(config, value) {
			t.Fatalf("local Marketplace pin missing %q:\n%s", value, config)
		}
	}
	if strings.Contains(config, "selected_by:") || strings.Contains(config, "marketplaces:") {
		t.Fatalf("direct TUI scope copy wrote legacy keys:\n%s", config)
	}
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.local.yaml", "local", fixture.server.URL, "skill", "skill-one", evidence)
	if consumer.ReadFile("/workspace/.agents/config.yaml") != projectConfig || consumer.ReadFile("/workspace/.agents/lock.yaml") != projectLock {
		t.Fatal("copying the TUI pin into local scope changed repository intent or its exact acquisition record")
	}
	waitForMarketplaceTUIRow(t, terminal, &output, "PL  skill ")
	for _, key := range []byte(" l") {
		if _, err := terminal.Write([]byte{key}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	waitForMarketplaceTUI(t, &output, func(string) bool {
		return !consumer.CheckFileContains("/workspace/.agents/config.local.yaml", "behavior-source/skill/skill-one")
	})
	waitForMarketplaceTUIRow(t, terminal, &output, "P   skill ")
	assertBehaviorMarketplaceLockHasNoResources(t, consumer, "/workspace/.agents/lock.local.yaml", "local")
	if consumer.ReadFile("/workspace/.agents/config.yaml") != projectConfig || consumer.ReadFile("/workspace/.agents/lock.yaml") != projectLock {
		t.Fatal("removing the local TUI pin changed repository intent or its exact acquisition record")
	}
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", evidence)
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "TUI exact pin")
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
