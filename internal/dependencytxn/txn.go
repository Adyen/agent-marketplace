package dependencytxn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	ErrGenerationConflict = errors.New("dependency transaction: generation conflict")
	ErrUnsafePath         = errors.New("dependency transaction: unsafe path")
	ErrRecoveryBlocked    = errors.New("dependency transaction: recovery blocked")
	ErrLockTimeout        = errors.New("dependency transaction: lock timeout")
)

type Generation struct {
	Exists bool
	Digest string
}

type DesiredFile struct {
	Path     string
	Expected Generation
	// ExpectedMode is an optional compare-and-swap precondition for the
	// destination's permission bits. Zero preserves compatibility with callers
	// that predate mode-aware transactions; transaction participants that read
	// an existing destination should provide its mode.
	ExpectedMode fs.FileMode
	Bytes        []byte
	Mode         fs.FileMode
}

type Result struct {
	Changed []string
}

type FaultStage string

const (
	FaultAfterLocks    FaultStage = "after-locks"
	FaultAfterCAS      FaultStage = "after-cas"
	FaultAfterStage    FaultStage = "after-stage"
	FaultAfterJournal  FaultStage = "after-journal"
	FaultBeforePublish FaultStage = "before-publish"
	FaultAfterPublish  FaultStage = "after-publish"
	FaultAfterFinalize FaultStage = "after-finalize"
)

type FaultPoint struct {
	Stage FaultStage
	Index int
	Path  string
}

type FaultHook func(FaultPoint) error

type Option func(*options)

type options struct {
	fault   FaultHook
	syncDir func(string) error
}

func WithFaultHook(hook FaultHook) Option {
	return func(o *options) { o.fault = hook }
}

// WithFsyncDir replaces directory syncing for tests that need to exercise a
// failure after an on-disk rename. The default remains fsyncDir, so callers
// cannot weaken the transaction's durability unless they explicitly opt into
// this test seam.
func WithFsyncDir(syncDir func(string) error) Option {
	return func(o *options) {
		if syncDir != nil {
			o.syncDir = syncDir
		}
	}
}

type journal struct {
	Version    int                `json:"version"`
	ID         string             `json:"id"`
	Files      []journalFile      `json:"files"`
	Assertions []journalAssertion `json:"assertions,omitempty"`
}

type journalAssertion struct {
	Path     string      `json:"path"`
	Expected Generation  `json:"expected"`
	Mode     fs.FileMode `json:"mode"`
}

type journalFile struct {
	Path      string      `json:"path"`
	Temp      string      `json:"temp"`
	Old       Generation  `json:"old"`
	OldMode   fs.FileMode `json:"old_mode"`
	NewDigest string      `json:"new_digest"`
	NewMode   fs.FileMode `json:"new_mode"`
}

type preparedFile struct {
	desired DesiredFile
	oldMode fs.FileMode
	temp    string
}

const (
	journalVersion              = 1
	maxDependencyFileSize int64 = 4 << 20
	maxJournalSize              = maxDependencyFileSize
)

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ReadGeneration(path string) (Generation, error) {
	raw, _, err := readRegularFile(path, maxDependencyFileSize)
	if errors.Is(err, os.ErrNotExist) {
		return Generation{}, nil
	}
	if err != nil {
		return Generation{}, fmt.Errorf("dependency transaction: read generation %s: %w", path, err)
	}
	return Generation{Exists: true, Digest: Digest(raw)}, nil
}

// ReadGenerationAndMode reads the destination's content generation and
// permission bits from the same inspected file state. It is intended for
// callers that pass both pieces as a transaction precondition.
func ReadGenerationAndMode(path string) (Generation, fs.FileMode, error) {
	return inspectDestination(path)
}

func readRegularFile(path string, maxSize int64) ([]byte, fs.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, ErrUnsafePath
	}
	if maxSize > 0 && info.Size() > maxSize {
		return nil, nil, fmt.Errorf("%w: file exceeds %d bytes", ErrRecoveryBlocked, maxSize)
	}
	reader := io.Reader(file)
	if maxSize > 0 {
		reader = io.LimitReader(file, maxSize+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, err
	}
	if maxSize > 0 && int64(len(raw)) > maxSize {
		return nil, nil, fmt.Errorf("%w: file exceeds %d bytes", ErrRecoveryBlocked, maxSize)
	}
	return raw, info, nil
}

func Commit(ctx context.Context, stateDir string, desired []DesiredFile, opts ...Option) (Result, error) {
	cfg := applyOptions(opts)
	files, err := normalizeDesired(desired)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, nil
	}
	stateDir, err = prepareStateDir(stateDir)
	if err != nil {
		return Result{}, err
	}
	paths := desiredPaths(files)
	locks, err := acquireLocks(ctx, paths)
	if err != nil {
		return Result{}, err
	}
	defer releaseLocks(locks)
	if err := inject(cfg, FaultPoint{Stage: FaultAfterLocks, Index: -1}); err != nil {
		return Result{}, err
	}

	changed := make([]preparedFile, 0, len(files))
	assertions := make([]journalAssertion, 0, len(files))
	for _, file := range files {
		current, mode, err := inspectDestination(file.Path)
		if err != nil {
			return Result{}, err
		}
		if current != file.Expected || file.ExpectedMode != 0 && mode != file.ExpectedMode {
			return Result{}, fmt.Errorf("%w: %s", ErrGenerationConflict, file.Path)
		}
		if current.Exists && current.Digest == Digest(file.Bytes) && mode == file.Mode {
			assertions = append(assertions, journalAssertion{Path: file.Path, Expected: current, Mode: mode})
			continue
		}
		changed = append(changed, preparedFile{desired: file, oldMode: mode})
	}
	if err := inject(cfg, FaultPoint{Stage: FaultAfterCAS, Index: -1}); err != nil {
		return Result{}, err
	}
	if len(changed) == 0 {
		return Result{}, nil
	}

	journalPublished := false
	defer func() {
		if !journalPublished {
			for _, file := range changed {
				if file.temp != "" {
					_ = os.Remove(file.temp)
				}
			}
		}
	}()
	stagedDirs := make(map[string]struct{}, len(changed))
	for i := range changed {
		temp, err := stageFile(changed[i].desired.Path, changed[i].desired.Bytes, changed[i].desired.Mode)
		if err != nil {
			return Result{}, err
		}
		changed[i].temp = temp
		stagedDirs[filepath.Dir(temp)] = struct{}{}
		if err := inject(cfg, FaultPoint{Stage: FaultAfterStage, Index: i, Path: changed[i].desired.Path}); err != nil {
			return Result{}, err
		}
	}
	for _, dir := range sortedKeys(stagedDirs) {
		if err := cfg.syncDir(dir); err != nil {
			return Result{}, err
		}
	}

	id, err := randomID()
	if err != nil {
		return Result{}, err
	}
	j := journal{Version: journalVersion, ID: id, Files: make([]journalFile, len(changed)), Assertions: assertions}
	for i, file := range changed {
		j.Files[i] = journalFile{Path: file.desired.Path, Temp: file.temp, Old: file.desired.Expected, OldMode: file.oldMode, NewDigest: Digest(file.desired.Bytes), NewMode: file.desired.Mode}
	}
	journalPath, err := persistJournal(stateDir, &j, cfg.syncDir)
	if journalPath != "" {
		// Rename publishes recovery evidence even when the following directory
		// fsync fails. Never remove staged files after that point.
		journalPublished = true
	}
	if err != nil {
		return Result{}, err
	}
	if err := inject(cfg, FaultPoint{Stage: FaultAfterJournal, Index: -1}); err != nil {
		return Result{}, err
	}
	for i, file := range j.Files {
		if err := inject(cfg, FaultPoint{Stage: FaultBeforePublish, Index: i, Path: file.Path}); err != nil {
			return Result{}, err
		}
		if err := validatePublishPreconditions(j.Files, j.Assertions, i); err != nil {
			return Result{}, err
		}
		if err := publish(file, cfg.syncDir); err != nil {
			return Result{}, err
		}
		if err := inject(cfg, FaultPoint{Stage: FaultAfterPublish, Index: i, Path: file.Path}); err != nil {
			return Result{}, err
		}
	}
	if err := finalizeJournal(journalPath, stateDir, cfg.syncDir); err != nil {
		return Result{}, err
	}
	if err := inject(cfg, FaultPoint{Stage: FaultAfterFinalize, Index: -1}); err != nil {
		return Result{}, err
	}
	result := Result{Changed: make([]string, len(changed))}
	for i := range changed {
		result.Changed[i] = changed[i].desired.Path
	}
	slog.Info("dependency files committed", "files", len(result.Changed))
	return result, nil
}

func Recover(ctx context.Context, stateDir string, opts ...Option) error {
	cfg := applyOptions(opts)
	var err error
	stateDir, err = prepareStateDir(stateDir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("dependency transaction: list journals: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".dependencytxn-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := recoverJournal(ctx, filepath.Join(stateDir, entry.Name()), stateDir, cfg); err != nil {
			return err
		}
	}
	return nil
}

func recoverJournal(ctx context.Context, path, stateDir string, cfg options) error {
	raw, _, err := readRegularFile(path, maxJournalSize)
	if err != nil {
		return fmt.Errorf("%w: read journal %s: %v", ErrRecoveryBlocked, path, err)
	}
	var j journal
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&j); err != nil {
		return fmt.Errorf("%w: decode journal %s: %v", ErrRecoveryBlocked, path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data in journal %s", ErrRecoveryBlocked, path)
	}
	if err := validateJournal(&j); err != nil {
		return err
	}
	paths := make([]string, 0, len(j.Files)+len(j.Assertions))
	for i := range j.Files {
		paths = append(paths, j.Files[i].Path)
	}
	for i := range j.Assertions {
		paths = append(paths, j.Assertions[i].Path)
	}
	sort.Strings(paths)
	locks, err := acquireLocks(ctx, paths)
	if err != nil {
		return err
	}
	defer releaseLocks(locks)
	if err := inject(cfg, FaultPoint{Stage: FaultAfterLocks, Index: -1}); err != nil {
		return err
	}
	if err := validateAssertions(j.Assertions); err != nil {
		return err
	}

	seenOld := false
	for _, file := range j.Files {
		state, err := classify(file)
		if err != nil {
			return err
		}
		switch state {
		case "old":
			seenOld = true
		case "new":
			if seenOld {
				return fmt.Errorf("%w: transaction %s is not a published prefix", ErrRecoveryBlocked, j.ID)
			}
		default:
			return fmt.Errorf("%w: third-party bytes at %s", ErrRecoveryBlocked, file.Path)
		}
	}
	for i, file := range j.Files {
		state, _ := classify(file)
		if state == "new" {
			continue
		}
		if err := inject(cfg, FaultPoint{Stage: FaultBeforePublish, Index: i, Path: file.Path}); err != nil {
			return err
		}
		if err := validatePublishPreconditions(j.Files, j.Assertions, i); err != nil {
			return err
		}
		if err := publish(file, cfg.syncDir); err != nil {
			return err
		}
		if err := inject(cfg, FaultPoint{Stage: FaultAfterPublish, Index: i, Path: file.Path}); err != nil {
			return err
		}
	}
	if err := finalizeJournal(path, stateDir, cfg.syncDir); err != nil {
		return err
	}
	slog.Info("dependency transaction recovered", "journal", filepath.Base(path), "files", len(j.Files))
	return nil
}

func normalizeDesired(desired []DesiredFile) ([]DesiredFile, error) {
	files := append([]DesiredFile(nil), desired...)
	for i := range files {
		path, err := canonicalDestination(files[i].Path)
		if err != nil {
			return nil, err
		}
		files[i].Path = path
		if err := validateGeneration(files[i].Expected); err != nil {
			return nil, fmt.Errorf("dependency transaction: expected generation for %s: %w", path, err)
		}
		if files[i].ExpectedMode != files[i].ExpectedMode.Perm() {
			return nil, fmt.Errorf("dependency transaction: invalid expected mode %o for %s", files[i].ExpectedMode, path)
		}
		if files[i].Mode != files[i].Mode.Perm() {
			return nil, fmt.Errorf("dependency transaction: invalid mode %o for %s", files[i].Mode, path)
		}
	}
	sort.Slice(files, func(i, k int) bool { return files[i].Path < files[k].Path })
	for i := 1; i < len(files); i++ {
		if files[i-1].Path == files[i].Path {
			return nil, fmt.Errorf("dependency transaction: duplicate destination %s", files[i].Path)
		}
	}
	return files, nil
}

// ValidateDestination validates the lexical destination used by a dependency
// transaction. Existing ancestors are inspected with Lstat so a symlink cannot
// be canonicalized into a different layer before the transaction starts.
func ValidateDestination(path string) (string, error) {
	path, err := normalizeDestinationPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return "", fmt.Errorf("%w: destination is not a regular file: %s", ErrUnsafePath, path)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("dependency transaction: inspect destination %s: %w", path, err)
	}
	return path, nil
}

// CanonicalConfigDestination preserves the supported config-file symlink
// compatibility, but only when the target remains adjacent to the logical
// config path. It never follows an ancestor symlink, and therefore cannot
// redirect a config transaction (or its adjacent lock) outside its layer.
func CanonicalConfigDestination(path string) (string, error) {
	path, err := normalizeDestinationPath(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("dependency transaction: inspect config destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: config destination is not a regular file: %s", ErrUnsafePath, path)
		}
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolve config destination symlink %s: %v", ErrUnsafePath, path, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || filepath.Dir(resolved) != parent {
		return "", fmt.Errorf("%w: config symlink target is outside its logical layer: %s", ErrUnsafePath, path)
	}
	resolvedInfo, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: inspect config symlink target %s: %v", ErrUnsafePath, resolved, err)
	}
	if !resolvedInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%w: config symlink target is not a regular file: %s", ErrUnsafePath, resolved)
	}
	return resolved, nil
}

func canonicalDestination(path string) (string, error) {
	return ValidateDestination(path)
}

func normalizeDestinationPath(path string) (string, error) {
	if err := validateCleanAbsolutePath(path); err != nil {
		return "", err
	}
	// macOS exposes /var, /tmp, and /etc as root-level aliases into /private.
	// Resolve those platform aliases so ordinary temporary and system paths are
	// usable, while still rejecting every symlink below the trusted root alias.
	root := filepath.VolumeName(path) + string(filepath.Separator)
	relative := strings.TrimPrefix(path, root)
	first := relative
	if separator := strings.IndexByte(relative, byte(filepath.Separator)); separator >= 0 {
		first = relative[:separator]
	}
	if first != "" {
		alias := filepath.Join(root, first)
		info, err := os.Lstat(alias)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("%w: inspect destination root %s: %v", ErrUnsafePath, alias, err)
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(alias)
			if resolveErr != nil {
				return "", fmt.Errorf("%w: resolve destination root alias %s: %v", ErrUnsafePath, alias, resolveErr)
			}
			expected := filepath.Join(string(filepath.Separator), "private", first)
			if resolved != expected || (first != "var" && first != "tmp" && first != "etc") {
				return "", fmt.Errorf("%w: symlink in destination ancestors: %s", ErrUnsafePath, alias)
			}
			path = filepath.Join(resolved, strings.TrimPrefix(path, alias))
		}
	}
	if err := validateDestinationAncestors(filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, nil
}

func validateCleanAbsolutePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: destination must be a clean absolute path: %s", ErrUnsafePath, path)
	}
	return nil
}

func validateDestinationAncestors(parent string) error {
	for current := parent; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: symlink in destination ancestors: %s", ErrUnsafePath, current)
			}
			if !info.IsDir() {
				return fmt.Errorf("%w: destination ancestor is not a directory: %s", ErrUnsafePath, current)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%w: inspect destination ancestor %s: %v", ErrUnsafePath, current, err)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func prepareStateDir(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%w: state directory must be a clean absolute path", ErrUnsafePath)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("dependency transaction: create state directory: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return "", fmt.Errorf("%w: state directory contains a symlink: %s", ErrUnsafePath, path)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: state path is not a directory: %s", ErrUnsafePath, path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return "", fmt.Errorf("%w: state directory is not owned by the current user: %s", ErrUnsafePath, path)
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(path, 0o700); err != nil {
			return "", fmt.Errorf("dependency transaction: secure state directory: %w", err)
		}
	}
	return path, nil
}

func inspectDestination(path string) (Generation, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Generation{}, 0, nil
	}
	if err != nil {
		return Generation{}, 0, fmt.Errorf("dependency transaction: inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Generation{}, 0, fmt.Errorf("%w: destination is not regular: %s", ErrUnsafePath, path)
	}
	generation, err := ReadGeneration(path)
	return generation, info.Mode().Perm(), err
}

func stageFile(destination string, data []byte, mode fs.FileMode) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(destination), ".dependencytxn-*.tmp")
	if err != nil {
		return "", fmt.Errorf("dependency transaction: create staged file: %w", err)
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := ensureContained(filepath.Dir(destination), path); err != nil {
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return "", fmt.Errorf("dependency transaction: set staged mode: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return "", fmt.Errorf("dependency transaction: write staged file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("dependency transaction: sync staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("dependency transaction: close staged file: %w", err)
	}
	ok = true
	return path, nil
}

func persistJournal(stateDir string, j *journal, syncDir func(string) error) (string, error) {
	raw, err := json.Marshal(j)
	if err != nil {
		return "", fmt.Errorf("dependency transaction: marshal journal: %w", err)
	}
	path := filepath.Join(stateDir, ".dependencytxn-"+j.ID+".json")
	temp, err := os.CreateTemp(stateDir, ".dependencytxn-journal-*.tmp")
	if err != nil {
		return "", fmt.Errorf("dependency transaction: create journal: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", fmt.Errorf("dependency transaction: publish journal: %w", err)
	}
	if err := syncDir(stateDir); err != nil {
		// The rename is already visible and the journal remains the durable
		// recovery authority even if its directory fsync failed.
		return path, err
	}
	return path, nil
}

func validatePublishPreconditions(files []journalFile, assertions []journalAssertion, publishIndex int) error {
	if err := validateAssertions(assertions); err != nil {
		return err
	}
	for i, file := range files {
		state, err := classify(file)
		if err != nil {
			return err
		}
		want := "old"
		if i < publishIndex {
			want = "new"
		}
		if state != want {
			return fmt.Errorf("%w: generation changed before publishing %s", ErrRecoveryBlocked, files[publishIndex].Path)
		}
	}
	return nil
}

func validateAssertions(assertions []journalAssertion) error {
	for _, assertion := range assertions {
		generation, mode, err := inspectDestination(assertion.Path)
		if err != nil {
			return err
		}
		if generation != assertion.Expected || mode != assertion.Mode {
			return fmt.Errorf("%w: asserted generation changed at %s", ErrRecoveryBlocked, assertion.Path)
		}
	}
	return nil
}

func publish(file journalFile, syncDir func(string) error) error {
	state, err := classify(file)
	if err != nil {
		return err
	}
	if state == "new" {
		return nil
	}
	if state != "old" {
		return fmt.Errorf("%w: third-party bytes at %s", ErrRecoveryBlocked, file.Path)
	}
	if err := validateStaged(file); err != nil {
		return err
	}
	if err := os.Rename(file.Temp, file.Path); err != nil {
		return fmt.Errorf("dependency transaction: publish %s: %w", file.Path, err)
	}
	return syncDir(filepath.Dir(file.Path))
}

func classify(file journalFile) (string, error) {
	generation, mode, err := inspectDestination(file.Path)
	if err != nil {
		return "", err
	}
	if generation == file.Old && (!generation.Exists || mode == file.OldMode) {
		return "old", nil
	}
	if generation.Exists && generation.Digest == file.NewDigest && mode == file.NewMode {
		return "new", nil
	}
	return "foreign", nil
}

func validateStaged(file journalFile) error {
	if err := ensureContained(filepath.Dir(file.Path), file.Temp); err != nil {
		return err
	}
	raw, info, err := readRegularFile(file.Temp, 0)
	if err != nil {
		return fmt.Errorf("%w: staged file unavailable for %s", ErrRecoveryBlocked, file.Path)
	}
	if Digest(raw) != file.NewDigest || info.Mode().Perm() != file.NewMode {
		return fmt.Errorf("%w: staged file changed for %s", ErrRecoveryBlocked, file.Path)
	}
	return nil
}

func validateJournal(j *journal) error {
	if j.Version != journalVersion || j.ID == "" || len(j.Files) == 0 {
		return fmt.Errorf("%w: invalid journal header", ErrRecoveryBlocked)
	}
	destinations := make(map[string]struct{}, len(j.Files)+len(j.Assertions))
	previous := ""
	for _, file := range j.Files {
		canonical, err := canonicalDestination(file.Path)
		if err != nil || canonical != file.Path || file.Path <= previous {
			return fmt.Errorf("%w: unsafe or unordered journal destination %s", ErrRecoveryBlocked, file.Path)
		}
		if err := validateGeneration(file.Old); err != nil || !validDigest(file.NewDigest) {
			return fmt.Errorf("%w: invalid journal digest for %s", ErrRecoveryBlocked, file.Path)
		}
		if file.NewMode != file.NewMode.Perm() || file.OldMode != file.OldMode.Perm() {
			return fmt.Errorf("%w: invalid journal mode for %s", ErrRecoveryBlocked, file.Path)
		}
		if err := ensureContained(filepath.Dir(file.Path), file.Temp); err != nil {
			return fmt.Errorf("%w: %v", ErrRecoveryBlocked, err)
		}
		destinations[file.Path] = struct{}{}
		previous = file.Path
	}
	previous = ""
	for _, assertion := range j.Assertions {
		canonical, err := canonicalDestination(assertion.Path)
		_, duplicate := destinations[assertion.Path]
		if err != nil || canonical != assertion.Path || assertion.Path <= previous || duplicate {
			return fmt.Errorf("%w: unsafe, duplicate, or unordered journal assertion %s", ErrRecoveryBlocked, assertion.Path)
		}
		if err := validateGeneration(assertion.Expected); err != nil || !assertion.Expected.Exists || assertion.Mode != assertion.Mode.Perm() {
			return fmt.Errorf("%w: invalid journal assertion for %s", ErrRecoveryBlocked, assertion.Path)
		}
		destinations[assertion.Path] = struct{}{}
		previous = assertion.Path
	}
	return nil
}

func validateGeneration(g Generation) error {
	if g.Exists != (g.Digest != "") || g.Exists && !validDigest(g.Digest) {
		return errors.New("existence and SHA-256 digest disagree")
	}
	return nil
}

func validDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil && strings.ToLower(digest) == digest
}

func ensureContained(dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: path %s escapes %s", ErrUnsafePath, path, dir)
	}
	return nil
}

func finalizeJournal(path, stateDir string, syncDir func(string) error) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("dependency transaction: remove journal: %w", err)
	}
	return syncDir(stateDir)
}

func fsyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dependency transaction: open directory for sync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("dependency transaction: sync directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("dependency transaction: close directory after sync: %w", err)
	}
	return nil
}

func desiredPaths(files []DesiredFile) []string {
	paths := make([]string, len(files))
	for i := range files {
		paths[i] = files[i].Path
	}
	return paths
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return "", fmt.Errorf("dependency transaction: generate journal ID: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func applyOptions(opts []Option) options {
	cfg := options{syncDir: fsyncDir}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func inject(cfg options, point FaultPoint) error {
	if cfg.fault == nil {
		return nil
	}
	return cfg.fault(point)
}
