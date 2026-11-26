package targetprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type approvalTestFixture struct {
	profile                     Profile
	identity                    Identity
	env                         ApprovalEnvironment
	root, stateRoot, executable string
}

func newApprovalTestFixture(t *testing.T, data []byte) approvalTestFixture {
	t.Helper()
	id := profileIdentity(data, "invented-app")
	profile, err := Parse(data, id)
	if err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, state := filepath.Join(home, ".config", "invented-app"), filepath.Join(home, ".local", "state", "agent-marketplace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	exe := filepath.Join(bin, "invented-app-bin")
	writeApprovalTestFile(t, exe, "fixture executable text\n", 0o755)
	t.Setenv("PATH", bin)
	return approvalTestFixture{profile, id, ApprovalEnvironment{"linux", home, "", state, nil}, root, state, exe}
}

func resolveApprovalTest(t *testing.T, p Profile, id Identity, env ApprovalEnvironment) ApprovalBinding {
	t.Helper()
	binding, err := ResolveApprovalBinding(p, id, env)
	if err != nil {
		t.Fatalf("ResolveApprovalBinding() error = %v", err)
	}
	return binding
}

func requireApprovalError(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want refusal")
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not contain %q", err, part)
		}
	}
}

func writeApprovalTestFile(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func TestTargetApprovalBinding(t *testing.T) {
	t.Run("missing other allowed base does not block home default", func(t *testing.T) {
		data := replaceProfileText(t, "linux: {base: xdg-config, path: invented-app}", "linux: {base: home, path: invented-app}")
		f := newApprovalTestFixture(t, data)
		configDir := filepath.Join(f.env.Home, ".config")
		if err := os.RemoveAll(configDir); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(f.env.Home, "invented-app")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		binding, err := ResolveApprovalBinding(f.profile, f.identity, f.env)
		if err != nil {
			t.Fatalf("ResolveApprovalBinding() error = %v", err)
		}
		if binding.CanonicalRoot != root {
			t.Fatalf("canonical root = %q, want %q", binding.CanonicalRoot, root)
		}
		if _, err := os.Stat(configDir); !os.IsNotExist(err) {
			t.Fatalf("missing allowed base was created: stat error = %v", err)
		}
	})

	f := newApprovalTestFixture(t, []byte(validProfileYAML))
	approved := resolveApprovalTest(t, f.profile, f.identity, f.env)
	override := f.root
	env := f.env
	env.Override = &override
	current := resolveApprovalTest(t, f.profile, f.identity, env)
	if approved.CanonicalRoot != current.CanonicalRoot || approved.UsedOverride || !current.UsedOverride {
		t.Fatalf("bindings do not distinguish same-path override: %#v %#v", approved, current)
	}
	requireApprovalError(t, approved.Match(current), "override")

	alias := filepath.Join(filepath.Dir(f.root), "alias")
	if err := os.Symlink(f.root, alias); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, path := range []string{alias, outside} {
		env := f.env
		env.Override = &path
		_, err := ResolveApprovalBinding(f.profile, f.identity, env)
		requireApprovalError(t, err)
	}
	oldFile := filepath.Join(f.root, "keep")
	writeApprovalTestFile(t, oldFile, "old root", 0o600)
	root := filepath.Join(filepath.Dir(f.root), "replacement")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	env = f.env
	env.Override = &root
	requireApprovalError(t, current.Match(resolveApprovalTest(t, f.profile, f.identity, env)), "root-mismatch")
	if got, err := os.ReadFile(oldFile); err != nil || string(got) != "old root" {
		t.Fatalf("old root file = %q, %v", got, err)
	}
	fExec := newApprovalTestFixture(t, []byte(validProfileYAML))
	approvedExec := resolveApprovalTest(t, fExec.profile, fExec.identity, fExec.env)
	writeApprovalTestFile(t, fExec.executable, "changed fixture text\n", 0o755)
	requireApprovalError(t, approvedExec.VerifyExecutable(), "executable")

	none := newApprovalTestFixture(t, profileWithoutCommandRoles())
	t.Setenv("PATH", filepath.Join(none.env.Home, "missing"))
	binding := resolveApprovalTest(t, none.profile, none.identity, none.env)
	if len(binding.CommandRoles) != 0 || binding.Executable != (ExecutableBinding{}) {
		t.Fatalf("no-role binding = %#v", binding)
	}
	requireApprovalError(t, binding.VerifyExecutable())
	fState := newApprovalTestFixture(t, []byte(validProfileYAML))
	approvedState := resolveApprovalTest(t, fState.profile, fState.identity, fState.env)
	receipt, target := filepath.Join(fState.stateRoot, "existing-receipt"), filepath.Join(fState.root, "existing-file")
	for path, value := range map[string]string{receipt: "receipt sentinel", target: "target sentinel"} {
		writeApprovalTestFile(t, path, value, 0o600)
	}
	writeApprovalTestFile(t, fState.executable, "changed fixture text\n", 0o755)
	otherState := filepath.Join(fState.env.Home, ".local", "state", "other-agent-marketplace")
	if err := os.MkdirAll(otherState, 0o755); err != nil {
		t.Fatal(err)
	}
	stateEnv := fState.env
	stateEnv.CanonicalStateRoot = otherState
	matchErr := approvedState.Match(resolveApprovalTest(t, fState.profile, fState.identity, stateEnv))
	requireApprovalError(t, matchErr, "target.approval.state-root-mismatch", fState.stateRoot, otherState)
	for path, want := range map[string]string{receipt: "receipt sentinel", target: "target sentinel", fState.executable: "changed fixture text\n"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%q changed: got %q, err %v", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(fState.stateRoot, "new-receipt")); !os.IsNotExist(err) {
		t.Errorf("unexpected receipt: stat error = %v", err)
	}
	fSource := newApprovalTestFixture(t, []byte(validProfileYAML))
	approvedSource := resolveApprovalTest(t, fSource.profile, fSource.identity, fSource.env)
	otherIdentity := fSource.identity
	otherIdentity.SourceID = "directory:%2Fprofile%2Fcopy"
	currentSource := resolveApprovalTest(t, fSource.profile, otherIdentity, fSource.env)
	if currentSource.Profile.RevisionDigest != approvedSource.Profile.RevisionDigest {
		t.Fatal("test source identities have different profile digests")
	}
	requireApprovalError(t, approvedSource.Match(currentSource), "profile identity")
	unsupportedEnv := fSource.env
	unsupportedEnv.Platform = "windows"
	_, platformErr := ResolveApprovalBinding(fSource.profile, fSource.identity, unsupportedEnv)
	requireApprovalError(t, platformErr)
	invalidIdentity := profileIdentity([]byte(validProfileYAML), "different-app")
	_, identityErr := ResolveApprovalBinding(fSource.profile, invalidIdentity, fSource.env)
	requireApprovalError(t, identityErr)
}
