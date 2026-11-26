package targetprofile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalReceiptPersistsAndRequiresExactBinding(t *testing.T) {
	fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
	binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	if err := RequireApproval(binding); err == nil || !strings.Contains(err.Error(), "target.approval.required") {
		t.Fatalf("RequireApproval absent receipt error = %v", err)
	}
	if entries, err := os.ReadDir(fixture.stateRoot); err != nil || len(entries) != 0 {
		t.Fatalf("RequireApproval created receipt state: entries=%v err=%v", entries, err)
	}
	if err := RecordApproval(binding); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}
	entries, err := os.ReadDir(fixture.stateRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("receipt entries = %v, %v; want exactly one", entries, err)
	}
	receiptPath := filepath.Join(fixture.stateRoot, entries[0].Name())
	if !strings.HasPrefix(entries[0].Name(), "target-approval-") || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("receipt name = %q", entries[0].Name())
	}
	info, err := os.Stat(receiptPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode = %v, %v; want 0600", info, err)
	}
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if receipt["schema_version"] != float64(1) || receipt["os"] != binding.OS ||
		receipt["target_id"] != binding.Profile.TargetID || receipt["profile_digest"] != binding.Profile.RevisionDigest ||
		receipt["profile_source_id"] != binding.Profile.SourceID || receipt["canonical_root"] != binding.CanonicalRoot ||
		receipt["used_override"] != binding.UsedOverride {
		t.Fatalf("receipt metadata = %#v", receipt)
	}
	store, ok := receipt["state_store"].(map[string]any)
	if !ok || store["canonical_root"] != binding.CanonicalStateRoot || store["root_key"] != binding.StateRootKey {
		t.Fatalf("receipt state store = %#v", receipt["state_store"])
	}
	roles, ok := receipt["command_roles"].([]any)
	executable, executableOK := receipt["executable"].(map[string]any)
	if !ok || len(roles) == 0 || !executableOK ||
		executable["canonical_path"] != binding.Executable.CanonicalPath ||
		executable["sha256"] != binding.Executable.SHA256 {
		t.Fatalf("receipt execution binding = %#v", receipt)
	}
	if err := RequireApproval(binding); err != nil {
		t.Fatalf("RequireApproval: %v", err)
	}
	before, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordApproval(binding); err != nil {
		t.Fatalf("RecordApproval unchanged: %v", err)
	}
	after, err := os.Stat(receiptPath)
	afterRaw, readErr := os.ReadFile(receiptPath)
	if err != nil || readErr != nil || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(raw, afterRaw) {
		t.Fatalf("unchanged receipt was rewritten: before=%v after=%v read=%v", before.ModTime(), after, readErr)
	}
}

func TestApprovalReceiptExecutableChangeRefusesOldReceipt(t *testing.T) {
	fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
	oldBinding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	if err := RecordApproval(oldBinding); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.stateRoot, entries[0].Name())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeApprovalTestFile(t, fixture.executable, "changed executable\n", 0o755)
	newBinding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	requireApprovalError(t, RequireApproval(newBinding), "target.approval.executable-mismatch")
	requireApprovalError(t, RequireApproval(oldBinding), "target.approval.executable-mismatch")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("old receipt changed after executable update: err=%v", err)
	}
	if err := RecordApproval(newBinding); err != nil {
		t.Fatalf("RecordApproval updated executable: %v", err)
	}
	if err := RequireApproval(newBinding); err != nil {
		t.Fatalf("RequireApproval updated executable: %v", err)
	}
}

func TestApprovalReceiptNoExecutionWireValue(t *testing.T) {
	fixture := newApprovalTestFixture(t, profileWithoutCommandRoles())
	t.Setenv("PATH", filepath.Join(fixture.env.Home, "missing"))
	binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	if len(binding.CommandRoles) != 0 || binding.Executable != (ExecutableBinding{}) {
		t.Fatalf("no-command binding = %#v", binding)
	}
	if err := RecordApproval(binding); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixture.stateRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("receipt entries = %v, %v", entries, err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture.stateRoot, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"command_roles":[]`)) || !bytes.Contains(raw, []byte(`"executable":"no-execution"`)) {
		t.Fatalf("no-execution receipt = %s", raw)
	}
	if err := RequireApproval(binding); err != nil {
		t.Fatalf("RequireApproval no-execution: %v", err)
	}
}

func TestApprovalReceiptRejectsLinksAndTampering(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
		binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
		if err := RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(fixture.stateRoot)
		path := filepath.Join(fixture.stateRoot, entries[0].Name())
		raw, _ := os.ReadFile(path)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(t.TempDir(), "external")
		writeApprovalTestFile(t, external, string(raw), 0o600)
		if err := os.Symlink(external, path); err != nil {
			t.Fatal(err)
		}
		requireApprovalError(t, RequireApproval(binding), "target.approval.receipt-invalid")
		requireApprovalError(t, RecordApproval(binding), "target.approval.receipt-invalid")
		assertReceiptDirectoryHasOnlyLink(t, fixture.stateRoot, filepath.Base(path), os.ModeSymlink)
		got, err := os.ReadFile(external)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("symlink target changed: %v", err)
		}
	})

	t.Run("hardlink", func(t *testing.T) {
		fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
		binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
		if err := RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(fixture.stateRoot)
		path := filepath.Join(fixture.stateRoot, entries[0].Name())
		raw, _ := os.ReadFile(path)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(t.TempDir(), "external")
		writeApprovalTestFile(t, external, string(raw), 0o600)
		if err := os.Link(external, path); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		requireApprovalError(t, RequireApproval(binding), "target.approval.receipt-invalid")
		requireApprovalError(t, RecordApproval(binding), "target.approval.receipt-invalid")
		after, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Fatal("hardlink receipt was replaced")
		}
		assertReceiptDirectoryHasOnlyLink(t, fixture.stateRoot, filepath.Base(path), 0)
		got, err := os.ReadFile(external)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("hardlink target changed: %v", err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
		binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
		if err := RecordApproval(binding); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(fixture.stateRoot)
		path := filepath.Join(fixture.stateRoot, entries[0].Name())
		valid, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(valid, []byte(`{"schema_version":1,`)) {
			t.Fatalf("baseline receipt is invalid: %v", err)
		}
		tampered := append([]byte(`{"schema_version":1,`), valid[1:]...)
		writeApprovalTestFile(t, path, string(tampered), 0o600)
		before, _ := os.ReadFile(path)
		requireApprovalError(t, RequireApproval(binding), "target.approval.receipt-invalid")
		requireApprovalError(t, RecordApproval(binding), "target.approval.receipt-invalid")
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("malformed receipt changed: %v", err)
		}
	})
}

func TestApprovalReceiptRejectsInvalidUTF8Path(t *testing.T) {
	fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
	binding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	binding.CanonicalRoot = filepath.Join(fixture.env.Home, "invalid-"+string([]byte{0xff}))
	requireApprovalError(t, RecordApproval(binding), "target.approval.binding-invalid")
	requireApprovalError(t, RequireApproval(binding), "target.approval.binding-invalid")
	if entries, err := os.ReadDir(fixture.stateRoot); err != nil || len(entries) != 0 {
		t.Fatalf("invalid path created receipt: entries=%v err=%v", entries, err)
	}
}

func assertReceiptDirectoryHasOnlyLink(t *testing.T, root, name string, linkType os.FileMode) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != name ||
		(linkType != 0 && entries[0].Type()&linkType == 0) {
		t.Fatalf("state directory residue = %v, %v; want only original link %q", entries, err, name)
	}
}

func TestApprovalReceiptSeparatesCanonicalRoots(t *testing.T) {
	fixture := newApprovalTestFixture(t, []byte(validProfileYAML))
	oldBinding := resolveApprovalTest(t, fixture.profile, fixture.identity, fixture.env)
	if err := RecordApproval(oldBinding); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(fixture.stateRoot)
	oldPath := filepath.Join(fixture.stateRoot, entries[0].Name())
	oldBytes, _ := os.ReadFile(oldPath)
	newRoot := filepath.Join(fixture.env.Home, "other-app")
	newState := filepath.Join(fixture.env.Home, ".local", "state", "other-marketplace")
	if err := os.Mkdir(newRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(newState, 0o755); err != nil {
		t.Fatal(err)
	}
	env := fixture.env
	env.Override = &newRoot
	env.CanonicalStateRoot = newState
	newBinding := resolveApprovalTest(t, fixture.profile, fixture.identity, env)
	requireApprovalError(t, RequireApproval(newBinding), "target.approval.required")
	if entries, err := os.ReadDir(newState); err != nil || len(entries) != 0 {
		t.Fatalf("new state root was modified: entries=%v err=%v", entries, err)
	}
	if got, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(got, oldBytes) {
		t.Fatalf("old state root changed: %v", err)
	}
	override := fixture.root
	overrideEnv := fixture.env
	overrideEnv.Override = &override
	overrideBinding := resolveApprovalTest(t, fixture.profile, fixture.identity, overrideEnv)
	if overrideBinding.CanonicalRoot != oldBinding.CanonicalRoot || overrideBinding.UsedOverride == oldBinding.UsedOverride {
		t.Fatalf("override fixture does not isolate override state: old=%#v new=%#v", oldBinding, overrideBinding)
	}
	requireApprovalError(t, RequireApproval(overrideBinding), "target.approval.required")
	if entries, err := os.ReadDir(fixture.stateRoot); err != nil || len(entries) != 1 {
		t.Fatalf("override lookup modified state: entries=%v err=%v", entries, err)
	}
	if got, err := os.ReadFile(oldPath); err != nil || !bytes.Equal(got, oldBytes) {
		t.Fatalf("override lookup changed original receipt: %v", err)
	}
}
