package targetprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/rootkey"
)

type ApprovalEnvironment struct {
	Platform, Home, XDGConfig, CanonicalStateRoot string
	Override                                      *string
}

type ExecutableBinding struct {
	CanonicalPath string `json:"canonical_path" yaml:"canonical_path"`
	SHA256        string `json:"sha256" yaml:"sha256"`
}

type ApprovalBinding struct {
	OS, CanonicalRoot, CanonicalStateRoot, StateRootKey string
	Profile                                             Identity
	UsedOverride                                        bool
	CommandRoles                                        []string
	Executable                                          ExecutableBinding
}

func ResolveApprovalBinding(profile Profile, identity Identity, env ApprovalEnvironment) (ApprovalBinding, error) {
	root, usedOverride, err := ResolvePlaceRoot(profile, identity, env)
	if err != nil {
		return ApprovalBinding{}, err
	}
	roles := approvalCommandRoles(profile)
	executable := ExecutableBinding{}
	if len(roles) != 0 {
		executable, err = approvalExecutable(profile.Host.Executable)
		if err != nil {
			return ApprovalBinding{}, err
		}
	}
	return ApprovalBinding{
		OS: env.Platform, Profile: identity, CanonicalRoot: root,
		CanonicalStateRoot: env.CanonicalStateRoot,
		StateRootKey:       rootkey.ForCanonicalRoot(env.CanonicalStateRoot),
		UsedOverride:       usedOverride, CommandRoles: roles, Executable: executable,
	}, nil
}

// ResolvePlaceRoot admits the current canonical target root without resolving a host executable.
func ResolvePlaceRoot(profile Profile, identity Identity, env ApprovalEnvironment) (root string, usedOverride bool, err error) {
	if err := identity.Validate(); err != nil {
		return "", false, err
	}
	if err := validateProfile(profile, identity.TargetID); err != nil {
		return "", false, err
	}
	if (env.Platform != "macos" && env.Platform != "linux") || !slices.Contains(profile.Platforms, env.Platform) {
		return "", false, fmt.Errorf("target.approval.unsupported-platform: %q", env.Platform)
	}
	if !filepath.IsAbs(env.CanonicalStateRoot) || filepath.Clean(env.CanonicalStateRoot) != env.CanonicalStateRoot {
		return "", false, fmt.Errorf("target.approval.state-root-invalid: state root must be absolute and clean")
	}
	bases, err := approvalBases(profile.Roots.User, env)
	if err != nil {
		return "", false, err
	}
	root, usedOverride, err = approvalRoot(profile.Roots.User, env, bases)
	if err != nil {
		return "", false, err
	}
	return root, usedOverride, nil
}

func (b ApprovalBinding) Match(current ApprovalBinding) error {
	for _, diff := range []struct {
		mismatch          bool
		category, message string
	}{
		{b.OS != current.OS, "os", "approved OS differs from current OS"},
		{b.Profile != current.Profile, "profile-identity", "approved profile identity differs from current identity"},
		{b.CanonicalRoot != current.CanonicalRoot, "root", "approved root differs from current root"},
		{b.UsedOverride != current.UsedOverride, "override", "approved override state differs from current state"},
		{b.CanonicalStateRoot != current.CanonicalStateRoot, "state-root", fmt.Sprintf("expected state root %q, actual state root %q", b.CanonicalStateRoot, current.CanonicalStateRoot)},
		{b.StateRootKey != current.StateRootKey, "state-root-key", "state root keys differ"},
		{!slices.Equal(b.CommandRoles, current.CommandRoles), "command-roles", "approved command roles differ from current roles"},
		{b.Executable != current.Executable, "executable", "approved executable binding differs from current binding"},
	} {
		if diff.mismatch {
			return fmt.Errorf("target.approval.%s-mismatch: %s", diff.category, diff.message)
		}
	}
	return nil
}

func (b ApprovalBinding) VerifyExecutable() error {
	if len(b.CommandRoles) == 0 || b.Executable == (ExecutableBinding{}) {
		return fmt.Errorf("target.approval.no-execution: binding does not authorize execution")
	}
	digest, err := captureExecutableDigest(b.Executable.CanonicalPath)
	if err != nil || digest != b.Executable.SHA256 {
		return fmt.Errorf("target.approval.executable-mismatch: approved executable is missing, changed or not executable")
	}
	return nil
}

func approvalBases(root UserRoot, env ApprovalEnvironment) (map[string]string, error) {
	home, err := canonicalApprovalDirectory(env.Home)
	if err != nil {
		return nil, fmt.Errorf("target.approval.home-invalid: %w", err)
	}
	bases := make(map[string]string, len(root.AllowedBases))
	for _, name := range root.AllowedBases {
		if name == "home" {
			bases[name] = home
			continue
		}
		path := env.XDGConfig
		if path == "" {
			path = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("target.approval.root-base-invalid: %s: directory must be absolute", name)
		}
		canonical, err := fsboundary.ResolveExistingPrefix(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("target.approval.root-base-invalid: %s: %w", name, err)
		}
		bases[name] = canonical
	}
	return bases, nil
}

func canonicalApprovalDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("directory must be absolute")
	}
	dir, err := fsboundary.OpenDir(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	return dir.Path(), nil
}

func approvalRoot(profileRoot UserRoot, env ApprovalEnvironment, bases map[string]string) (string, bool, error) {
	usedOverride := env.Override != nil
	root := ""
	if usedOverride {
		root = *env.Override
		if profileRoot.Override == "" || root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return "", false, fmt.Errorf("target.approval.override-invalid: override must be declared, nonempty, absolute and clean")
		}
	} else {
		defaultRoot := profileRoot.Default[env.Platform]
		base := bases[defaultRoot.Base]
		root = filepath.Join(base, defaultRoot.Path)
	}
	contained := false
	for _, base := range bases {
		rel, err := filepath.Rel(base, root)
		contained = contained || err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if !contained {
		return "", false, fmt.Errorf("target.approval.root-outside-allowed-bases: requested root is not strictly within an allowed base")
	}
	captured, err := fsboundary.OpenDir(root)
	if err != nil {
		return "", false, fmt.Errorf("target.approval.root-unavailable: %w", err)
	}
	defer func() { _ = captured.Close() }()
	if captured.Path() != root {
		return "", false, fmt.Errorf("target.approval.root-alias: requested root is not its canonical spelling")
	}
	return root, usedOverride, nil
}

func approvalCommandRoles(profile Profile) []string {
	roles := make([]string, 0, len(profile.Checks)+1)
	if profile.Host.VersionProbe != nil {
		roles = append(roles, "version_probe")
	}
	checks := make([]string, 0, len(profile.Checks))
	for name := range profile.Checks {
		checks = append(checks, name)
	}
	sort.Strings(checks)
	return append(roles, checks...)
}

func approvalExecutable(command string) (ExecutableBinding, error) {
	located, err := exec.LookPath(command)
	if err != nil || !filepath.IsAbs(located) {
		return ExecutableBinding{}, fmt.Errorf("target.approval.executable-unavailable: declared executable must resolve to an absolute path")
	}
	canonical, err := filepath.EvalSymlinks(located)
	if err != nil || !filepath.IsAbs(canonical) || filepath.Clean(canonical) != canonical {
		return ExecutableBinding{}, fmt.Errorf("target.approval.executable-unavailable: declared executable has no canonical absolute path")
	}
	digest, err := captureExecutableDigest(canonical)
	if err != nil {
		return ExecutableBinding{}, fmt.Errorf("target.approval.executable-invalid: declared executable is not a readable regular executable")
	}
	return ExecutableBinding{CanonicalPath: canonical, SHA256: digest}, nil
}

func captureExecutableDigest(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("executable path is not canonical")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", errors.New("executable path is no longer canonical")
	}
	parentPath := filepath.Dir(path)
	parent, err := fsboundary.OpenDir(parentPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = parent.Close() }()
	if parent.Path() != parentPath {
		return "", errors.New("executable parent is not canonical")
	}
	destination, err := fsboundary.CaptureDestination(parent, filepath.Base(path), fsboundary.LinkPolicyUser)
	if err != nil {
		return "", err
	}
	defer func() { _ = destination.Close() }()
	mode := destination.Identity().Mode
	if !destination.Exists() || destination.Path() != path || !mode.IsRegular() || mode.Perm()&0o111 == 0 {
		return "", errors.New("executable is not a regular executable file")
	}
	hash := sha256.New()
	if err := destination.ReadTargetWith(func(reader io.Reader) error {
		_, err := io.Copy(hash, reader)
		return err
	}); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
