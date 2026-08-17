package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
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

// trustedOwnerFn is the predicate used to decide whether a file's owner is
// trusted for the managed-installation organization config. It is a variable
// so tests can inject a deterministic predicate without requiring root.
var trustedOwnerFn = isTrustedOwner

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
		if !trustedOwnerFn(info) {
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
		case source == Organization && (cfg.APIVersion == APIVersionV2 || cfg.APIVersion == APIVersionV3):
			d.State = ManagedInstallationManaged
			d.Validation = "valid"
		case cfg.APIVersion == APIVersionV2 || cfg.APIVersion == APIVersionV3:
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
