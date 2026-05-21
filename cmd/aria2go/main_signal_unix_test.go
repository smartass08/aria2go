//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestSignalHandlingSetup(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-sigCh:
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}()

	signal.Reset(syscall.SIGINT)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		sigCh <- syscall.SIGINT
	}
}

func TestSignalHandling(t *testing.T) {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan os.Signal, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		count := 0
		for {
			select {
			case sig := <-sigCh:
				received <- sig
				count++
				if count >= 2 {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Logf("cannot send SIGINT: %v", err)
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Logf("cannot send SIGTERM: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for signal handlers")
	}

	if len(received) < 1 {
		t.Error("expected at least one signal to be received")
	}
}
