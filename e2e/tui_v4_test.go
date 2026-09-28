package e2e_test

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// A user whose config is v4 opens the TUI: it lists what the v4 sources
// select and exits cleanly on Ctrl-C without changing the workspace.
func TestTUIStartsFromV4UserConfig(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	url := env.CreateBareRepo("tui-v4", RegistryFilesEx([]Skill{{Name: "review", Description: "Review changes"}}, nil, nil, nil))
	env.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n", url))
	env.AssertExitCode(env.RunAM("sync"), 0)
	before := env.ReadFile("/root/.agents/config.yaml")

	cmd := exec.Command(env.binary, "tui")
	cmd.Dir = env.workspace
	cmd.Env = append(env.subprocessEnv(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatalf("start TUI: %v", err)
	}
	defer terminal.Close()
	var output synchronizedBuffer
	go func() { _, _ = io.Copy(&output, terminal) }()
	if _, err := terminal.Write([]byte("\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;1R\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;1R")); err != nil {
		t.Fatal(err)
	}
	waitForMarketplaceTUI(t, &output, func(value string) bool { return strings.Contains(value, "review") })
	if _, err := terminal.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("TUI exited with %v:\n%s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("TUI did not exit after Ctrl-C")
	}
	if got := env.ReadFile("/root/.agents/config.yaml"); got != before {
		t.Fatalf("opening and closing the TUI changed the v4 user config:\n%s", got)
	}
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)
}
