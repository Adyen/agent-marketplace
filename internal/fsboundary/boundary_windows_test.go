//go:build windows

package fsboundary

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Each protection below is tested with a control proving the hazard is real
// (a following implementation would have succeeded), so a refusal cannot pass
// vacuously.

func openFileErr(root *Dir, rel string) error {
	file, _, err := root.OpenFile(rel)
	if file != nil {
		_ = file.Close()
	}
	return err
}

func openPathErr(root *Dir, rel string) error {
	dir, err := root.OpenPath(rel)
	if dir != nil {
		_ = dir.Close()
	}
	return err
}

func ensurePathErr(root *Dir, rel string) error {
	dir, err := root.EnsurePath(rel)
	if dir != nil {
		_ = dir.Close()
	}
	return err
}

func persistentErr(root *Dir, name string) error {
	file, err := root.OpenPersistentFile(name, 0o600)
	if file != nil {
		_ = file.Close()
	}
	return err
}

func captureErr(root *Dir, rel string, policy LinkPolicy) error {
	dest, err := CaptureDestination(root, rel, policy)
	if dest != nil {
		_ = dest.Close()
	}
	return err
}

// wantErr reports a failure unless err matches want.
func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s = %v, want %v", what, err, want)
	}
}

func mustNoErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// mustJunction creates a directory junction (a reparse point that needs no
// privilege) through the Windows API, so no shell parses the paths. It proves
// the junction is live by listing the target's entries.
func mustJunction(t *testing.T, target, link string) {
	t.Helper()
	mustNoErr(t, "create junction directory", os.Mkdir(link, 0o700))
	name, err := windows.UTF16PtrFromString(link)
	mustNoErr(t, "encode path", err)
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, shareAll, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	mustNoErr(t, "open the junction directory", err)
	defer func() { _ = windows.CloseHandle(handle) }()

	// REPARSE_DATA_BUFFER for IO_REPARSE_TAG_MOUNT_POINT: an 8-byte header, four
	// uint16 name offsets and lengths, then the NT substitute name and the print
	// name, each NUL-terminated.
	substitute := windows.StringToUTF16(`\??\` + target)
	print := windows.StringToUTF16(target)
	pathBytes := 2 * (len(substitute) + len(print))
	buffer := make([]byte, 16+pathBytes)
	put16 := func(offset int, v uint16) { buffer[offset], buffer[offset+1] = byte(v), byte(v>>8) }
	for i, b := range []byte{0x03, 0x00, 0x00, 0xA0} { // IO_REPARSE_TAG_MOUNT_POINT, little endian
		buffer[i] = b
	}
	put16(4, uint16(8+pathBytes)) // ReparseDataLength
	put16(8, 0)                   // SubstituteNameOffset
	put16(10, uint16(2*(len(substitute)-1)))
	put16(12, uint16(2*len(substitute))) // PrintNameOffset
	put16(14, uint16(2*(len(print)-1)))
	for i, u := range append(substitute, print...) {
		put16(16+2*i, u)
	}
	var returned uint32
	const fsctlSetReparsePoint = 0x000900A4
	mustNoErr(t, "set the mount point", windows.DeviceIoControl(handle, fsctlSetReparsePoint, &buffer[0], uint32(len(buffer)), nil, 0, &returned, nil))

	got, err := os.ReadDir(link)
	want, _ := os.ReadDir(target)
	if err != nil || len(got) != len(want) {
		t.Fatalf("junction %s does not show the target's entries: %v", link, err)
	}
}

// mustUnhandledReparsePoint turns the empty file or directory at path into a
// reparse point whose tag no driver claims. It is not a name-surrogate link and
// needs no privilege; opening it without FILE_OPEN_REPARSE_POINT fails, so an
// implementation that does not open reparse points as themselves cannot pass
// by accident. It stands for every reparse point other than a symlink or
// junction (cloud placeholders, deduplicated files, and similar).
func mustUnhandledReparsePoint(t *testing.T, path string, directory bool) {
	t.Helper()
	if directory {
		mustMkdir(t, path)
	} else {
		mustWrite(t, path, nil, 0o600)
	}
	name, err := windows.UTF16PtrFromString(path)
	mustNoErr(t, "encode path", err)
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, shareAll, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	mustNoErr(t, "open to set a reparse point", err)
	defer func() { _ = windows.CloseHandle(handle) }()

	// REPARSE_DATA_BUFFER: tag, data length, reserved, payload. The tag is in
	// the Microsoft range but unassigned; it is used only on temporary files.
	buffer := struct {
		Tag        uint32
		DataLength uint16
		Reserved   uint16
		Payload    [4]byte
	}{Tag: 0x8000F0F0, DataLength: 4, Payload: [4]byte{'t', 'e', 's', 't'}}
	var returned uint32
	const fsctlSetReparsePoint = 0x000900A4
	if err := windows.DeviceIoControl(handle, fsctlSetReparsePoint, (*byte)(unsafe.Pointer(&buffer)), uint32(unsafe.Sizeof(buffer)), nil, 0, &returned, nil); err != nil {
		t.Skipf("host cannot set a test reparse point: %v", err)
	}
	if _, err := os.ReadFile(path); err == nil && !directory {
		t.Fatalf("%s is not a reparse point: an ordinary read succeeded", path)
	}
}

// settle waits for the file system clock to advance so a rewrite is visible
// through modification and change times. Like the Unix contract, the boundary
// is not a compare-and-swap against a writer finishing inside one clock tick.
func settle() { time.Sleep(40 * time.Millisecond) }

func TestWindowsRefusesLinks(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "real"))
	mustWrite(t, filepath.Join(path, "real", "file.txt"), []byte("outside via link"), 0o600)
	mustJunction(t, filepath.Join(path, "real"), filepath.Join(path, "junction"))

	// Controls: the real path opens, and the junction really reaches the bytes.
	mustNoErr(t, "control open", openFileErr(root, `real\file.txt`))
	if data, err := os.ReadFile(filepath.Join(path, "junction", "file.txt")); err != nil || string(data) != "outside via link" {
		t.Fatalf("junction control: %q %v", data, err)
	}

	for _, c := range []struct {
		what string
		err  error
	}{
		{"OpenFile through a junction", openFileErr(root, `junction\file.txt`)},
		{"OpenFile of a junction leaf", openFileErr(root, "junction")},
		{"OpenPath of a junction", openPathErr(root, "junction")},
		{"EnsurePath through a junction", ensurePathErr(root, `junction\created`)},
		{"capture through a junction", captureErr(root, `junction\file.txt`, LinkPolicyRepository)},
		{"capture of a junction (user policy)", captureErr(root, "junction", LinkPolicyUser)},
		{"Rmdir of a junction", root.Rmdir("junction")},
	} {
		wantErr(t, c.what, c.err, ErrUnsafePath)
	}
	if _, err := os.Lstat(filepath.Join(path, "real", "created")); err == nil {
		t.Error("EnsurePath created a directory through the junction")
	}
	if _, err := os.Lstat(filepath.Join(path, "junction")); err != nil {
		t.Errorf("Rmdir removed the junction: %v", err)
	}
}

// Reparse points that are not symlinks or junctions (cloud placeholders,
// deduplicated files, and similar) are refused too. This is separate from the
// junction test so an unavailable synthetic fixture cannot hide those checks.
func TestWindowsRefusesOtherReparsePoints(t *testing.T) {
	root, path := tempRoot(t)
	mustUnhandledReparsePoint(t, filepath.Join(path, "stub.txt"), false)
	mustUnhandledReparsePoint(t, filepath.Join(path, "stubdir"), true)
	for _, c := range []struct {
		what string
		err  error
	}{
		{"OpenFile of a reparse-point file", openFileErr(root, "stub.txt")},
		{"OpenPersistentFile of a reparse-point file", persistentErr(root, "stub.txt")},
		{"OpenPath of a reparse-point directory", openPathErr(root, "stubdir")},
		{"capture of a reparse-point file", captureErr(root, "stub.txt", LinkPolicyRepository)},
		{"Rmdir of a reparse-point directory", root.Rmdir("stubdir")},
	} {
		wantErr(t, c.what, c.err, ErrUnsafePath)
	}
	if _, err := os.Lstat(filepath.Join(path, "stubdir")); err != nil {
		t.Errorf("Rmdir removed the reparse-point directory: %v", err)
	}
}

// Paths with characters a shell would interpret must work: no step may pass them
// through cmd.exe.
func TestWindowsJunctionsUnderShellSpecialPaths(t *testing.T) {
	base := filepath.Join(t.TempDir(), "a&b (c) ^d")
	mustMkdir(t, filepath.Join(base, "real"))
	mustJunction(t, filepath.Join(base, "real"), filepath.Join(base, "junction"))
	root, err := OpenDir(base)
	mustNoErr(t, "OpenDir", err)
	defer func() { _ = root.Close() }()
	wantErr(t, "OpenPath of a junction below a shell-special path", openPathErr(root, "junction"), ErrUnsafePath)
	mustNoErr(t, "control: the real directory", openPathErr(root, "real"))
}

// File symlinks need Developer Mode or administrator rights to create; this runs
// where the host allows it (including CI). TestWindowsRefusesLinks covers the
// same open path without privilege.
func TestWindowsRefusesFileSymlinkLeaf(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "target.txt"), []byte("outside via link"), 0o600)
	mustSymlink(t, filepath.Join(path, "target.txt"), filepath.Join(path, "link.txt"))
	if data, err := os.ReadFile(filepath.Join(path, "link.txt")); err != nil || string(data) != "outside via link" {
		t.Fatalf("symlink control: %q %v", data, err)
	}
	wantErr(t, "OpenFile of a symlink", openFileErr(root, "link.txt"), ErrUnsafePath)
	wantErr(t, "capture of a symlink (user policy)", captureErr(root, "link.txt", LinkPolicyUser), ErrUnsafePath)
	if got := mustRead(t, filepath.Join(path, "target.txt")); got != "outside via link" {
		t.Fatalf("the symlink target changed: %q", got)
	}
}

func TestWindowsRefusesHardLinkedLeaf(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "single.txt"), []byte("one name"), 0o600)
	mustWrite(t, filepath.Join(path, "linked.txt"), []byte("two names"), 0o600)
	mustNoErr(t, "create hard link", os.Link(filepath.Join(path, "linked.txt"), filepath.Join(path, "alias.txt")))

	file, id, err := root.OpenFile("single.txt")
	mustNoErr(t, "control open of a singly linked file", err)
	_ = file.Close()
	if id.Links != 1 {
		t.Fatalf("control link count = %d, want 1", id.Links)
	}
	for _, name := range []string{"linked.txt", "alias.txt"} {
		wantErr(t, "OpenFile("+name+")", openFileErr(root, name), ErrUnsafeFile)
	}
	wantErr(t, "OpenPersistentFile of a hard-linked file", persistentErr(root, "linked.txt"), ErrUnsafeFile)
	if got := mustRead(t, filepath.Join(path, "alias.txt")); got != "two names" {
		t.Fatalf("hard-linked bytes changed: %q", got)
	}
}

func TestWindowsRefusesSpecialNames(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "ordinary.txt"), []byte("ok"), 0o600)
	mustNoErr(t, "control: ordinary name", openFileErr(root, "ordinary.txt"))
	for _, name := range []string{"nul", "NUL.txt", "con", "aux.md", "com1", "lpt9.log", "stream:name", "trailing.", "trailing ", `a"b`, "a*b", "a?b"} {
		wantErr(t, "OpenFile("+name+")", openFileErr(root, name), ErrUnsafePath)
		wantErr(t, "Mkdir("+name+")", root.Mkdir(name, 0o755), ErrUnsafePath)
		wantErr(t, "OpenPersistentFile("+name+")", persistentErr(root, name), ErrUnsafePath)
		wantErr(t, "Unlink("+name+")", root.Unlink(name), ErrUnsafePath)
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) != 1 {
		t.Fatalf("a refused name left residue: %v %v", entries, err)
	}
}

// A captured directory stays the operation's target after its path is renamed
// and a decoy takes the old path. This needs the captured handle to share delete
// access, and is what makes the boundary handle-bound.
func TestWindowsOperationsStayBoundToTheCapturedDirectory(t *testing.T) {
	parent := t.TempDir()
	original, moved := filepath.Join(parent, "captured"), filepath.Join(parent, "moved")
	mustMkdir(t, original)
	root, err := OpenDir(original)
	mustNoErr(t, "OpenDir", err)
	defer func() { _ = root.Close() }()
	mustNoErr(t, "rename the captured directory (its handle must allow it)", os.Rename(original, moved))
	mustMkdir(t, original) // decoy replacement tree

	file, name, err := root.CreateTemp("staged-*")
	mustNoErr(t, "CreateTemp", err)
	_ = file.Close()
	mustNoErr(t, "Mkdir", root.Mkdir("child", 0o755))
	persistent, err := root.OpenPersistentFile("state.json", 0o600)
	mustNoErr(t, "OpenPersistentFile", err)
	_ = persistent.Close()

	for _, entry := range []string{name, "child", "state.json"} {
		if _, err := os.Stat(filepath.Join(moved, entry)); err != nil {
			t.Errorf("%s is not in the captured (moved) directory: %v", entry, err)
		}
	}
	if decoy, err := os.ReadDir(original); err != nil || len(decoy) != 0 {
		t.Fatalf("operations leaked into the replacement tree: %v %v", decoy, err)
	}
	mustNoErr(t, "Sync of a captured directory", root.Sync())
}

func TestWindowsMkdirUnlinkRmdirNeverFollowLinks(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "real"))
	mustWrite(t, filepath.Join(path, "real", "keep.txt"), []byte("keep"), 0o600)
	mustJunction(t, filepath.Join(path, "real"), filepath.Join(path, "junction"))
	mustWrite(t, filepath.Join(path, "plain.txt"), []byte("remove me"), 0o600)
	mustMkdir(t, filepath.Join(path, "empty"))
	mustWrite(t, filepath.Join(path, "real", "sub.txt"), []byte("x"), 0o600)

	// Controls: ordinary Mkdir, Unlink and Rmdir work and report the usual errors.
	mustNoErr(t, "Mkdir", root.Mkdir("fresh", 0o755))
	wantErr(t, "Mkdir of an existing directory", root.Mkdir("fresh", 0o755), fs.ErrExist)
	mustNoErr(t, "Unlink", root.Unlink("plain.txt"))
	wantErr(t, "Unlink of a missing name", root.Unlink("plain.txt"), fs.ErrNotExist)
	mustNoErr(t, "Rmdir of an empty directory", root.Rmdir("empty"))
	if err := root.Rmdir("real"); err == nil {
		t.Fatal("Rmdir removed a non-empty directory")
	}

	wantErr(t, "Mkdir over a junction", root.Mkdir("junction", 0o755), fs.ErrExist)
	if err := root.Unlink("junction"); err == nil {
		t.Error("Unlink removed a junction as if it were a file")
	}
	if _, err := os.Lstat(filepath.Join(path, "junction")); err != nil {
		t.Errorf("the junction was removed: %v", err)
	}
	if got := mustRead(t, filepath.Join(path, "real", "keep.txt")); got != "keep" {
		t.Errorf("the junction target lost content: %q", got)
	}
}

// stage creates a staged file below staging and returns its name.
func stage(t *testing.T, staging *Dir, content string) string {
	t.Helper()
	file, name, err := staging.CreateTemp("publish-*")
	mustNoErr(t, "CreateTemp", err)
	_, err = file.WriteString(content)
	mustNoErr(t, "write staged file", err)
	mustNoErr(t, "close staged file", file.Close())
	return name
}

// stagingDir captures a staging directory below the root.
func stagingDir(t *testing.T, root *Dir, rootPath string) *Dir {
	t.Helper()
	mustMkdir(t, filepath.Join(rootPath, "staging"))
	staging, err := root.OpenPath("staging")
	mustNoErr(t, "OpenPath(staging)", err)
	t.Cleanup(func() { _ = staging.Close() })
	return staging
}

func capture(t *testing.T, root *Dir, rel string) *Destination {
	t.Helper()
	dest, err := CaptureDestination(root, rel, LinkPolicyRepository)
	mustNoErr(t, "CaptureDestination("+rel+")", err)
	t.Cleanup(func() { _ = dest.Close() })
	return dest
}

func TestWindowsPublishReplacesAndCreatesDestinations(t *testing.T) {
	root, path := tempRoot(t)
	staging := stagingDir(t, root, path)
	config := filepath.Join(path, "config.yaml")

	dest := capture(t, root, "config.yaml")
	if dest.Exists() {
		t.Fatal("destination should be absent")
	}
	first := stage(t, staging, "first")
	mustNoErr(t, "publish to an absent destination", dest.Publish(staging, first))
	if got := mustRead(t, config); got != "first" {
		t.Fatalf("published bytes = %q", got)
	}
	if _, err := os.Stat(filepath.Join(path, "staging", first)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the staged file survived a successful publish: %v", err)
	}

	// Replace the destination while a boundary handle holds it open. Boundary
	// handles share delete access and rename has POSIX semantics, so this works.
	reader, _, err := root.OpenFile("config.yaml")
	mustNoErr(t, "open the destination", err)
	defer func() { _ = reader.Close() }()
	settle()
	dest = capture(t, root, "config.yaml")
	mustNoErr(t, "publish over a destination held open by a boundary handle", dest.Publish(staging, stage(t, staging, "second")))
	if got := mustRead(t, config); got != "second" {
		t.Fatalf("replaced bytes = %q", got)
	}
}

// Another program that holds the destination without delete sharing (many
// editors and scanners do) makes replacement impossible on Windows. Publication
// must fail closed and succeed once that program lets go.
func TestWindowsPublishFailsClosedWhenAnotherProgramHoldsTheDestination(t *testing.T) {
	root, path := tempRoot(t)
	config := filepath.Join(path, "config.yaml")
	mustWrite(t, config, []byte("original"), 0o600)
	staging := stagingDir(t, root, path)
	name := stage(t, staging, "published")
	dest := capture(t, root, "config.yaml")

	holder, err := os.Open(config) // no FILE_SHARE_DELETE
	mustNoErr(t, "hold the destination", err)
	if err := dest.Publish(staging, name); err == nil {
		t.Fatal("publish succeeded although another program holds the destination")
	}
	_ = holder.Close()
	if got := mustRead(t, config); got != "original" {
		t.Fatalf("a failed publish changed the destination: %q", got)
	}
	if _, err := os.Stat(filepath.Join(path, "staging", name)); err != nil {
		t.Fatalf("a failed publish removed the staged file: %v", err)
	}
	mustNoErr(t, "publish after the holder closed", dest.Publish(staging, name))
	if got := mustRead(t, config); got != "published" {
		t.Fatalf("published bytes = %q", got)
	}
}

// A one-character destination makes the rename request shorter than
// sizeof(FILE_RENAME_INFORMATION) unless the length is padded.
func TestWindowsPublishToShortNames(t *testing.T) {
	root, path := tempRoot(t)
	staging := stagingDir(t, root, path)
	for _, name := range []string{"a", "ab", "x.y"} {
		mustNoErr(t, "publish to "+name, capture(t, root, name).Publish(staging, stage(t, staging, "bytes for "+name)))
		if got := mustRead(t, filepath.Join(path, name)); got != "bytes for "+name {
			t.Errorf("%s = %q", name, got)
		}
	}
}

// A staging pattern must not smuggle in syntax Windows treats specially: with
// "file:stream" the staged name would create a stream on an existing file
// instead of a new directory entry.
func TestWindowsCreateTempRefusesSpecialPatterns(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "existing.txt"), []byte("user data"), 0o600)
	for _, pattern := range []string{"existing.txt:stage-*", "stage-*:stream", "nul.*", "stage-*."} {
		file, name, err := root.CreateTemp(pattern)
		if file != nil {
			_ = file.Close()
		}
		wantErr(t, "CreateTemp("+pattern+")", err, ErrUnsafePath)
		if name != "" {
			t.Errorf("CreateTemp(%q) returned the name %q", pattern, name)
		}
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) != 1 {
		t.Fatalf("a refused pattern left residue: %v %v", entries, err)
	}
	if got := mustRead(t, filepath.Join(path, "existing.txt")); got != "user data" {
		t.Fatalf("existing.txt changed: %q", got)
	}
	file, _, err := root.CreateTemp("publish-*") // control
	mustNoErr(t, "control CreateTemp", err)
	_ = file.Close()
}

func TestWindowsPublishRefusesChangedDestinations(t *testing.T) {
	for _, c := range []struct {
		name   string
		exists bool
		change func(t *testing.T, path string)
		want   string // destination bytes after the refusal
	}{
		{"replaced after capture", true, func(t *testing.T, p string) {
			settle()
			mustNoErr(t, "remove", os.Remove(filepath.Join(p, "config.yaml")))
			mustWrite(t, filepath.Join(p, "config.yaml"), []byte("concurrent edit"), 0o600)
		}, "concurrent edit"},
		{"edited in place after capture", true, func(t *testing.T, p string) {
			settle()
			mustWrite(t, filepath.Join(p, "config.yaml"), []byte("edited!!"), 0o600)
		}, "edited!!"},
		{"appeared after capture", false, func(t *testing.T, p string) {
			mustWrite(t, filepath.Join(p, "config.yaml"), []byte("appeared"), 0o600)
		}, "appeared"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, path := tempRoot(t)
			if c.exists {
				mustWrite(t, filepath.Join(path, "config.yaml"), []byte("original"), 0o600)
			}
			staging := stagingDir(t, root, path)
			name := stage(t, staging, "published")
			dest := capture(t, root, "config.yaml")
			c.change(t, path)
			wantErr(t, "publish", dest.Publish(staging, name), ErrGenerationConflict)
			if got := mustRead(t, filepath.Join(path, "config.yaml")); got != c.want {
				t.Errorf("the concurrent change was overwritten: %q", got)
			}
			if _, err := os.Stat(filepath.Join(path, "staging", name)); err != nil {
				t.Errorf("the refusal removed the caller's staged file: %v", err)
			}
		})
	}

	t.Run("ancestor renamed and replaced after capture", func(t *testing.T) {
		root, path := tempRoot(t)
		mustMkdir(t, filepath.Join(path, "sub"))
		staging := stagingDir(t, root, path)
		name := stage(t, staging, "published")
		dest := capture(t, root, `sub\config.yaml`)
		mustNoErr(t, "rename ancestor", os.Rename(filepath.Join(path, "sub"), filepath.Join(path, "moved")))
		mustMkdir(t, filepath.Join(path, "sub")) // decoy
		wantErr(t, "publish below a replaced ancestor", dest.Publish(staging, name), ErrGenerationConflict)
		for _, dir := range []string{"sub", "moved"} {
			if entries, err := os.ReadDir(filepath.Join(path, dir)); err != nil || len(entries) != 0 {
				t.Errorf("publish wrote into %s: %v %v", dir, entries, err)
			}
		}
	})
}

func TestWindowsRemoveLeafVerifiesTheCapturedObject(t *testing.T) {
	root, path := tempRoot(t)
	mustWrite(t, filepath.Join(path, "owned.txt"), []byte("owned"), 0o600)
	mustNoErr(t, "remove a captured leaf", capture(t, root, "owned.txt").RemoveLeaf())
	if _, err := os.Stat(filepath.Join(path, "owned.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the leaf survived RemoveLeaf: %v", err)
	}

	swapped := filepath.Join(path, "swapped.txt")
	mustWrite(t, swapped, []byte("captured"), 0o600)
	dest := capture(t, root, "swapped.txt")
	settle()
	mustNoErr(t, "remove", os.Remove(swapped))
	mustWrite(t, swapped, []byte("someone else's file"), 0o600)
	wantErr(t, "RemoveLeaf of a replaced leaf", dest.RemoveLeaf(), ErrGenerationConflict)
	if got := mustRead(t, swapped); got != "someone else's file" {
		t.Fatalf("the replacement was removed: %q", got)
	}
}

func TestWindowsBoundedReadDetectsChange(t *testing.T) {
	root, path := tempRoot(t)
	data := filepath.Join(path, "data.txt")
	mustWrite(t, data, []byte("0123456789"), 0o600)

	got, id, err := root.ReadFile("data.txt", 64)
	if err != nil || string(got) != "0123456789" || id.Size != 10 {
		t.Fatalf("control read: %q %+v %v", got, id, err)
	}
	_, _, err = root.ReadFile("data.txt", 4)
	wantErr(t, "read over the limit", err, ErrTooLarge)

	file, _, err := root.OpenFile("data.txt")
	mustNoErr(t, "OpenFile", err)
	defer func() { _ = file.Close() }()
	_, _, err = readBounded(file, 64, func(*os.File) {
		settle()
		mustWrite(t, data, []byte("ABCDEFGHIJ"), 0o600) // same size
	})
	wantErr(t, "same-size rewrite during read", err, ErrGenerationConflict)
}

func TestWindowsIdentityComesFromTheHandle(t *testing.T) {
	root, path := tempRoot(t)
	mustMkdir(t, filepath.Join(path, "a"))
	mustMkdir(t, filepath.Join(path, "b"))
	mustWrite(t, filepath.Join(path, "a", "f.txt"), []byte("x"), 0o600)

	identity := func(rel string) Identity {
		dir, err := root.OpenPath(rel)
		mustNoErr(t, "OpenPath("+rel+")", err)
		defer func() { _ = dir.Close() }()
		return dir.Identity()
	}
	first, again, other := identity("a"), identity("a"), identity("b")
	if !first.SameObject(again) || first.SameObject(other) {
		t.Fatal("directories must be the same object only when they are")
	}
	if first.Device == 0 || first.Inode == 0 {
		t.Fatalf("handle identity carries no volume or file ID: %+v", first)
	}

	file, id, err := root.OpenFile(`a\f.txt`)
	mustNoErr(t, "OpenFile", err)
	defer func() { _ = file.Close() }()
	if id.Links != 1 || !id.Mode.IsRegular() || id.Size != 1 {
		t.Fatalf("file identity: %+v", id)
	}
	settle()
	mustWrite(t, filepath.Join(path, "a", "f.txt"), []byte("yy"), 0o600)
	after, err := FileIdentity(file)
	mustNoErr(t, "FileIdentity", err)
	if !after.SameObject(id) || after.SameState(id) {
		t.Fatalf("an in-place edit must keep the object and change the state: %+v -> %+v", id, after)
	}
}

func TestWindowsFileInfoIdentityIsWeaker(t *testing.T) {
	_, path := tempRoot(t)
	file := filepath.Join(path, "f.txt")
	mustWrite(t, file, []byte("x"), 0o600)
	identity := func() Identity {
		info := statOf(t, file)
		id, err := FileInfoIdentity(info)
		mustNoErr(t, "FileInfoIdentity", err)
		return id
	}
	before := identity()
	if before.Inode == 0 || before.Size != 1 || !before.Mode.IsRegular() || before.Links != 1 {
		t.Fatalf("FileInfoIdentity: %+v", before)
	}
	settle()
	mustWrite(t, file, []byte("yy"), 0o600)
	if identity().SameState(before) {
		t.Fatal("size and mtime changes must change a FileInfo identity")
	}
}

func TestWindowsNativeRootFollowsTheCapturedDirectory(t *testing.T) {
	parent := t.TempDir()
	original, moved := filepath.Join(parent, "captured"), filepath.Join(parent, "moved")
	mustMkdir(t, original)
	mustWrite(t, filepath.Join(original, "inside.txt"), []byte("captured bytes"), 0o600)
	root, err := OpenDir(original)
	mustNoErr(t, "OpenDir", err)
	defer func() { _ = root.Close() }()

	read := func() string {
		native, err := root.NativeRoot()
		mustNoErr(t, "NativeRoot", err)
		defer func() { _ = native.Close() }()
		data, err := native.ReadFile("inside.txt")
		mustNoErr(t, "read through the native root", err)
		return string(data)
	}
	if got := read(); got != "captured bytes" {
		t.Fatalf("native root read %q", got)
	}
	// After the captured directory moves and a decoy takes its path, the native
	// root is still the captured directory.
	mustNoErr(t, "rename", os.Rename(original, moved))
	mustMkdir(t, original)
	mustWrite(t, filepath.Join(original, "inside.txt"), []byte("decoy bytes"), 0o600)
	if got := read(); got != "captured bytes" {
		t.Fatalf("native root after a swap read %q, want the captured directory's bytes", got)
	}
}

func TestWindowsRootsUnderLongPaths(t *testing.T) {
	deep := t.TempDir()
	for len(deep) < 330 {
		deep = filepath.Join(deep, strings.Repeat("d", 60))
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Skipf("host cannot create paths over the legacy limit: %v", err)
	}
	root, err := OpenDir(deep)
	mustNoErr(t, "OpenDir under a long path", err)
	defer func() { _ = root.Close() }()
	file, name, err := root.CreateTemp("long-*")
	mustNoErr(t, "CreateTemp", err)
	_, err = file.WriteString("deep")
	mustNoErr(t, "write", err)
	_ = file.Close()
	if data, _, err := root.ReadFile(name, 16); err != nil || string(data) != "deep" {
		t.Fatalf("read under a long path: %q %v", data, err)
	}
}
