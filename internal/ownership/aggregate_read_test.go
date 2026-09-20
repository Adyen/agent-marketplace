package ownership

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadAggregateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aggregate.json")
	for _, data := range [][]byte{nil, bytes.Repeat([]byte("x"), 4096)} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ReadAggregateFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("got %d bytes err=%v", len(got), err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("read changed regular file: %v", err)
		}
	}
	if _, err := ReadAggregateFile(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) || !os.IsNotExist(err) {
		t.Fatalf("missing identity lost: %v", err)
	}
	for _, kind := range []string{"directory", "symlink", "dangling", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			p := filepath.Join(dir, kind)
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(p, 0700)
			case "symlink":
				err = os.Symlink(path, p)
			case "dangling":
				err = os.Symlink(filepath.Join(dir, "absent"), p)
			case "hardlink":
				err = os.Link(path, p)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadAggregateFile(p)
			if got != nil || !errors.Is(err, ErrUnsafeAggregateFile) || os.IsNotExist(err) {
				t.Fatalf("unsafe input returned %q %v", got, err)
			}
			if (kind == "symlink" || kind == "dangling") && !errors.Is(err, syscall.ELOOP) {
				t.Fatalf("lost symlink error: %v", err)
			}
			after, err := os.Lstat(p)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unsafe file changed: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, bytes.Repeat([]byte("x"), 4096)) {
				t.Fatalf("external bytes changed: %v", err)
			}
		})
	}
	if data, err := ReadAggregateFile("/dev/null"); data != nil || !errors.Is(err, ErrUnsafeAggregateFile) {
		t.Fatalf("device accepted: %q %v", data, err)
	}
}

func TestAggregateConsumersRejectFIFO(t *testing.T) {
	if os.Getenv("OWNED_AGGREGATE_FIFO_CHILD") == "1" {
		f := newFixture(t)
		f.writeAggregate("{}\n")
		store := f.store()
		mode := os.Getenv("OWNED_AGGREGATE_FIFO_MODE")
		protected := map[string][]byte{}
		if mode == "recovery" {
			crashing := f.store(WithCrasher(CrashAt(CrashAfterStagedWrite)))
			_, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte("{\"new\":true}\n"), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte("{}\n"))})
			if !errors.Is(err, ErrInjectedCrash) {
				t.Fatalf("prepare journal: %v", err)
			}
			journal, err := store.ReadJournal()
			if err != nil || journal == nil {
				t.Fatalf("read prepared journal: %v", err)
			}
			for _, path := range []string{store.JournalFile(), journal.StagedPath, journal.BackupPath, metaPathFor(journal.BackupPath)} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				protected[path] = data
			}
		}
		if err := os.Rename(f.absFile, f.absFile+".saved"); err != nil {
			t.Fatal(err)
		}
		fifo := f.absFile
		if os.Getenv("OWNED_AGGREGATE_FIFO_LINK") == "1" {
			fifo = filepath.Join(f.targetRoot, "fifo")
		}
		if err := syscall.Mkfifo(fifo, 0600); err != nil {
			t.Fatal(err)
		}
		if fifo != f.absFile {
			if err := os.Symlink(fifo, f.absFile); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Lstat(fifo)
		if err != nil {
			t.Fatal(err)
		}
		invoke := func() error {
			switch mode {
			case "read":
				_, err := ReadAggregateFile(f.absFile)
				return err
			case "backup":
				_, err := store.BackupOnly(f.rel, f.absFile, ReasonSync)
				return err
			case "cas":
				return store.assertPrecondition(Mutation{AbsFile: f.absFile, ExpectPriorDigest: Digest([]byte("{}\n"))})
			case "recovery":
				_, err := store.Recover()
				return err
			default:
				t.Fatalf("unknown mode %s", mode)
				return nil
			}
		}
		if err := invoke(); !errors.Is(err, ErrUnsafeAggregateFile) {
			t.Fatalf("%s error = %v", mode, err)
		}
		after, err := os.Lstat(fifo)
		if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
			t.Fatalf("FIFO changed: %v", err)
		}
		if fileExists(t, store.StateFile()) {
			t.Fatal("refusal published ownership state")
		}
		if mode != "recovery" {
			if fileExists(t, store.JournalFile()) || fileExists(t, store.SafetyBackupDir(f.rel)) {
				t.Fatal("refusal published journal/backup")
			}
		}
		for path, want := range protected {
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("refusal changed recovery evidence %s: %v", path, err)
			}
		}
		if err := os.Remove(f.absFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(f.absFile+".saved", f.absFile); err != nil {
			t.Fatal(err)
		}
		if err := invoke(); err != nil {
			t.Fatalf("regular-file control: %v", err)
		}
		if mode == "recovery" && fileExists(t, store.JournalFile()) {
			t.Fatal("regular-file recovery did not complete")
		}
		return
	}
	for _, mode := range []string{"read", "backup", "cas", "recovery"} {
		for _, link := range []string{"0", "1"} {
			t.Run(mode+"/link="+link, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAggregateConsumersRejectFIFO$")
				cmd.Env = append(os.Environ(), "OWNED_AGGREGATE_FIFO_CHILD=1", "OWNED_AGGREGATE_FIFO_MODE="+mode, "OWNED_AGGREGATE_FIFO_LINK="+link)
				output, err := cmd.CombinedOutput()
				if err != nil || ctx.Err() != nil {
					t.Fatalf("child error=%v deadline=%v output=%s", err, ctx.Err(), output)
				}
			})
		}
	}
}
