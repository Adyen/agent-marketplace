package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// ManagedInstallationState is the high-level managed/unmanaged state of the
// externally managed organization config.
type ManagedInstallationState int

const (
	ManagedInstallationUnknown ManagedInstallationState = iota
	ManagedInstallationUnmanaged
	ManagedInstallationManaged
)

// String returns "managed", "unmanaged", or "unknown".
func (s ManagedInstallationState) String() string {
	switch s {
	case ManagedInstallationManaged:
		return "managed"
	case ManagedInstallationUnmanaged:
		return "unmanaged"
	default:
		return "unknown"
	}
}

// ManagedInstallationDiagnostics describes the filesystem and trust state of
// the fixed organization config path. It is intentionally independent of
// the parsed config contents so diagnostics can be reported even when the
// file is unreadable or invalid.
type ManagedInstallationDiagnostics struct {
	Path          string
	State         ManagedInstallationState
	Validation    string
	Owner         string
	Group         string
	Mode          os.FileMode
	FileType      string
	SymlinkTarget string
	Warnings      []error
}

// Managed reports whether the diagnostics indicate a valid managed installation.
func (d ManagedInstallationDiagnostics) Managed() bool {
	return d.State == ManagedInstallationManaged
}

// TrustWarning is an actionable trust finding about the organization config file.
type TrustWarning struct {
	Code string
	Path string
	Msg  string
	Hint string
}

// Error returns the warning message.
func (w *TrustWarning) Error() string { return w.Msg }

// DedupKey returns a stable key for deduplicating warnings that differ only in
// target-specific details.
func (w *TrustWarning) DedupKey() string { return w.Code + "\x00" + w.Path }

// Remediation returns a user-facing hint for fixing the trust issue.
func (w *TrustWarning) Remediation() string { return w.Hint }

type OrganizationTrustError struct {
	Code string
	Path string
	Err  error
}

func (e *OrganizationTrustError) Error() string {
	return fmt.Sprintf("%s: organization authority %q: %v", e.Code, e.Path, e.Err)
}
func (e *OrganizationTrustError) Unwrap() error { return e.Err }

// trustedOwnerFn is the predicate used to decide whether a file's owner is
// trusted for the managed-installation organization config. It is a variable
// so tests can inject a deterministic predicate without requiring root.
var trustedOwnerFn = isTrustedOwner

func organizationTrustError(code, path string, err error) error {
	return &OrganizationTrustError{Code: code, Path: path, Err: err}
}

func testPathTrustEnabled(path string) bool {
	return (testing.Testing() || testOrganizationConfigPath != "") && filepath.Clean(path) != defaultOrganizationConfigPath
}

func trustedOrganizationOwner(info os.FileInfo, configPath string) bool {
	return trustedOwnerFn(info) || testPathTrustEnabled(configPath) && ownedByCurrentProcess(info)
}

func ValidateOrganizationConfigAuthority(configPath string) error {
	if err := validateOrganizationAuthorityDirectory(filepath.Dir(configPath), configPath); err != nil {
		return err
	}
	return validateOrganizationNode(configPath, configPath, "config")
}

func OrganizationLockPath(configPath string) string {
	path, err := lockfile.PathForConfig(configPath)
	if err == nil {
		return path
	}
	return filepath.Join(filepath.Dir(configPath), "lock.yaml")
}

func ValidateOrganizationTrustUnit(configPath string, requireLock, allowMissingLock bool) error {
	if err := ValidateOrganizationConfigAuthority(configPath); err != nil {
		return err
	}
	if !requireLock {
		return nil
	}
	lockPath := OrganizationLockPath(configPath)
	if err := validateOrganizationNode(lockPath, configPath, "lock"); err != nil {
		if allowMissingLock && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, info, err := lockfile.ReadBytesInfo(lockPath)
	if err != nil {
		return organizationTrustError("organization.trust.lock.read", lockPath, err)
	}
	if err := validateOrganizationNodeInfo(info, lockPath, configPath, "lock"); err != nil {
		return err
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		return organizationTrustError("organization.trust.lock.malformed", lockPath, err)
	}
	if err := lockfile.ValidateScope(doc, lockfile.ScopeOrganization); err != nil {
		return organizationTrustError("organization.trust.lock.scope", lockPath, err)
	}
	return nil
}

func validateOrganizationAuthorityDirectory(authorityDir, configPath string) error {
	authorityDir = filepath.Clean(authorityDir)
	boundary := organizationTrustBoundary(authorityDir, configPath)
	for current := authorityDir; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return organizationTrustError("organization.trust.ancestor.inspect", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return organizationTrustError("organization.trust.ancestor.symlink", current, errors.New("organization authority ancestor is a symlink"))
		}
		if !info.IsDir() {
			return organizationTrustError("organization.trust.ancestor.non-directory", current, errors.New("organization authority ancestor is not a directory"))
		}
		// Test-only organization paths live below the process temporary
		// directory, whose permissions and owner are controlled by the test
		// harness rather than by the organization authority. Keep validating
		// the boundary's type and symlink status, but preserve the existing
		// owner override and metadata checks for every fixture ancestor below it.
		boundaryNode := current == boundary
		if !boundaryNode || !testPathTrustEnabled(configPath) {
			if info.Mode().Perm()&0o022 != 0 {
				return organizationTrustError("organization.trust.ancestor.writable", current, fmt.Errorf("mode %04o permits group or other writes", info.Mode().Perm()))
			}
			if !trustedOrganizationOwner(info, configPath) {
				return organizationTrustError("organization.trust.ancestor.owner", current, errors.New("organization authority ancestor is not owned by a trusted principal"))
			}
		}
		if boundaryNode || filepath.Dir(current) == current {
			return nil
		}
	}
}

func organizationTrustBoundary(authorityDir, configPath string) string {
	if testPathTrustEnabled(configPath) {
		boundary := filepath.Clean(os.TempDir())
		if isPathWithin(boundary, authorityDir) {
			return boundary
		}
		return filepath.Dir(authorityDir)
	}
	return filepath.Dir(defaultOrganizationConfigPath)
}

func isPathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateOrganizationNode(path, configPath, kind string) error {
	info, err := os.Lstat(path)
	if err != nil {
		code := "organization.trust." + kind + ".inspect"
		if errors.Is(err, fs.ErrNotExist) {
			code = "organization.trust." + kind + ".missing"
		}
		return organizationTrustError(code, path, err)
	}
	return validateOrganizationNodeInfo(info, path, configPath, kind)
}

func validateOrganizationNodeInfo(info os.FileInfo, path, configPath, kind string) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return organizationTrustError("organization.trust."+kind+".symlink", path, errors.New(kind+" is a symlink"))
	}
	if !info.Mode().IsRegular() {
		return organizationTrustError("organization.trust."+kind+".non-regular", path, errors.New(kind+" is not a regular file"))
	}
	if info.Mode().Perm()&0o022 != 0 {
		return organizationTrustError("organization.trust."+kind+".writable", path, fmt.Errorf("mode %04o permits group or other writes", info.Mode().Perm()))
	}
	if !trustedOrganizationOwner(info, configPath) {
		return organizationTrustError("organization.trust."+kind+".owner", path, errors.New(kind+" is not owned by a trusted principal"))
	}
	return nil
}

// inspectManagedConfig inspects path using filesystem metadata and the load
// outcome represented by readErr, cfg, source, and loadErrs. It returns a
// ManagedInstallationDiagnostics value that is safe to use even when cfg is nil.
func inspectManagedConfig(path string, readErr error, cfg *Config, source Source, loadErrs []error) ManagedInstallationDiagnostics {
	d := ManagedInstallationDiagnostics{
		Path:       path,
		State:      ManagedInstallationUnmanaged,
		Validation: "absent",
		FileType:   "missing",
		Owner:      "-",
		Group:      "-",
	}

	info, lstatErr := os.Lstat(path)
	if lstatErr != nil {
		if !errors.Is(lstatErr, fs.ErrNotExist) {
			d.Validation = "unreadable"
			d.FileType = "unknown"
			d.Warnings = append(d.Warnings, &TrustWarning{
				Code: "org-config-unreadable",
				Path: path,
				Msg:  fmt.Sprintf("organization config %q cannot be inspected: %v", path, lstatErr),
				Hint: "check permissions and filesystem state",
			})
		}
		return d
	}

	d.Mode = info.Mode().Perm()
	d.FileType = fileTypeString(info.Mode())
	d.Owner, d.Group = ownerAndGroup(info)

	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		d.SymlinkTarget = target
		d.Warnings = append(d.Warnings, &TrustWarning{
			Code: "org-config-symlink",
			Path: path,
			Msg:  fmt.Sprintf("organization config %q is a symlink to %q", path, target),
			Hint: "verify the symlink target is under IT control and cannot be redirected",
		})
	} else if info.Mode().IsRegular() {
		if d.Mode&0o022 != 0 {
			d.Warnings = append(d.Warnings, &TrustWarning{
				Code: "org-config-writable",
				Path: path,
				Msg:  fmt.Sprintf("organization config %q is writable by group or other (mode %04o)", path, d.Mode),
				Hint: "restrict write permissions to the file owner only",
			})
		}
		ownerTrusted := trustedOwnerFn(info)
		if !testing.Testing() && testPathTrustEnabled(path) {
			ownerTrusted = trustedOrganizationOwner(info, path)
		}
		if !ownerTrusted {
			d.Warnings = append(d.Warnings, &TrustWarning{
				Code: "org-config-wrong-owner",
				Path: path,
				Msg:  fmt.Sprintf("organization config %q has the wrong owner %s:%s; expected root", path, d.Owner, d.Group),
				Hint: "change ownership to root so the file cannot be tampered with by unprivileged users",
			})
		}
	} else {
		d.Warnings = append(d.Warnings, &TrustWarning{
			Code: "org-config-non-regular",
			Path: path,
			Msg:  fmt.Sprintf("organization config %q is not a regular file (type: %s)", path, d.FileType),
			Hint: "the organization config should be a regular YAML file",
		})
	}

	// Determine validation from the load outcome. The fixed location remains
	// authoritative, so trust findings do not block loading.
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) && d.FileType == "missing" {
			d.Validation = "absent"
		} else {
			d.Validation = "unreadable"
		}
		return d
	}

	if len(loadErrs) > 0 {
		d.Validation = "invalid"
		return d
	}

	if cfg != nil {
		validVersion := cfg.APIVersion == APIVersionV2 || cfg.APIVersion == APIVersionV3
		switch {
		case source == Organization && validVersion:
			d.State = ManagedInstallationManaged
			d.Validation = "valid"
		case validVersion:
			d.Validation = "untrusted-source"
		default:
			d.Validation = "unsupported-version"
		}
	}

	return d
}

// LogManagedDiagnostics records the managed-installation state and any trust
// warnings to the structured log. It should be called once per command
// invocation that loads the effective configuration. The contents of the parsed
// organization config are intentionally not logged.
func LogManagedDiagnostics(d ManagedInstallationDiagnostics) {
	slog.Info("managed installation state",
		"path", d.Path,
		"managed", d.Managed(),
		"validation", d.Validation,
		"owner", d.Owner,
		"group", d.Group,
		"mode", fmt.Sprintf("%04o", d.Mode),
		"file_type", d.FileType,
	)
	for _, w := range d.Warnings {
		var tw *TrustWarning
		if errors.As(w, &tw) {
			slog.Warn(tw.Error(), "code", tw.Code, "path", tw.Path)
		} else {
			slog.Warn(w.Error())
		}
	}
}

// fileTypeString, ownerAndGroup, and isTrustedOwner are implemented in
// diagnostics_unix.go and diagnostics_windows.go.
