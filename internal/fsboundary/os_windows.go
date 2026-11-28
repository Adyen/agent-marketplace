//go:build windows

package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// The Windows counterpart of os_unix.go. It follows Go's own os.Root on
// Windows (internal/syscall/windows/at_windows.go in the Go source tree):
//
//   - A captured directory is a handle, and every open is NtCreateFile with
//     RootDirectory set to it and a single path component as the name. This is
//     openat: a path renamed or replaced after capture cannot redirect it.
//   - FILE_OPEN_REPARSE_POINT opens a reparse point (symlink, junction, mount
//     point, any tag) as itself; the handle is then inspected and refused. This
//     is O_NOFOLLOW. OBJ_DONT_REPARSE is not used: it is unavailable on some
//     supported Windows releases.
//   - Rename and delete act on handles opened relative to a captured directory,
//     with POSIX semantics so an open handle elsewhere does not block them.
//
// Links are never honored: readLink refuses, so the Unix link policies refuse.

const (
	shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

	dirAccess           = windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.FILE_READ_EA | windows.STANDARD_RIGHTS_READ | windows.SYNCHRONIZE
	fileReadAccess      = windows.FILE_READ_DATA | windows.FILE_READ_ATTRIBUTES | windows.FILE_READ_EA | windows.STANDARD_RIGHTS_READ | windows.SYNCHRONIZE
	fileReadWriteAccess = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE
	deleteAccess        = windows.FILE_READ_ATTRIBUTES | windows.DELETE | windows.SYNCHRONIZE

	// noFollow opens a reparse point as itself instead of resolving it.
	noFollow = windows.FILE_OPEN_REPARSE_POINT

	fileBasicInfoClass         = 0
	fileDispositionInformation = 13
	fileDispositionInfoEx      = 64
	fileRenameInformationEx    = 65

	fileRenameReplaceIfExists = 0x1
	fileRenamePOSIXSemantics  = 0x2
	fileRenameIgnoreReadOnly  = 0x40

	filetimeUnixEpoch = 116444736000000000 // FILETIME ticks (100ns) from 1601 to 1970
)

type fileBasicInfo struct {
	CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
	FileAttributes                                          uint32
}

type fileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

// checkComponentName applies the project's portable filename policy, which
// refuses device names, alternate data streams, a trailing dot or space, and
// characters Windows forbids.
func checkComponentName(name string) error {
	if err := nameutil.ValidateSafeFilename(name); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrUnsafePath, name, err)
	}
	return nil
}

func handleOf(file *os.File) windows.Handle { return windows.Handle(file.Fd()) }

func statusOf(err error) windows.NTStatus {
	status, _ := err.(windows.NTStatus)
	return status
}

// errnoOf maps an NT status to its Win32 error, so errors.Is works with
// fs.ErrNotExist, fs.ErrExist, and fs.ErrPermission. Other errors pass through.
func errnoOf(err error) error {
	if status, ok := err.(windows.NTStatus); ok {
		return status.Errno()
	}
	return err
}

func isNotFound(status windows.NTStatus) bool {
	return status == windows.STATUS_OBJECT_NAME_NOT_FOUND || status == windows.STATUS_OBJECT_PATH_NOT_FOUND || status == windows.STATUS_NO_SUCH_FILE
}

// ntCreate is NtCreateFile relative to parent. Synchronous I/O is always
// requested so the handle works with ordinary blocking reads and writes.
func ntCreate(parent windows.Handle, name *windows.NTUnicodeString, access, disposition, options uint32) (windows.Handle, error) {
	attributes := &windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	var handle windows.Handle
	var block windows.IO_STATUS_BLOCK
	err := windows.NtCreateFile(&handle, access, attributes, &block, nil, windows.FILE_ATTRIBUTE_NORMAL, shareAll, disposition,
		options|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	runtime.KeepAlive(name)
	runtime.KeepAlive(attributes)
	if err != nil {
		return 0, err
	}
	return handle, nil
}

// ntOpen opens the single path component name below the directory handle
// parent. A name with a separator would traverse several components.
func ntOpen(parent windows.Handle, name string, access, disposition, options uint32) (windows.Handle, error) {
	if name == "" || strings.ContainsAny(name, `\/`) {
		return 0, fmt.Errorf("%w: invalid path component %q", ErrUnsafePath, name)
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid path component %q: %w", ErrUnsafePath, name, err)
	}
	return ntCreate(parent, objectName, access, disposition, options)
}

// withHandle opens name below parent, runs fn on the handle, and closes it.
func withHandle(parent *os.File, name string, access, disposition, options uint32, fn func(windows.Handle) error) error {
	handle, err := ntOpen(handleOf(parent), name, access, disposition, options)
	if err != nil {
		return errnoOf(err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return fn(handle)
}

func fileInformation(h windows.Handle) (info windows.ByHandleFileInformation, err error) {
	err = windows.GetFileInformationByHandle(h, &info)
	return info, err
}

func isReparsePoint(h windows.Handle) (bool, error) {
	info, err := fileInformation(h)
	return info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, err
}

func duplicateFile(file *os.File, display string) (*os.File, error) {
	self := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(self, handleOf(file), self, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, fmt.Errorf("duplicate %s: %w", display, err)
	}
	return os.NewFile(uintptr(duplicate), display), nil
}

// longPath returns an extended-length form of an absolute path, so a root
// below a deep tree opens without the legacy path-length limit.
func longPath(path string) string {
	switch {
	case strings.HasPrefix(path, `\\?\`), strings.HasPrefix(path, `\\.\`):
		return path
	case strings.HasPrefix(path, `\\`):
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}

// openDirectoryPath opens the already-canonical directory path. Delete sharing
// lets the captured directory be renamed while captured, as on Unix.
func openDirectoryPath(canonical string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(longPath(canonical))
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", canonical, err)
	}
	handle, err := windows.CreateFile(name, dirAccess, shareAll, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", canonical, err)
	}
	info, err := fileInformation(handle)
	if err == nil && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		err = fmt.Errorf("%w: root is not a directory: %s", ErrUnsafePath, canonical)
	}
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	return os.NewFile(uintptr(handle), canonical), nil
}

// openFailed classifies a failed relative open into the shared refusal
// identities. what names the kind of entry for messages; only a leaf that is a
// link carries errLeafSymlink, which destination capture uses.
func openFailed(err error, path string, what string) error {
	status := statusOf(err)
	switch {
	case status == windows.STATUS_REPARSE_POINT_ENCOUNTERED && what == "leaf":
		return fmt.Errorf("%w: %s is a link: %w", ErrUnsafePath, path, errLeafSymlink)
	case status == windows.STATUS_REPARSE_POINT_ENCOUNTERED:
		return fmt.Errorf("%w: %s is a link", ErrUnsafePath, path)
	case isNotFound(status):
		return fmt.Errorf("open %s %s: %w", what, path, fs.ErrNotExist)
	case errors.Is(err, ErrUnsafePath):
		return err
	}
	return fmt.Errorf("open %s %s: %w", what, path, errnoOf(err))
}

// openComponent opens one intermediate directory below parent without
// following links. A reparse point of any kind is refused.
func openComponent(parent *os.File, name string) (*os.File, error) {
	path := filepath.Join(parent.Name(), name)
	if name == "." {
		return duplicateFile(parent, path)
	}
	handle, err := ntOpen(handleOf(parent), name, dirAccess, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|noFollow)
	if err != nil {
		if statusOf(err) == windows.STATUS_NOT_A_DIRECTORY {
			return nil, fmt.Errorf("%w: %s is not a directory", ErrUnsafePath, path)
		}
		return nil, openFailed(err, path, "path component")
	}
	reparse, err := isReparsePoint(handle)
	if err == nil && reparse {
		err = fmt.Errorf("%w: %s is a link; intermediate path components must be real directories", ErrUnsafePath, path)
	}
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	return os.NewFile(uintptr(handle), path), nil
}

// openLeaf opens the final component below parent without following links and
// validates that it is a singly linked regular file.
func openLeaf(parent *os.File, name string) (*os.File, Identity, error) {
	path := filepath.Join(parent.Name(), name)
	handle, err := ntOpen(handleOf(parent), name, fileReadAccess, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|noFollow)
	if err != nil {
		if statusOf(err) == windows.STATUS_FILE_IS_A_DIRECTORY {
			// A directory, or a directory link, sits at the leaf.
			if isReparsePointAt(parent, name) {
				return nil, Identity{}, fmt.Errorf("%w: %s: %w", ErrUnsafePath, path, errLeafSymlink)
			}
			return nil, Identity{}, fmt.Errorf("%w: leaf is not a regular file: %s", ErrUnsafeFile, path)
		}
		return nil, Identity{}, openFailed(err, path, "leaf")
	}
	info, err := fileInformation(handle)
	if err == nil {
		kind, kindErr := windows.GetFileType(handle)
		switch {
		case info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0:
			err = fmt.Errorf("%w: %s: %w", ErrUnsafePath, path, errLeafSymlink)
		case info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, kindErr != nil || kind != windows.FILE_TYPE_DISK:
			err = fmt.Errorf("%w: leaf is not a regular file: %s", ErrUnsafeFile, path)
			if kindErr != nil {
				err = fmt.Errorf("%w (%w)", err, kindErr)
			}
		case info.NumberOfLinks != 1:
			err = fmt.Errorf("%w: leaf has %d links: %s", ErrUnsafeFile, info.NumberOfLinks, path)
		}
	}
	if err != nil {
		return nil, Identity{}, errors.Join(err, windows.CloseHandle(handle))
	}
	file := os.NewFile(uintptr(handle), path)
	id, err := identityOf(file)
	if err == nil {
		err = validateLeafIdentity(id, path)
	}
	if err != nil {
		return nil, Identity{}, errors.Join(err, file.Close())
	}
	return file, id, nil
}

// isReparsePointAt reports whether name below parent is a reparse point.
func isReparsePointAt(parent *os.File, name string) (reparse bool) {
	_ = withHandle(parent, name, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_OPEN, noFollow, func(h windows.Handle) (err error) {
		reparse, err = isReparsePoint(h)
		return err
	})
	return reparse
}

// identityOf captures identity from the handle, never from a name: volume
// serial number, 128-bit file ID, link count, and change time.
func identityOf(file *os.File) (Identity, error) {
	handle := handleOf(file)
	info, err := fileInformation(handle)
	if err != nil {
		return Identity{}, err
	}
	var basic fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(handle, fileBasicInfoClass, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return Identity{}, fmt.Errorf("query basic information: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		return Identity{}, err
	}
	id := Identity{
		Device:       uint64(info.VolumeSerialNumber),
		Inode:        uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		Size:         stat.Size(),
		ModTimeNS:    stat.ModTime().UnixNano(),
		ChangeTimeNS: (basic.ChangeTime - filetimeUnixEpoch) * 100,
		Links:        uint64(info.NumberOfLinks),
		Mode:         stat.Mode(),
	}
	var extended fileIDInfo
	if windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&extended)), uint32(unsafe.Sizeof(extended))) == nil {
		id.Device = extended.VolumeSerialNumber
		id.Inode = leUint64(extended.FileID[:8])
		id.InodeHigh = leUint64(extended.FileID[8:])
	}
	return id, nil
}

func leUint64(b []byte) (v uint64) {
	for i := 7; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// FileInfoIdentity derives an identity from file metadata alone. A Windows
// os.FileInfo carries no file ID, volume serial number, change time, or link
// count, so this identity is weaker than one taken from a handle: it compares
// creation time, size, modification time, and mode, reports one link, and
// cannot detect a hard link. It is not comparable with a handle identity.
func FileInfoIdentity(info os.FileInfo) (Identity, error) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return Identity{}, fmt.Errorf("unexpected stat payload for %s", info.Name())
	}
	return Identity{
		Inode:     uint64(data.CreationTime.HighDateTime)<<32 | uint64(data.CreationTime.LowDateTime),
		Size:      info.Size(),
		ModTimeNS: info.ModTime().UnixNano(),
		Links:     1,
		Mode:      info.Mode(),
	}, nil
}

// ownedByCurrentUser is false: identities carry no owner and no link is
// honored, so no link target is ever accepted.
func ownedByCurrentUser(Identity) bool { return false }

func restoreBlocking(*os.File) error { return nil }

// syncDir cannot flush a directory handle on Windows; NTFS journals metadata.
func syncDir(*os.File) error { return nil }

// syncFile flushes a regular file. A read-only handle cannot be flushed, so the
// same object is reopened for write through the handle, never through a path.
func syncFile(file *os.File) error {
	err := file.Sync()
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	writable, openErr := ntCreate(handleOf(file), &windows.NTUnicodeString{}, windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|noFollow)
	if openErr != nil {
		return fmt.Errorf("reopen %s for sync: %w", file.Name(), errnoOf(openErr))
	}
	defer func() { _ = windows.CloseHandle(writable) }()
	return windows.FlushFileBuffers(writable)
}

func mkdirAt(parent *os.File, name string, _ os.FileMode) error {
	return withHandle(parent, name, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE|noFollow,
		func(windows.Handle) error { return nil })
}

// rmdirAt removes an empty directory and refuses a link.
func rmdirAt(parent *os.File, name string) error {
	return withHandle(parent, name, deleteAccess, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|noFollow, func(h windows.Handle) error {
		reparse, err := isReparsePoint(h)
		if err == nil && reparse {
			err = fmt.Errorf("%w: %s is a link, not a directory", ErrUnsafePath, name)
		}
		if err != nil {
			return err
		}
		return deleteHandle(h)
	})
}

// unlinkAt removes a non-directory leaf without following it; a link is
// removed as itself.
func unlinkAt(parent *os.File, name string) error {
	return withHandle(parent, name, deleteAccess, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|noFollow, deleteHandle)
}

// deleteHandle deletes the object behind h with POSIX semantics where the file
// system supports them, so an open handle elsewhere does not block removal,
// and falls back to classic delete otherwise.
func deleteHandle(h windows.Handle) error {
	var block windows.IO_STATUS_BLOCK
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.NtSetInformationFile(h, &block, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)), fileDispositionInfoEx)
	if unsupportedClass(err) {
		deleteFile := byte(1)
		err = windows.NtSetInformationFile(h, &block, &deleteFile, 1, fileDispositionInformation)
	}
	return errnoOf(err)
}

// unsupportedClass reports that the OS or file system lacks an information
// class or one of its flags.
func unsupportedClass(err error) bool {
	switch statusOf(err) {
	case windows.STATUS_INVALID_INFO_CLASS, windows.STATUS_INVALID_PARAMETER, windows.STATUS_NOT_SUPPORTED:
		return true
	}
	return false
}

// renameHeader is the fixed part of FILE_RENAME_INFORMATION_EX; the new name
// follows it in the same buffer.
type renameHeader struct {
	Flags         uint32
	RootDirectory windows.Handle
	FileNameLen   uint32
}

// renameAt renames src below srcParent to dst below dstParent, replacing an
// existing destination file. The source is opened as the entry itself and the
// destination is a name relative to the captured destination directory.
func renameAt(srcParent *os.File, src string, dstParent *os.File, dst string) error {
	name, err := windows.UTF16FromString(dst)
	if err != nil {
		return fmt.Errorf("%w: invalid path component %q: %w", ErrUnsafePath, dst, err)
	}
	name = name[:len(name)-1]
	headerSize := unsafe.Offsetof(renameHeader{}.FileNameLen) + unsafe.Sizeof(uint32(0))
	// The request must be at least sizeof(FILE_RENAME_INFORMATION): the fixed
	// part plus one WCHAR, padded to pointer alignment. A shorter length is
	// rejected (STATUS_INFO_LENGTH_MISMATCH) for one-character destinations.
	minimum := (int(headerSize) + 2 + int(unsafe.Alignof(uintptr(0))) - 1) &^ (int(unsafe.Alignof(uintptr(0))) - 1)
	size := max(int(headerSize)+2*len(name), minimum)
	// A uint64-backed buffer keeps the handle field aligned.
	backing := make([]uint64, (size+7)/8)
	header := (*renameHeader)(unsafe.Pointer(&backing[0]))
	header.RootDirectory = handleOf(dstParent)
	header.FileNameLen = uint32(2 * len(name))
	copy(unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(&backing[0]), headerSize)), len(name)), name)
	buffer := (*byte)(unsafe.Pointer(&backing[0]))

	return withHandle(srcParent, src, windows.DELETE|windows.SYNCHRONIZE, windows.FILE_OPEN, noFollow, func(h windows.Handle) error {
		var block windows.IO_STATUS_BLOCK
		header.Flags = fileRenameReplaceIfExists | fileRenamePOSIXSemantics | fileRenameIgnoreReadOnly
		err := windows.NtSetInformationFile(h, &block, buffer, uint32(size), fileRenameInformationEx)
		if unsupportedClass(err) {
			// Without the extended rename, retry with the classic class, which
			// replaces an existing file but has no POSIX semantics.
			header.Flags = 1
			err = windows.NtSetInformationFile(h, &block, buffer, uint32(size), windows.FileRenameInformation)
		}
		return errnoOf(err)
	})
}

// createExclusiveAt creates a brand-new regular file open for reading and
// writing. An existing name reports fs.ErrExist; a link is never followed.
func createExclusiveAt(parent *os.File, name string) (*os.File, error) {
	handle, err := ntOpen(handleOf(parent), name, fileReadWriteAccess, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|noFollow)
	if err != nil {
		return nil, errnoOf(err)
	}
	return os.NewFile(uintptr(handle), name), nil
}

// openPersistentAt opens name for reading and writing, creating it when
// absent. The leaf must be a singly linked regular file and is never a followed
// link. The mode is a Unix concept and is not applied.
func openPersistentAt(parent *os.File, name string, _ fs.FileMode, display string) (*os.File, error) {
	handle, err := ntOpen(handleOf(parent), name, fileReadWriteAccess, windows.FILE_OPEN_IF, windows.FILE_NON_DIRECTORY_FILE|noFollow)
	if err != nil {
		if statusOf(err) == windows.STATUS_FILE_IS_A_DIRECTORY {
			return nil, ErrUnsafeFile
		}
		return nil, errnoOf(err)
	}
	info, err := fileInformation(handle)
	if err == nil {
		switch {
		case info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0:
			err = fmt.Errorf("%w: %s is a link", ErrUnsafePath, name)
		case info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, info.NumberOfLinks != 1:
			err = ErrUnsafeFile
		}
	}
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	return os.NewFile(uintptr(handle), display), nil
}

// isSymlinkAt is false: links are never honored on Windows, so a refused
// intermediate component keeps its original refusal.
func isSymlinkAt(*os.File, string) bool { return false }

// readLink refuses: symlinks, junctions, and mount points are not honored.
func readLink(_ *os.File, name string) (string, error) {
	return "", fmt.Errorf("%w: links are not honored on Windows: %s", ErrUnsafePath, name)
}

// openFilesystemRoot is unreachable because no link is ever honored.
func openFilesystemRoot() (*os.File, error) {
	return nil, fmt.Errorf("%w: links outside the captured root are not honored on Windows", ErrUnsafePath)
}
