//go:build unix

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

// VerifyDeployedPlace verifies one installed package against the selected,
// approved deployed profile and records only the exact check outcome.
func VerifyDeployedPlace(
	ctx context.Context,
	ws Workspace,
	kind, name, installationID string,
) (ownership.PlaceVerification, error) {
	targetID, _, scoped := nameutil.SplitPlaceKind(kind)
	if !scoped || !nameutil.IsPlacePackageName(name) || validatePlaceTargetID(targetID) != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.package-invalid: kind and name must identify a scoped place package")
	}
	var decoded [sha256.Size]byte
	if len(installationID) != hex.EncodedLen(len(decoded)) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.installation-unavailable: installation ID is not a canonical lowercase SHA-256 identity")
	}
	if _, err := hex.Decode(decoded[:], []byte(installationID)); err != nil || hex.EncodeToString(decoded[:]) != installationID {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.installation-unavailable: installation ID is not a canonical lowercase SHA-256 identity")
	}
	prepared, err := loadDeployedPlaceApproval(ctx, ws, targetID)
	if err != nil {
		return ownership.PlaceVerification{}, err
	}
	if PlaceInstallationID(prepared.binding) != installationID {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.installation-unavailable: installation ID does not match the current approved root")
	}
	return runPlaceVerification(ctx, prepared.binding, prepared.raw, prepared.environment, ws.RepoRoot(), kind, name)
}

func runPlaceVerification(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	raw []byte,
	env targetprofile.ApprovalEnvironment,
	repoRoot, kind, name string,
) (ownership.PlaceVerification, error) {
	prepared, err := preparePlaceVerification(ctx, binding, raw, env, kind, name)
	if err != nil {
		return ownership.PlaceVerification{}, err
	}
	return runPreparedPlaceVerification(ctx, prepared, raw, env, repoRoot, kind, name, "target-verify")
}

func runPreparedPlaceVerification(
	ctx context.Context,
	prepared preparedPlaceVerification,
	raw []byte,
	env targetprofile.ApprovalEnvironment,
	repoRoot, kind, name, evidenceSource string,
) (ownership.PlaceVerification, error) {
	binding := prepared.binding
	if repoRoot == "" || !filepath.IsAbs(repoRoot) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.repository-invalid: repository root must be absolute")
	}
	repoPath, err := filepath.EvalSymlinks(filepath.Clean(repoRoot))
	if err != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.repository-invalid: repository root is unavailable")
	}
	repo, err := fsboundary.OpenDir(repoPath)
	if err != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.repository-invalid: repository root is not a directory")
	}
	repoPath = repo.Path()
	if err := repo.Close(); err != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.repository-invalid: close repository root: %w", err)
	}
	if placeSyncRootWithin(repoPath, binding.Executable.CanonicalPath) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.repository-executable-refused: approved executable is inside the repository")
	}
	home, err := filepath.EvalSymlinks(env.Home)
	if err != nil || !filepath.IsAbs(home) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.approval.home-invalid: approved home is unavailable")
	}
	hostVersion, hostVersionStatus, err := probePlaceHostVersion(ctx, binding, prepared.profile, home, binding.CanonicalRoot)
	if err != nil {
		return ownership.PlaceVerification{}, err
	}
	if prepared.profile.Host.VersionProbe != nil {
		afterProbe, err := preparePlaceVerification(ctx, binding, raw, env, kind, name)
		if err != nil {
			return ownership.PlaceVerification{}, fmt.Errorf("target.place.check-state-changed: %w", err)
		}
		if err := comparePreparedPlaceVerification(prepared, afterProbe); err != nil {
			return ownership.PlaceVerification{}, err
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	exitCode, err := runApprovedPlaceCheck(commandCtx, binding, prepared.argv, home, binding.CanonicalRoot)
	if ctx.Err() != nil {
		return ownership.PlaceVerification{}, ctx.Err()
	}
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.check-timeout: approved check exceeded its time limit")
	}
	if err != nil {
		return ownership.PlaceVerification{}, err
	}
	if err := ctx.Err(); err != nil {
		return ownership.PlaceVerification{}, err
	}
	post, err := preparePlaceVerification(ctx, binding, raw, env, kind, name)
	if err != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.check-state-changed: %w", err)
	}
	if err := comparePreparedPlaceVerification(prepared, post); err != nil {
		return ownership.PlaceVerification{}, err
	}
	result := ownership.PlaceVerification{
		Binding: prepared.pkg.Binding, GenerationKey: prepared.pkg.GenerationKey,
		Check: prepared.checkName, Scope: prepared.scope,
		HostVersion: hostVersion, HostVersionStatus: hostVersionStatus,
		Outcome: "pass", EvidenceSource: evidenceSource,
	}
	if exitCode != 0 {
		result.Outcome = "fail"
	}
	desired, err := prepared.owner.PreparePlaceVerification(prepared.snapshot, prepared.pkg.ID, result)
	if err != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("prepare place verification record: %w", err)
	}
	if desired.Path != "" {
		if _, err := dependencytxn.CommitPlace(ctx, prepared.stateDir, dependencytxn.PlacePlan{
			Root: binding.CanonicalRoot, RootIdentity: prepared.rootIdentity, Ownership: desired,
		}); err != nil {
			return ownership.PlaceVerification{}, fmt.Errorf("record place verification: %w", err)
		}
	} else if post.pkg.Verification == nil || *post.pkg.Verification != result {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.check-state-changed: verification record changed during check")
	}
	if result.Outcome == "fail" {
		return result, fmt.Errorf("target.place.check-failed: target %s check %s failed", binding.Profile.TargetID, prepared.checkName)
	}
	return result, nil
}

func comparePreparedPlaceVerification(before, after preparedPlaceVerification) error {
	beforePkg, afterPkg := before.pkg, after.pkg
	beforePkg.Verification, afterPkg.Verification = nil, nil
	if !before.rootIdentity.SameObject(after.rootIdentity) ||
		!before.subjectIdentity.SameObject(after.subjectIdentity) ||
		!reflect.DeepEqual(beforePkg, afterPkg) {
		return fmt.Errorf("target.place.check-state-changed: installed package changed during check")
	}
	return nil
}

func runApprovedPlaceCheck(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	argv []string,
	home, dir string,
) (int, error) {
	return runApprovedPlaceCommand(ctx, binding, argv, home, dir, io.Discard)
}

func runApprovedPlaceCommand(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	argv []string,
	home, dir string,
	stdout io.Writer,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, binding.Executable.CanonicalPath, argv...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + home, "LANG=C"}
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = stdout, io.Discard
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := binding.VerifyExecutable(); err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("target.place.check-start-failed: approved check could not be started")
	}
	err := cmd.Wait()
	if err == nil {
		return cmd.ProcessState.ExitCode(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("target.place.check-run-failed: approved check did not exit normally")
}
