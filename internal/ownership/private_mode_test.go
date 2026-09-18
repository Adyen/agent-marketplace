package ownership

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestPrivateNewAggregateModeAndExistingMode(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{"new":{}}}`), Entries: []Entry{mcpEntry("new", "generated", "{}")}, Reason: ReasonSync})
	info, err := os.Stat(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new aggregate mode = %o, want 600", got)
	}

	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		t.Run(strconv.FormatUint(uint64(mode.Perm()), 8), func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(`{"mcpServers":{"old":{}}}`)
			if err := os.Chmod(f.absFile, mode); err != nil {
				t.Fatal(err)
			}
			st := f.store()
			result := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{"updated":{}}}`), Entries: []Entry{mcpEntry("updated", "generated", "{}")}, Reason: ReasonSync})
			info, err := os.Stat(f.absFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != mode.Perm() {
				t.Fatalf("aggregate mode = %o, want %o", got, mode.Perm())
			}
			backup, err := os.ReadFile(result.BackupPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(backup) != `{"mcpServers":{"old":{}}}` {
				t.Fatalf("backup = %q", backup)
			}
			for _, path := range []string{result.BackupPath, metaPathFor(result.BackupPath)} {
				backupInfo, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if backupInfo.Mode().Perm() != 0o600 {
					t.Fatalf("backup mode for %s = %o, want 600", path, backupInfo.Mode().Perm())
				}
			}
		})
	}

	t.Run("identical bytes preserve permissions", func(t *testing.T) {
		f := newFixture(t)
		f.writeAggregate(`{"mcpServers":{"same":{}}}`)
		if err := os.Chmod(f.absFile, 0o640); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(f.absFile)
		if err != nil {
			t.Fatal(err)
		}
		st := f.store()
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{"same":{}}}`), Entries: []Entry{mcpEntry("same", "generated", "{}")}, Reason: ReasonSync})
		after, err := os.Stat(f.absFile)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := f.readAggregate()
		if got != `{"mcpServers":{"same":{}}}` || before.Size() != after.Size() || after.Mode().Perm() != 0o640 {
			t.Fatalf("identical apply changed aggregate bytes or mode: before=%v after=%v content=%q", before, after, got)
		}
	})
}

func TestOwnershipPrivateModesUmaskChild(t *testing.T) {
	if os.Getenv("OWNERSHIP_PRIVATE_MODE_CHILD") == "1" {
		mode, err := strconv.ParseUint(os.Getenv("OWNERSHIP_PRIVATE_MODE_UMASK"), 8, 32)
		if err != nil {
			t.Fatal(err)
		}
		old := syscall.Umask(int(mode))
		defer syscall.Umask(old)
		f := newFixture(t)
		st := f.store()
		mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"mcpServers":{"new":{}}}`), Entries: []Entry{mcpEntry("new", "generated", "{}")}, Reason: ReasonSync})
		info, err := os.Stat(f.absFile)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("new mode = %v, %v", info, err)
		}
		f2 := newFixture(t)
		f2.writeAggregate(`{"mcpServers":{"old":{}}}`)
		if err := os.Chmod(f2.absFile, 0o640); err != nil {
			t.Fatal(err)
		}
		st2 := f2.store()
		mustApply(t, st2, Mutation{Rel: f2.rel, AbsFile: f2.absFile, NextBytes: []byte(`{"mcpServers":{"updated":{}}}`), Entries: []Entry{mcpEntry("updated", "generated", "{}")}, Reason: ReasonSync})
		info, err = os.Stat(f2.absFile)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("existing mode = %v, %v", info, err)
		}
		return
	}
	for _, umask := range []string{"077", "000"} {
		t.Run(umask, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnershipPrivateModesUmaskChild$", "-test.v")
			cmd.Env = append(os.Environ(), "OWNERSHIP_PRIVATE_MODE_CHILD=1", "OWNERSHIP_PRIVATE_MODE_UMASK="+umask)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child: %v\n%s", err, output)
			}
		})
	}
}
