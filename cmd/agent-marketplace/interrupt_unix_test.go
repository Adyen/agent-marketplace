//go:build unix

package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/processstart"
)

// spec: git-authentication/interrupting-the-cli-stops-its-git-subprocesses
func TestInterruptCancelsDetachedSubprocess(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	reraised := make(chan os.Signal, 1)
	ctx, finish := watchInterrupts(context.Background(), interruptGracePeriod, func(sig os.Signal) { reraised <- sig })
	cmd := processstart.Detached(ctx, "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("detached sleep exited normally after interrupt")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("detached sleep survived the interrupt")
	}

	finish()
	select {
	case sig := <-reraised:
		t.Fatalf("signal %v re-raised although the command returned promptly", sig)
	default:
	}
}

func TestInterruptReraisesWhenCommandDoesNotReturn(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	reraised := make(chan os.Signal, 1)
	_, finish := watchInterrupts(context.Background(), 50*time.Millisecond, func(sig os.Signal) { reraised <- sig })
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-reraised:
		if sig != syscall.SIGINT {
			t.Fatalf("re-raised signal = %v, want %v", sig, syscall.SIGINT)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("interrupt was not re-raised after the grace period")
	}
	finish()
}

func TestInterruptWatchDoesNotReraiseWithoutSignal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	reraised := make(chan os.Signal, 1)
	_, finish := watchInterrupts(context.Background(), interruptGracePeriod, func(sig os.Signal) { reraised <- sig })
	finish()
	select {
	case sig := <-reraised:
		t.Fatalf("signal %v re-raised without an interrupt", sig)
	default:
	}
}
