//go:build unix

package processstart

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestDetachedCancelKillsProcessGroupAndReleasesOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lockedBuffer{}
	cmd := Detached(ctx, "sh", "-c", "sleep 30 & echo $!; wait")
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "\n") {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid was not printed; output = %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	grandchild, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) }()

	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cmd.Wait did not return after cancel; a grandchild kept stdout open")
	}

	deadline = time.Now().Add(5 * time.Second)
	for !errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d survived cancellation", grandchild)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCommandLeavesSysProcAttrUnsetAndDetachedSetsid(t *testing.T) {
	ctx := context.Background()

	command := Command(ctx, "true")
	if command.SysProcAttr != nil {
		t.Fatalf("Command SysProcAttr = %#v, want nil", command.SysProcAttr)
	}

	detached := Detached(ctx, "true")
	if detached.SysProcAttr == nil || !detached.SysProcAttr.Setsid {
		t.Fatalf("Detached SysProcAttr = %#v, want Setsid", detached.SysProcAttr)
	}
}
