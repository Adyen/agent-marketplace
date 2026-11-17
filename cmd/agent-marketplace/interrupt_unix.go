//go:build unix

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// interruptGracePeriod bounds how long the command may keep running after the
// first interrupt cancels its context, before the signal is re-raised.
const interruptGracePeriod = time.Second

// commandInterruptContext returns a context cancelled by the first SIGINT or
// SIGTERM. Call finish once the command returns.
func commandInterruptContext() (ctx context.Context, finish func()) {
	return watchInterrupts(context.Background(), interruptGracePeriod, func(sig os.Signal) {
		_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
	})
}

// watchInterrupts cancels the returned context on the first interrupt. If the
// command has not returned after grace, it re-raises that signal through
// reraise, so the default disposition terminates the process or a handler
// registered by the command absorbs it. A command that returns within grace
// handles its own exit.
func watchInterrupts(parent context.Context, grace time.Duration, reraise func(os.Signal)) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	returned := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case sig := <-signals:
			cancel()
			select {
			case <-returned:
			case <-time.After(grace):
				signal.Stop(signals)
				reraise(sig)
			}
		case <-returned:
		}
	}()
	return ctx, func() {
		close(returned)
		<-stopped
		signal.Stop(signals)
		cancel()
	}
}
