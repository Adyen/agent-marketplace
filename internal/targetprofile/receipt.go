package targetprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/rootkey"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

const approvalReceiptLimit = 16 << 10

type approvalReceiptKey struct {
	OS        string   `json:"os"`
	Profile   Identity `json:"profile"`
	Root      string   `json:"root"`
	StateRoot string   `json:"state_root"`
	Override  bool     `json:"used_override"`
}

type approvalReceiptState struct {
	CanonicalRoot string `json:"canonical_root"`
	RootKey       string `json:"root_key"`
}

type approvalReceipt struct {
	SchemaVersion   int                  `json:"schema_version"`
	OS              string               `json:"os"`
	TargetID        string               `json:"target_id"`
	ProfileDigest   string               `json:"profile_digest"`
	ProfileSourceID string               `json:"profile_source_id"`
	CanonicalRoot   string               `json:"canonical_root"`
	UsedOverride    bool                 `json:"used_override"`
	StateStore      approvalReceiptState `json:"state_store"`
	CommandRoles    []string             `json:"command_roles"`
	Executable      json.RawMessage      `json:"executable"`
}

// RecordApproval stores a previously resolved binding after interactive consent
// or exact preapproval. It is a storage primitive and does not verify that a
// TTY was used or that consent occurred.
func RecordApproval(binding ApprovalBinding) error {
	want, err := approvalReceiptFor(binding)
	if err != nil {
		return err
	}
	raw, err := marshalApprovalReceipt(want)
	if err != nil {
		return err
	}
	state, err := openApprovalStateRoot(binding)
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()
	name := approvalReceiptName(binding)
	expectedPath := filepath.Join(state.Path(), name)
	destination, err := fsboundary.CaptureDestination(state, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return fmt.Errorf("target.approval.receipt-invalid: capture receipt destination: %w", err)
	}
	defer func() { _ = destination.Close() }()
	if err := validateApprovalDestination(destination, expectedPath); err != nil {
		return err
	}
	if destination.Exists() {
		existingRaw, _, err := destination.ReadTarget(approvalReceiptLimit)
		if err != nil {
			return fmt.Errorf("target.approval.receipt-invalid: read receipt: %w", err)
		}
		existing, err := decodeApprovalReceipt(existingRaw)
		if err != nil {
			return err
		}
		if bytes.Equal(existingRaw, raw) {
			return nil
		}
		existing.Executable = want.Executable
		contextRaw, err := marshalApprovalReceipt(existing)
		if err != nil {
			return err
		}
		if !bytes.Equal(contextRaw, raw) {
			return fmt.Errorf("target.approval.receipt-mismatch: existing receipt has different approval context")
		}
	}
	return publishApprovalReceipt(state, destination, raw)
}

// RequireApproval requires a canonical local receipt for a previously resolved
// binding and rechecks any authorized executable on every call.
func RequireApproval(binding ApprovalBinding) error {
	want, err := approvalReceiptFor(binding)
	if err != nil {
		return err
	}
	wantRaw, err := marshalApprovalReceipt(want)
	if err != nil {
		return err
	}
	state, err := openApprovalStateRoot(binding)
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()
	name := approvalReceiptName(binding)
	expectedPath := filepath.Join(state.Path(), name)
	destination, err := fsboundary.CaptureDestination(state, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return fmt.Errorf("target.approval.receipt-invalid: capture receipt: %w", err)
	}
	defer func() { _ = destination.Close() }()
	if !destination.Exists() {
		return fmt.Errorf("target.approval.required: no local approval receipt exists")
	}
	if err := validateApprovalDestination(destination, expectedPath); err != nil {
		return err
	}
	raw, _, err := destination.ReadTarget(approvalReceiptLimit)
	if err != nil {
		return fmt.Errorf("target.approval.receipt-invalid: read receipt: %w", err)
	}
	got, err := decodeApprovalReceipt(raw)
	if err != nil {
		return err
	}
	if !bytes.Equal(got.Executable, want.Executable) {
		return fmt.Errorf("target.approval.executable-mismatch: receipt executable differs from current binding")
	}
	if !bytes.Equal(raw, wantRaw) {
		return fmt.Errorf("target.approval.receipt-mismatch: receipt does not match current binding")
	}
	return nil
}

func approvalReceiptFor(binding ApprovalBinding) (approvalReceipt, error) {
	if err := validateApprovalBinding(binding); err != nil {
		return approvalReceipt{}, err
	}
	executable := json.RawMessage(`"no-execution"`)
	if len(binding.CommandRoles) != 0 {
		raw, err := json.Marshal(binding.Executable)
		if err != nil {
			return approvalReceipt{}, fmt.Errorf("target.approval.receipt-invalid: encode executable binding: %w", err)
		}
		executable = raw
	}
	roles := append([]string{}, binding.CommandRoles...)
	return approvalReceipt{
		SchemaVersion: 1, OS: binding.OS, TargetID: binding.Profile.TargetID,
		ProfileDigest: binding.Profile.RevisionDigest, ProfileSourceID: binding.Profile.SourceID,
		CanonicalRoot: binding.CanonicalRoot, UsedOverride: binding.UsedOverride,
		StateStore:   approvalReceiptState{CanonicalRoot: binding.CanonicalStateRoot, RootKey: binding.StateRootKey},
		CommandRoles: roles, Executable: executable,
	}, nil
}

func validateApprovalBinding(binding ApprovalBinding) error {
	fail := func(reason string) error { return fmt.Errorf("target.approval.binding-invalid: %s", reason) }
	if err := binding.Profile.Validate(); err != nil {
		return fmt.Errorf("target.approval.binding-invalid: %w", err)
	}
	if binding.OS != "linux" && binding.OS != "macos" {
		return fail("unsupported operating system")
	}
	for _, root := range []string{binding.CanonicalRoot, binding.CanonicalStateRoot} {
		if !canonicalReceiptPath(root) {
			return fail("roots must be canonical absolute paths")
		}
	}
	if approvalRootsOverlap(binding.CanonicalRoot, binding.CanonicalStateRoot) {
		return fail("target root and state root overlap")
	}
	if binding.StateRootKey != rootkey.ForCanonicalRoot(binding.CanonicalStateRoot) {
		return fail("state root key does not match state root")
	}
	if err := validateApprovalRoles(binding.CommandRoles); err != nil {
		return err
	}
	if len(binding.CommandRoles) == 0 {
		if binding.Executable != (ExecutableBinding{}) {
			return fail("no-command binding has an executable")
		}
		return nil
	}
	if !canonicalReceiptPath(binding.Executable.CanonicalPath) ||
		!nameutil.IsCanonicalSHA256Digest(binding.Executable.SHA256) {
		return fail("command roles require a canonical executable binding")
	}
	if err := binding.VerifyExecutable(); err != nil {
		return err
	}
	return nil
}

func canonicalReceiptPath(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func validateApprovalRoles(roles []string) error {
	seen := make(map[string]struct{}, len(roles))
	start := 0
	if len(roles) != 0 && roles[0] == "version_probe" {
		start = 1
	}
	if start == 0 && slices.Contains(roles, "version_probe") {
		return fmt.Errorf("target.approval.binding-invalid: version_probe role is out of order")
	}
	for i, role := range roles {
		if role == "" || strings.TrimSpace(role) != role {
			return fmt.Errorf("target.approval.binding-invalid: command roles must be nonempty clean values")
		}
		if _, exists := seen[role]; exists {
			return fmt.Errorf("target.approval.binding-invalid: command roles must be unique")
		}
		seen[role] = struct{}{}
		if i > start && roles[i-1] > role {
			return fmt.Errorf("target.approval.binding-invalid: command roles are not canonical")
		}
	}
	return nil
}

func approvalRootsOverlap(a, b string) bool {
	within := func(root, candidate string) bool {
		rel, err := filepath.Rel(root, candidate)
		return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	}
	return within(a, b) || within(b, a)
}

func openApprovalStateRoot(binding ApprovalBinding) (*fsboundary.Dir, error) {
	root, err := fsboundary.OpenDir(binding.CanonicalRoot)
	if err != nil || root.Path() != binding.CanonicalRoot {
		if root != nil {
			_ = root.Close()
		}
		return nil, fmt.Errorf("target.approval.root-invalid: target root is not canonical and available")
	}
	_ = root.Close()
	state, err := fsboundary.OpenDir(binding.CanonicalStateRoot)
	if err != nil || state.Path() != binding.CanonicalStateRoot {
		if state != nil {
			_ = state.Close()
		}
		return nil, fmt.Errorf("target.approval.state-root-invalid: state root is not canonical and available")
	}
	id := state.Identity()
	if id.UID != uint32(os.Geteuid()) || id.Mode.Perm()&0o022 != 0 {
		_ = state.Close()
		return nil, fmt.Errorf("target.approval.state-root-invalid: state root must be owner-controlled and not group or other writable")
	}
	return state, nil
}

func approvalReceiptName(binding ApprovalBinding) string {
	key, _ := json.Marshal(approvalReceiptKey{
		OS: binding.OS, Profile: binding.Profile, Root: binding.CanonicalRoot,
		StateRoot: binding.CanonicalStateRoot, Override: binding.UsedOverride,
	})
	hash := sha256.Sum256(append([]byte("agent-marketplace/target-approval/v1\x00"), key...))
	return "target-approval-" + hex.EncodeToString(hash[:]) + ".json"
}

func marshalApprovalReceipt(receipt approvalReceipt) ([]byte, error) {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("target.approval.receipt-invalid: encode receipt: %w", err)
	}
	if len(raw)+1 > approvalReceiptLimit {
		return nil, fmt.Errorf("target.approval.receipt-invalid: receipt exceeds size limit")
	}
	return append(raw, '\n'), nil
}

func decodeApprovalReceipt(raw []byte) (approvalReceipt, error) {
	var receipt approvalReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return receipt, fmt.Errorf("target.approval.receipt-invalid: decode receipt: %w", err)
	}
	canonical, err := marshalApprovalReceipt(receipt)
	if err != nil || !bytes.Equal(canonical, raw) || receipt.SchemaVersion != 1 || receipt.CommandRoles == nil {
		return receipt, fmt.Errorf("target.approval.receipt-invalid: receipt is not canonical schema version 1")
	}
	if err := validateReceiptExecutable(receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func validateReceiptExecutable(receipt approvalReceipt) error {
	if bytes.Equal(receipt.Executable, []byte(`"no-execution"`)) {
		if len(receipt.CommandRoles) != 0 {
			return fmt.Errorf("target.approval.receipt-invalid: no-execution receipt has command roles")
		}
		return nil
	}
	var executable ExecutableBinding
	if err := json.Unmarshal(receipt.Executable, &executable); err != nil {
		return fmt.Errorf("target.approval.receipt-invalid: decode executable: %w", err)
	}
	canonical, err := json.Marshal(executable)
	if err != nil || !bytes.Equal(canonical, receipt.Executable) ||
		len(receipt.CommandRoles) == 0 || !canonicalReceiptPath(executable.CanonicalPath) ||
		!nameutil.IsCanonicalSHA256Digest(executable.SHA256) {
		return fmt.Errorf("target.approval.receipt-invalid: executable representation is invalid")
	}
	return nil
}

func validateApprovalDestination(destination *fsboundary.Destination, expectedPath string) error {
	if destination.Link() != nil || destination.Path() != expectedPath {
		return fmt.Errorf("target.approval.receipt-invalid: receipt must be a direct state-root file")
	}
	if !destination.Exists() {
		return nil
	}
	id := destination.Identity()
	if !id.Mode.IsRegular() || id.UID != uint32(os.Geteuid()) || id.Links != 1 || id.Mode.Perm() != 0o600 {
		return fmt.Errorf("target.approval.receipt-invalid: receipt must be an owner-private regular file with one link")
	}
	return nil
}

func publishApprovalReceipt(state *fsboundary.Dir, destination *fsboundary.Destination, raw []byte) error {
	staged, name, err := state.CreateTemp(".target-approval-*")
	if err != nil {
		return fmt.Errorf("target.approval.receipt-write: create private staging file: %w", err)
	}
	defer func() { _ = state.Unlink(name) }()
	if err := staged.Chmod(0o600); err != nil {
		_ = staged.Close()
		return fmt.Errorf("target.approval.receipt-write: chmod staging file: %w", err)
	}
	if _, err := staged.Write(raw); err != nil {
		_ = staged.Close()
		return fmt.Errorf("target.approval.receipt-write: write staging file: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("target.approval.receipt-write: close staging file: %w", err)
	}
	if err := destination.Publish(state, name); err != nil {
		return fmt.Errorf("target.approval.receipt-write: publish receipt: %w", err)
	}
	return nil
}
