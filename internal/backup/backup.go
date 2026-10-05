// Package backup creates timestamped backups of machine-scope harness
// directories before materialization, with automatic two-axis rotation
// (per-day cap and total-day cap).
//
// Backups include the component subdirectories and config files that
// agent-marketplace materializes into (skills, commands, agents, hooks,
// MCP configs, etc.), not the entire harness directory. User-owned files
// inside those component subdirectories are included so restore can recover
// the pre-sync state of the affected paths.
//
// # Why this package is machine scope only, and stays that way
//
// Repo-scope syncs merge into user-owned aggregates too — <repo>/.claude/hooks/hooks.json,
// <repo>/.cursor/mcp.json, <repo>/.gemini/settings.json, <repo>/.windsurf/mcp_config.json
// and the rest — and none of them is archived here. That is deliberate, and bead
// agent-marketplace-5exj.30 re-examined it once TargetInfo.AggregateFiles.Project
// became truthful (bead .27) and a repo-scope collector became a small edit. The
// conclusion was to NOT write one. The reasoning, so the next person does not have
// to rediscover it:
//
//  1. THE ARCHIVE NAMESPACE HAS NO ROOT DIMENSION. Create writes to
//     <backupsDir>/<target>/<timestamp>.tar.gz and Restore takes only a target,
//     returning the newest archive in that one directory; cmd/agent-marketplace's
//     `backup restore` then overlays it into harness.MachineRoot(target). Adding
//     repo-scope archives would drop every repo's .cursor tree AND ~/.cursor into
//     the same rotation window, so `backup restore --target cursor` could overlay
//     one repository's committed config into the machine root, and two repositories
//     synced on the same day would evict each other under the per-day cap.
//     Covering repo scope correctly therefore means re-keying the on-disk layout by
//     canonical root, re-keying Restore, ListBackups and the CLI, and migrating
//     existing archives. That is a store redesign, not a collector.
//     TestBackupArchiveNamespaceHasNoRootDimension in internal/pipeline pins this
//     property, so the "it is a small change" claim cannot be made again by
//     inspection.
//  2. THAT REDESIGN ALREADY EXISTS, ONE PACKAGE OVER. internal/ownership keys its
//     state and its safety backups by canonical absolute root (ownership.RootKey),
//     stores raw pre-mutation bytes under $XDG_STATE_HOME — outside the repository,
//     so the archive never needs gitignoring and never enters a diff — retains the
//     five newest per file, and is MANDATORY: it runs even when the configured
//     backup setting here is disabled. It is already root-generic and already used
//     at BOTH scopes, so Devin's <repo>/.devin/hooks.v1.json and
//     <repo>/.devin/mcp_config.json are covered today. The remaining five targets
//     get the same coverage, at both scopes, as
//     docs/plans/2026-08-04-marker-to-ownership-migration.md's M4-M8 move each one
//     onto that store. Building a second, weaker, opt-out-able mechanism here for
//     files a pending migration moves to the stronger one would be solving the
//     problem twice and deleting one of the answers.
//  3. "GIT IS THE IMPLICIT RECOVERY MECHANISM" IS NOT A LEVER THIS PRODUCT HOLDS.
//     agent-marketplace writes exactly one .gitignore entry in its entire lifetime
//     — `.agents/config.local.yaml`, at the repository root
//     (pipeline.EnsureLocalConfigGitignored). It writes no .gitignore into any
//     generated harness directory. Whether .claude/, .cursor/, .gemini/ or .devin/
//     is ignored is the user's own repository policy, which docs/adoption-guide.md
//     recommends but explicitly leaves to them. So there is no product behaviour to
//     "stop", and committing generated output to make the git story real would put
//     per-sync churn into every user's history — the opposite of the design.
//  4. THE INTERIM EXPOSURE IS BOUNDED BY A PROPERTY, NOT BY LUCK. The marker-merge
//     write path no longer rewrites content it does not own: beads .26 and .28 put
//     both the write and the prune path on pkg/harness-compilation/jsonc, so an
//     unmanaged entry keeps its exact bytes, its position and the comments above
//     it, a duplicate key is refused with the file left byte-identical, and
//     malformed JSON is refused unchanged. A handwritten repo-scope aggregate is
//     therefore not damaged by a merge in the first place, which is why there is
//     nothing for a repo-scope archive to recover.
//     materialize.TestRepoScopeAggregateMergePreservesHandwrittenContent pins that
//     property over every declared repo-scope aggregate of every target, so it
//     cannot silently regress while this decision stands on it.
//
// If you are here to add repo-scope coverage anyway, the answer is almost
// certainly to flip a target's harness.TargetInfo.OwnershipReconciledAggregates
// and route it through internal/ownership (that is the migration), not to widen
// pipeline.CollectManagedPaths. If you genuinely need it HERE, point 1 is the work
// item you have to do first.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

const (
	backupsDirName   = "backups"
	defaultMaxDays   = 14
	defaultMaxPerDay = 10
)

// Config controls backup behavior.
type Config struct {
	Enabled   bool
	MaxDays   int
	MaxPerDay int
}

// DefaultConfig returns the default backup configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:   true,
		MaxDays:   defaultMaxDays,
		MaxPerDay: defaultMaxPerDay,
	}
}

// Dir returns the backups root directory under the agent-marketplace data dir.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("backup: home directory: %w", err)
	}
	return filepath.Join(home, ".agent-marketplace", backupsDirName), nil
}

// ManagedPaths describes which paths within a machine-scope harness root
// should be backed up.
type ManagedPaths struct {
	Root        string
	RelPaths    []string
	MappedFiles map[string]string
}

// Create backs up the given managed paths into timestamped tar.gz archives
// under <backupsDir>/<target>/<timestamp>.tar.gz.
// Targets with no existing managed paths are silently skipped.
func Create(managed map[hcc.Target]ManagedPaths) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	now := time.Now()
	timestamp := now.Format("2006-01-02-150405")

	for target, mp := range managed {
		existing, err := existingPaths(mp.Root, mp.RelPaths)
		if err != nil {
			return fmt.Errorf("backup: scan %s: %w", string(target), err)
		}
		mapped, err := existingMappedFiles(mp.MappedFiles)
		if err != nil {
			return fmt.Errorf("backup: scan mapped %s: %w", target, err)
		}
		if len(existing) == 0 && len(mapped) == 0 {
			continue
		}
		targetBackupDir := filepath.Join(dir, string(target))
		if err := os.MkdirAll(targetBackupDir, 0o755); err != nil {
			return fmt.Errorf("backup: create target dir %s: %w", targetBackupDir, err)
		}
		archivePath, err := nextArchivePath(targetBackupDir, timestamp)
		if err != nil {
			return err
		}
		if err := createArchive(mp.Root, existing, archivePath, mapped); err != nil {
			return fmt.Errorf("backup: archive %s: %w", string(target), err)
		}
	}

	return nil
}

// Rotate enforces the two-axis retention policy:
//   - within each calendar day keep at most cfg.MaxPerDay archives (oldest removed first)
//   - keep archives from at most cfg.MaxDays distinct calendar days (oldest days removed first)
func Rotate(cfg Config) error {
	maxDays := cfg.MaxDays
	if maxDays <= 0 {
		maxDays = defaultMaxDays
	}
	maxPerDay := cfg.MaxPerDay
	if maxPerDay <= 0 {
		maxPerDay = defaultMaxPerDay
	}

	dir, err := Dir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		targetDir := filepath.Join(dir, entry.Name())
		if entry.Name() == codexTargetName {
			if err := rotateCodexSets(targetDir, maxDays, maxPerDay); err != nil {
				return err
			}
			continue
		}
		if err := rotateTarget(targetDir, maxDays, maxPerDay); err != nil {
			return err
		}
	}
	return nil
}

// rotateTarget applies the two-axis retention policy to a single target directory.
func rotateTarget(targetDir string, maxDays, maxPerDay int) error {
	archives, err := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
	if err != nil {
		return fmt.Errorf("backup: list archives in %s: %w", targetDir, err)
	}
	if len(archives) == 0 {
		return nil
	}
	sortBackupArchives(archives)

	// Group by calendar day (first 10 chars of the archive name: YYYY-MM-DD).
	byDay := make(map[string][]string)
	var dayOrder []string
	for _, a := range archives {
		name := strings.TrimSuffix(filepath.Base(a), ".tar.gz")
		day := archiveDay(name)
		if _, seen := byDay[day]; !seen {
			dayOrder = append(dayOrder, day)
		}
		byDay[day] = append(byDay[day], a)
	}

	// Step 1: per-day cap — remove oldest beyond maxPerDay within each day.
	for _, day := range dayOrder {
		group := byDay[day]
		if len(group) > maxPerDay {
			toRemove := group[:len(group)-maxPerDay]
			for _, p := range toRemove {
				if err := os.Remove(p); err != nil {
					return fmt.Errorf("backup: remove old archive %s: %w", p, err)
				}
			}
			byDay[day] = group[len(group)-maxPerDay:]
		}
	}

	// Step 2: day cap — dayOrder is already sorted ascending; keep the newest maxDays days.
	if len(dayOrder) > maxDays {
		oldDays := dayOrder[:len(dayOrder)-maxDays]
		for _, day := range oldDays {
			for _, p := range byDay[day] {
				if err := os.Remove(p); err != nil {
					return fmt.Errorf("backup: remove old archive %s: %w", p, err)
				}
			}
		}
	}
	return nil
}

// archiveDay extracts the YYYY-MM-DD prefix from an archive base name.
func archiveDay(name string) string {
	const dayLen = len("2006-01-02")
	if len(name) >= dayLen {
		return name[:dayLen]
	}
	return name
}

// Restore overlays the latest backup for the given target into dstDir.
// The destination directory must exist. Files created after the backup are
// left in place. Returns the timestamp of the restored backup, or an error if
// no backup exists.
func Restore(target hcc.Target, dstDir string, mapped ...map[string]string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(dir, string(target))
	archives, err := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
	if err != nil {
		return "", fmt.Errorf("backup: list archives in %s: %w", targetDir, err)
	}
	if len(archives) == 0 {
		return "", fmt.Errorf("backup: no backups found for target %q", target)
	}
	sortBackupArchives(archives)
	latest := archives[len(archives)-1]
	timestamp := strings.TrimSuffix(filepath.Base(latest), ".tar.gz")

	if err := extractArchive(latest, dstDir, mapped...); err != nil {
		return "", fmt.Errorf("backup: restore %s: %w", string(target), err)
	}
	return timestamp, nil
}

// ListBackups returns available backups grouped by target.
func ListBackups() (map[hcc.Target][]string, error) {
	result := make(map[hcc.Target][]string)
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		target := hcc.Target(entry.Name())
		targetDir := filepath.Join(dir, entry.Name())
		if entry.Name() == codexTargetName {
			sets, err := loadCompleteCodexSets(targetDir)
			if err != nil {
				return nil, fmt.Errorf("backup: list Codex sets: %w", err)
			}
			if len(sets) > 0 {
				operations := make([]string, 0, len(sets))
				for _, set := range sets {
					operations = append(operations, set.Operation)
				}
				result[target] = operations
			}
			continue
		}
		archives, err := filepath.Glob(filepath.Join(targetDir, "*.tar.gz"))
		if err != nil {
			return nil, fmt.Errorf("backup: list archives in %s: %w", targetDir, err)
		}
		sortBackupArchives(archives)
		timestamps := make([]string, len(archives))
		for i, a := range archives {
			timestamps[i] = strings.TrimSuffix(filepath.Base(a), ".tar.gz")
		}
		result[target] = timestamps
	}
	return result, nil
}

// existingPaths filters relPaths to those that exist on disk under root.
func existingPaths(root string, relPaths []string) ([]string, error) {
	var out []string
	for _, p := range relPaths {
		if _, err := os.Lstat(filepath.Join(root, p)); err == nil {
			out = append(out, p)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return out, nil
}

func createArchive(root string, relPaths []string, archivePath string, mapped ...map[string]string) (err error) {
	var source *fsboundary.Dir
	if len(relPaths) > 0 {
		absolute, resolveErr := filepath.Abs(root)
		if resolveErr != nil {
			return resolveErr
		}
		source, err = fsboundary.OpenDir(absolute)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := source.Close(); closeErr != nil {
				if err != nil {
					err = errors.Join(err, closeErr)
				} else {
					err = closeErr
				}
			}
		}()
	}
	f, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	gw := gzip.NewWriter(f)

	tw := tar.NewWriter(gw)
	defer func() {
		for _, closeFn := range []func() error{tw.Close, gw.Close, f.Close} {
			if closeErr := closeFn(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		if err != nil {
			_ = os.Remove(archivePath)
		}
	}()

	for _, rel := range relPaths {
		fullPath := filepath.Join(root, rel)
		info, err := os.Lstat(fullPath)
		if err != nil {
			return err
		}

		if info.IsDir() {
			if err := archiveDir(tw, source, root, fullPath); err != nil {
				return err
			}
		} else {
			if err := writeTarEntry(tw, source, rel, fullPath, rel, info); err != nil {
				return err
			}
		}
	}

	for _, files := range mapped {
		keys := make([]string, 0, len(files))
		for key := range files {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			info, err := os.Lstat(files[key])
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("backup: mapped file is not regular")
			}
			parent, err := fsboundary.OpenDir(filepath.Dir(files[key]))
			if err != nil {
				return err
			}
			writeErr := writeTarEntry(tw, parent, filepath.Base(files[key]), files[key], key, info)
			closeErr := parent.Close()
			switch {
			case writeErr != nil && closeErr != nil:
				return errors.Join(writeErr, closeErr)
			case writeErr != nil:
				return writeErr
			case closeErr != nil:
				return closeErr
			}
		}
	}
	return nil
}

func nextArchivePath(targetBackupDir, timestamp string) (string, error) {
	archivePath := filepath.Join(targetBackupDir, timestamp+".tar.gz")
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return archivePath, nil
	} else if err != nil {
		return "", err
	}
	for i := 1; ; i++ {
		candidate := filepath.Join(targetBackupDir, fmt.Sprintf("%s-%d.tar.gz", timestamp, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func sortBackupArchives(archives []string) {
	sort.Slice(archives, func(i, j int) bool {
		iTime, iSeq := backupArchiveKey(archives[i])
		jTime, jSeq := backupArchiveKey(archives[j])
		if iTime != jTime {
			return iTime < jTime
		}
		return iSeq < jSeq
	})
}

func backupArchiveKey(path string) (string, int) {
	name := strings.TrimSuffix(filepath.Base(path), ".tar.gz")
	if len(name) == len("2006-01-02-150405") {
		return name, 0
	}
	if len(name) < len("2006-01-02-150405")+2 {
		return name, 0
	}
	prefix := name[:len("2006-01-02-150405")]
	if len(name) <= len(prefix)+1 || name[len(prefix)] != '-' {
		return name, 0
	}
	seq, err := strconv.Atoi(name[len(prefix)+1:])
	if err != nil {
		return name, 0
	}
	return prefix, seq
}

func archiveDir(tw *tar.Writer, source *fsboundary.Dir, root, fullPath string) error {
	return filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entryRel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entryRel == "." {
			return nil
		}
		return writeTarEntry(tw, source, entryRel, path, entryRel, info)
	})
}

func writeTarEntry(tw *tar.Writer, source *fsboundary.Dir, sourceRel, fullPath, rel string, info fs.FileInfo) error {
	var link string
	isSymlink := info.Mode()&fs.ModeSymlink != 0
	if isSymlink {
		var err error
		link, err = os.Readlink(fullPath)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", fullPath, err)
		}
	}

	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = rel

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	if isSymlink || !info.Mode().IsRegular() {
		return nil
	}

	return writeCapturedBackupSource(tw, source, sourceRel, info)
}

func extractArchive(archivePath, dstDir string, mapped ...map[string]string) error {
	return withBackupPayload(archivePath, func(reader io.Reader) error { return extractArchivePayload(reader, dstDir, mapped...) })
}

func extractArchivePayload(reader io.Reader, dstDir string, mapped ...map[string]string) (err error) {
	absolute, err := filepath.Abs(dstDir)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(absolute)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	gr, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		target, err := archiveTarget(dstDir, hdr.Name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(hdr.Name, ".agent-marketplace-native/") {
			if err := restoreMappedFile(tr, hdr, mapped); err != nil {
				return err
			}
			continue
		}

		if hdr.Typeflag == tar.TypeDir || hdr.Typeflag == tar.TypeSymlink {
			fresh, captureErr := fsboundary.OpenDir(absolute)
			if captureErr != nil {
				return fmt.Errorf("%w: restore root unavailable: %w", fsboundary.ErrGenerationConflict, captureErr)
			}
			unchanged := fresh.Identity().SameObject(root.Identity())
			if closeErr := fresh.Close(); closeErr != nil {
				return closeErr
			}
			if !unchanged {
				return fsboundary.ErrGenerationConflict
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := rejectSymlinkAncestors(dstDir, target); err != nil {
				return err
			}
			rel, err := filepath.Rel(dstDir, target)
			if err != nil {
				return err
			}
			if rel == "." {
				continue
			}
			directory, err := root.EnsurePath(rel)
			if err != nil {
				return err
			}
			if err := directory.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := validateSymlinkTarget(hdr.Linkname); err != nil {
				return err
			}
			if err := rejectSymlinkAncestors(dstDir, filepath.Dir(target)); err != nil {
				return err
			}
			rel, err := filepath.Rel(dstDir, filepath.Dir(target))
			if err != nil {
				return err
			}
			parent, err := root.EnsurePath(rel)
			if err != nil {
				return err
			}
			publishErr := restoreCapturedSymlink(parent, filepath.Base(target), hdr.Linkname)
			closeErr := parent.Close()
			switch {
			case publishErr != nil && closeErr != nil:
				return errors.Join(publishErr, closeErr)
			case publishErr != nil:
				return publishErr
			case closeErr != nil:
				return closeErr
			}
		case tar.TypeReg:
			if err := rejectSymlinkAncestors(dstDir, filepath.Dir(target)); err != nil {
				return err
			}
			if info, err := os.Lstat(target); err == nil && info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf("backup: refusing to restore over symlink %s", target)
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}
			rel, err := filepath.Rel(dstDir, filepath.Dir(target))
			if err != nil {
				return err
			}
			parent, err := root.EnsurePath(rel)
			if err != nil {
				return err
			}
			publishErr := publishRestoredFile(parent, filepath.Base(target), tr, hdr.FileInfo().Mode())
			closeErr := parent.Close()
			switch {
			case publishErr != nil && closeErr != nil:
				return errors.Join(publishErr, closeErr)
			case publishErr != nil:
				return publishErr
			case closeErr != nil:
				return closeErr
			}
		}
	}
	return nil
}

func rejectSymlinkAncestors(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("backup: validate path %s: %w", target, err)
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("backup: refusing to restore through symlink %s", current)
		}
	}
	return nil
}

func archiveTarget(dstDir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("backup: invalid archive entry %q", name)
	}
	target := filepath.Join(dstDir, name)
	rel, err := filepath.Rel(dstDir, target)
	if err != nil {
		return "", fmt.Errorf("backup: validate archive entry %q: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("backup: invalid archive entry %q", name)
	}
	return target, nil
}

func validateSymlinkTarget(linkname string) error {
	if filepath.IsAbs(linkname) {
		if ok, err := isAgentMarketplaceCachePath(linkname); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("backup: invalid absolute symlink target %q", linkname)
		}
		return nil
	}
	cleaned := filepath.Clean(linkname)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backup: invalid symlink target %q", linkname)
	}
	return nil
}

func isAgentMarketplaceCachePath(path string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("backup: home directory: %w", err)
	}
	cacheRoot := filepath.Join(home, ".cache", "agent-marketplace")
	rel, err := filepath.Rel(cacheRoot, path)
	if err != nil {
		return false, fmt.Errorf("backup: validate symlink target %q: %w", path, err)
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
