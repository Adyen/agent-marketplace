//go:build unix

package fsboundary

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func tempRoot(t *testing.T) (*Dir, string) {
	t.Helper()
	return captureRoot(t, t.TempDir())
}

func captureRoot(t *testing.T, path string) (*Dir, string) {
	t.Helper()
	root, err := OpenDir(path)
	if err != nil {
		t.Fatalf("OpenDir(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, root.Path()
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func statOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}

func lstatOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return info
}

func TestDirOpenFileReturnsBlockingCloseOnExecHandle(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "payload"), []byte("inert regular control"), 0o640)
	before := identityFromFileInfo(t, statOf(t, filepath.Join(path, "payload")))
	file, identity, err := root.OpenFile("payload")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var statusFlags, descriptorFlags int
	var inspectErr error
	if err := raw.Control(func(fd uintptr) {
		statusFlags, inspectErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
		if inspectErr == nil {
			descriptorFlags, inspectErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
		}
	}); err != nil || inspectErr != nil {
		t.Fatalf("inspect admitted descriptor: %v %v", err, inspectErr)
	}
	if statusFlags&unix.O_NONBLOCK != 0 || descriptorFlags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("wrong admitted flags: status=%#x descriptor=%#x", statusFlags, descriptorFlags)
	}
	if !identity.SameState(before) {
		t.Fatalf("opened identity differs from regular control: %+v %+v", identity, before)
	}
	if data, err := io.ReadAll(file); err != nil || string(data) != "inert regular control" {
		t.Fatalf("admitted regular read failed: %q %v", data, err)
	}
	if after := identityFromFileInfo(t, statOf(t, filepath.Join(path, "payload"))); !after.SameState(before) {
		t.Fatal("regular open/read changed payload state")
	}
}

func TestOpenDirValidation(t *testing.T) {
	t.Run("relative path refused", func(t *testing.T) {
		if _, err := OpenDir("relative/path"); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("OpenDir(relative) error = %v, want ErrUnsafePath", err)
		}
	})
	t.Run("missing path reports not-exist", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "absent")
		if _, err := OpenDir(missing); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("OpenDir(missing) error = %v, want fs.ErrNotExist", err)
		}
	})
	t.Run("regular file refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		mustWrite(t, path, []byte("x"), 0o600)
		if _, err := OpenDir(path); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("OpenDir(file) error = %v, want ErrUnsafePath", err)
		}
	})
	t.Run("symlinked root is captured canonically", func(t *testing.T) {
		base := t.TempDir()
		real := filepath.Join(base, "real")
		if err := os.Mkdir(real, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		root, err := OpenDir(link)
		if err != nil {
			t.Fatalf("OpenDir(symlinked root): %v", err)
		}
		defer func() { _ = root.Close() }()
		resolved, err := filepath.EvalSymlinks(real)
		if err != nil {
			t.Fatal(err)
		}
		if root.Path() != resolved {
			t.Fatalf("captured path = %q, want canonical %q", root.Path(), resolved)
		}
		info := statOf(t, resolved)
		if !root.Identity().SameObject(identityFromFileInfo(t, info)) {
			t.Fatal("captured root identity does not match the canonical directory")
		}
	})
}

func identityFromFileInfo(t *testing.T, info os.FileInfo) Identity {
	t.Helper()
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no stat_t for %s", info.Name())
	}
	return Identity{
		Device:       uint64(st.Dev),
		Inode:        st.Ino,
		Size:         st.Size,
		ModTimeNS:    info.ModTime().UnixNano(),
		ChangeTimeNS: changeTimeNS(st),
		Links:        uint64(st.Nlink),
		Mode:         info.Mode().Perm(),
		UID:          st.Uid,
	}
}

func TestDirOpenFileShapes(t *testing.T) {
	root, rootPath := tempRoot(t)

	t.Run("direct regular file is the positive control", func(t *testing.T) {
		mustWrite(t, filepath.Join(rootPath, "ok.yaml"), []byte("a: b\n"), 0o640)
		file, id, err := root.OpenFile("ok.yaml")
		if err != nil {
			t.Fatalf("OpenFile: %v", err)
		}
		defer func() { _ = file.Close() }()
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(content) != "a: b\n" {
			t.Fatalf("content = %q", content)
		}
		want := identityFromFileInfo(t, statOf(t, filepath.Join(rootPath, "ok.yaml")))
		if !id.SameState(want) {
			t.Fatalf("identity %+v does not match stat %+v", id, want)
		}
	})

	t.Run("directory leaf refused", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(rootPath, "dir"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, _, err := root.OpenFile("dir")
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("OpenFile(dir) error = %v, want ErrUnsafeFile", err)
		}
	})

	t.Run("FIFO refused without blocking", func(t *testing.T) {
		path := filepath.Join(rootPath, "fifo")
		if err := unix.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, _, err := root.OpenFile("fifo")
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrUnsafeFile) {
				t.Fatalf("OpenFile(fifo) error = %v, want ErrUnsafeFile", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("OpenFile(fifo) blocked")
		}
	})

	t.Run("unix socket refused without blocking", func(t *testing.T) {
		path := filepath.Join(rootPath, "sock")
		listener, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = listener.Close() }()
		done := make(chan error, 1)
		go func() {
			_, _, err := root.OpenFile("sock")
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrUnsafeFile) {
				t.Fatalf("OpenFile(socket) error = %v, want ErrUnsafeFile", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("OpenFile(socket) blocked")
		}
	})

	t.Run("device refused", func(t *testing.T) {
		dev, err := OpenDir("/dev")
		if err != nil {
			t.Fatalf("OpenDir(/dev): %v", err)
		}
		defer func() { _ = dev.Close() }()
		_, _, err = dev.OpenFile("null")
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("OpenFile(/dev/null) error = %v, want ErrUnsafeFile", err)
		}
	})

	t.Run("multiply linked file refused", func(t *testing.T) {
		path := filepath.Join(rootPath, "linked")
		mustWrite(t, path, []byte("x"), 0o600)
		if err := os.Link(path, filepath.Join(rootPath, "linked-alias")); err != nil {
			t.Fatal(err)
		}
		_, _, err := root.OpenFile("linked")
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("OpenFile(hard link) error = %v, want ErrUnsafeFile", err)
		}
	})

	t.Run("symlink leaf refused by no-follow open", func(t *testing.T) {
		mustWrite(t, filepath.Join(rootPath, "target"), []byte("x"), 0o600)
		if err := os.Symlink("target", filepath.Join(rootPath, "link")); err != nil {
			t.Fatal(err)
		}
		_, _, err := root.OpenFile("link")
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("OpenFile(symlink) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("symlinked intermediate refused", func(t *testing.T) {
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(rootPath, "dirlink")); err != nil {
			t.Fatal(err)
		}
		_, _, err := root.OpenFile("dirlink/anything")
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("OpenFile(through symlinked dir) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("escaping and absolute relatives refused", func(t *testing.T) {
		for _, rel := range []string{"../escape", "a/../../escape", "/absolute", "."} {
			if _, _, err := root.OpenFile(rel); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("OpenFile(%q) error = %v, want ErrUnsafePath", rel, err)
			}
		}
	})
}

func TestReadFileLimitBoundary(t *testing.T) {
	root, rootPath := tempRoot(t)
	const limit = 16

	t.Run("exact limit passes", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), limit)
		mustWrite(t, filepath.Join(rootPath, "exact"), data, 0o600)
		got, id, err := root.ReadFile("exact", limit)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("bytes differ at exact limit")
		}
		if id.Size != limit {
			t.Fatalf("identity size = %d, want %d", id.Size, limit)
		}
	})

	t.Run("one byte over fails as too large", func(t *testing.T) {
		mustWrite(t, filepath.Join(rootPath, "over"), bytes.Repeat([]byte("x"), limit+1), 0o600)
		_, _, err := root.ReadFile("over", limit)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("ReadFile(limit+1) error = %v, want ErrTooLarge", err)
		}
	})

	t.Run("growth after open fails as too large", func(t *testing.T) {
		path := filepath.Join(rootPath, "growing")
		mustWrite(t, path, bytes.Repeat([]byte("x"), limit), 0o600)
		file, _, err := root.OpenFile("growing")
		if err != nil {
			t.Fatalf("OpenFile: %v", err)
		}
		defer func() { _ = file.Close() }()
		mustWrite(t, path, bytes.Repeat([]byte("y"), limit+1), 0o600)
		_, _, err = ReadFile(file, limit)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("ReadFile(grown) error = %v, want ErrTooLarge", err)
		}
	})

	t.Run("non-regular file refused before reading", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(rootPath, "subdir"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, _, err := root.ReadFile("subdir", limit)
		if !errors.Is(err, ErrUnsafeFile) {
			t.Fatalf("ReadFile(directory) error = %v, want ErrUnsafeFile", err)
		}
	})
}

func TestReadFilePostReadStability(t *testing.T) {
	root, rootPath := tempRoot(t)
	const limit = 64

	open := func(name string) *os.File {
		t.Helper()
		file, _, err := root.OpenFile(name)
		if err != nil {
			t.Fatalf("OpenFile(%s): %v", name, err)
		}
		t.Cleanup(func() { _ = file.Close() })
		return file
	}

	t.Run("growth during read is too large", func(t *testing.T) {
		path := filepath.Join(rootPath, "grow-mid")
		mustWrite(t, path, bytes.Repeat([]byte("x"), limit), 0o600)
		file := open("grow-mid")
		barrier := func(_ *os.File) {
			mustWrite(t, path, bytes.Repeat([]byte("x"), limit+1), 0o600)
		}
		_, _, err := readBounded(file, limit, barrier)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("readBounded(growth) error = %v, want ErrTooLarge", err)
		}
	})

	t.Run("same-size rewrite during read is a generation conflict", func(t *testing.T) {
		path := filepath.Join(rootPath, "rewrite-mid")
		mustWrite(t, path, bytes.Repeat([]byte("x"), limit), 0o600)
		file := open("rewrite-mid")
		original := statOf(t, path).ModTime()
		barrier := func(_ *os.File) {
			mustWrite(t, path, bytes.Repeat([]byte("y"), limit), 0o600)
			if err := os.Chtimes(path, original, original); err != nil {
				t.Fatalf("chtimes: %v", err)
			}
		}
		_, _, err := readBounded(file, limit, barrier)
		if !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("readBounded(rewrite) error = %v, want ErrGenerationConflict", err)
		}
	})

	t.Run("stable read returns the captured identity", func(t *testing.T) {
		path := filepath.Join(rootPath, "stable")
		mustWrite(t, path, []byte("payload"), 0o600)
		file := open("stable")
		data, id, err := readBounded(file, limit, func(_ *os.File) {})
		if err != nil {
			t.Fatalf("readBounded(stable): %v", err)
		}
		if string(data) != "payload" {
			t.Fatalf("data = %q", data)
		}
		if !id.SameState(identityFromFileInfo(t, statOf(t, path))) {
			t.Fatal("returned identity does not match the read object")
		}
	})
}
