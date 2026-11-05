package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotConfigFileRestoresSymlinkTargetWithoutReplacingLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.yaml")
	link := filepath.Join(dir, "config.yaml")
	original := []byte("original\x00bytes\n")
	if err := os.WriteFile(target, original, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotConfigFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored target = %q, want %q", got, original)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rollback replaced config symlink")
	}
}

func TestConfigPathLockCanonicalizesSymlinkAliases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.yaml")
	link := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(target, []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withConfigPathLock(link, func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withConfigPathLock(target, func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("symlink and target paths acquired different locks")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first lock: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("target path did not acquire the lock after release")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second lock: %v", err)
	}
}

func TestSnapshotConfigFileRefusesNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	_, err := SnapshotConfigFile(dir)
	if err == nil {
		t.Fatal("SnapshotConfigFile accepted a directory")
	}
}
