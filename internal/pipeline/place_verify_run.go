//go:build unix

package pipeline

import (
	"context"
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
)

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
	if prepared.profile.Host.VersionProbe != nil {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.version-probe-unsupported: host version probes are not supported yet")
	}
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
	beforePkg, afterPkg := prepared.pkg, post.pkg
	beforePkg.Verification, afterPkg.Verification = nil, nil
	if !prepared.rootIdentity.SameObject(post.rootIdentity) ||
		!prepared.subjectIdentity.SameObject(post.subjectIdentity) ||
		!reflect.DeepEqual(beforePkg, afterPkg) {
		return ownership.PlaceVerification{}, fmt.Errorf("target.place.check-state-changed: installed package changed during check")
	}
	result := ownership.PlaceVerification{
		Binding: prepared.pkg.Binding, GenerationKey: prepared.pkg.GenerationKey,
		Check: prepared.checkName, Scope: prepared.scope, HostVersionStatus: "unknown",
		Outcome: "pass", EvidenceSource: "target-verify",
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

func runApprovedPlaceCheck(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	argv []string,
	home, dir string,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, binding.Executable.CanonicalPath, argv...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + home, "LANG=C"}
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
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
