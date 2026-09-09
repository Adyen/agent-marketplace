package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func captureCodexRepairLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

func prepareCodexRepairFixture(t *testing.T, repoRoot string) (string, []resolver.ResolvedArtifact) {
	t.Helper()
	selection := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "managed", Resource: ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetCodex},
	}}
	if report, err := materialize.Run(context.Background(), repoRoot, selection); err != nil || diagHasErrors(report) {
		t.Fatalf("seed Codex sync: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "config.toml"), selection
}

func diagHasErrors(report *materialize.Report) bool {
	return report == nil || diag.HasErrors(report.Diags)
}

func scenarioDoctorHelpDescribesCodexRepair(t *testing.T) {
	cmd := newDoctorCmd()
	for _, want := range []string{"Devin and Codex shared aggregate files", ".codex/hooks.json", ".codex/config.toml"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("doctor Long help missing %q:\n%s", want, cmd.Long)
		}
	}
	if usage := cmd.Flags().Lookup("repair").Usage; !strings.Contains(usage, "Devin and Codex") {
		t.Errorf("--repair usage = %q, want Devin and Codex", usage)
	}
}

func scenarioDoctorRepairIncludesCodexAndYesAdoptsExactMatch(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	configPath, selection := prepareCodexRepairFixture(t, repoRoot)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("doctor --repair --yes: %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"repair: Codex shared-aggregate ownership",
		"adopt mcp-server \"managed\"",
		"adopted 1 entr(y/ies) in config.toml",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("repair output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("--yes prompted for exact adoption:\n%s", out.String())
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("Codex adoption changed config.toml:\n%s", after)
	}
	preview, err := materialize.CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Actionable() {
		t.Fatal("doctor did not record Codex ownership")
	}
}

func scenarioDoctorCodexRepairRequiresInteractiveConflictConfirmationAndRedactsValues(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		userCommand      = "user-command-repair-secret"
		userToken        = "user-token-repair-secret"
		generatedCommand = "generated-command-repair-secret"
		generatedToken   = "generated-token-repair-secret"
	)
	original := "[mcp_servers.managed]\ncommand = \"" + userCommand + "\"\nenv = { TOKEN = \"" + userToken + "\" }\n"
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Name: "managed", Resource: ir.NewMcpServer(
			"managed", generatedCommand, nil, map[string]string{"TOKEN": generatedToken}, nil, ir.SourceRef{},
		)},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetCodex},
	}}
	logs := captureCodexRepairLogs(t)

	var previewOut bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &previewOut,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("non-interactive conflict preview: %v\n%s", err, previewOut.String())
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != original {
		t.Fatalf("--yes replaced a conflicting Codex value: %s (err=%v)", got, err)
	}
	for _, secret := range []string{userCommand, userToken, generatedCommand, generatedToken} {
		if strings.Contains(previewOut.String(), secret) {
			t.Fatalf("Codex repair preview leaked %q:\n%s", secret, previewOut.String())
		}
	}

	var declineOut bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &declineOut,
		in:      strings.NewReader("n\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("decline conflict replacement: %v\n%s", err, declineOut.String())
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != original {
		t.Fatalf("declined replacement changed config.toml: %s (err=%v)", got, err)
	}

	var repairOut bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &repairOut,
		in:      strings.NewReader("y\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("interactive conflict replacement: %v\n%s", err, repairOut.String())
	}
	if !strings.Contains(repairOut.String(), "replaced mcp-server \"managed\" in config.toml") {
		t.Fatalf("replacement was not reported:\n%s", repairOut.String())
	}
	for _, secret := range []string{userCommand, userToken, generatedCommand, generatedToken} {
		if strings.Contains(repairOut.String(), secret) {
			t.Fatalf("Codex repair action leaked %q:\n%s", secret, repairOut.String())
		}
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), userCommand) || !strings.Contains(string(after), generatedCommand) {
		t.Fatalf("confirmed replacement wrote the wrong value:\n%s", after)
	}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(canonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := os.ReadDir(store.SafetyBackupDir("config.toml"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("confirmed replacement has no fresh safety backup: entries=%d err=%v", len(backups), err)
	}
	for _, want := range []string{
		`msg="doctor repair preview start"`,
		`msg="doctor repair preview complete"`,
		`msg="doctor repair execution start"`,
		`msg="doctor repair decision"`,
		"target=codex",
		"action=replace",
		"decision=declined",
		`msg="doctor repair execution complete"`,
		"status=success",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("repair logs missing %q:\n%s", want, logs.String())
		}
	}
	for _, secret := range []string{userCommand, userToken, generatedCommand, generatedToken} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("Codex repair logs leaked %q:\n%s", secret, logs.String())
		}
	}
}

func scenarioDoctorCodexRepairCorruptStateFailsClosedWithNonzeroResult(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	selection := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "managed-mcp", Resource: ir.NewMcpServer("managed-mcp", "mcp-binary", nil, nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetCodex},
		},
		{
			Entry:     catalog.Entry{Name: "managed-hook", Resource: ir.NewHookWithMatcher("managed-hook", "PreToolUse", "Bash", "./hook-command", "command", nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetCodex},
		},
	}
	if report, err := materialize.Run(context.Background(), repoRoot, selection); err != nil || diagHasErrors(report) {
		t.Fatalf("seed Codex aggregates: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	const stateSecret = "state-payload-must-not-print"
	if err := os.WriteFile(store.StateFile(), []byte(`{"secret":"`+stateSecret+`"`), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeHooks, err := os.ReadFile(filepath.Join(root, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeMCP, err := os.ReadFile(filepath.Join(root, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	logs := captureCodexRepairLogs(t)

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err == nil {
		t.Fatalf("corrupt Codex ownership state returned success:\n%s", out.String())
	}
	for _, code := range []string{diag.CodeOwnershipStateCorrupt, diag.CodeDoctorRepairFailed} {
		if !strings.Contains(out.String(), code) {
			t.Fatalf("corrupt-state output missing %q:\n%s", code, out.String())
		}
	}
	if strings.Contains(out.String(), stateSecret) {
		t.Fatalf("corrupt-state output leaked state payload:\n%s", out.String())
	}
	for _, want := range []string{`msg="doctor repair execution failed"`, "status=error", "code=doctor.repair.failed"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("corrupt-state logs missing %q:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), stateSecret) {
		t.Fatalf("corrupt-state logs leaked state payload:\n%s", logs.String())
	}
	for path, before := range map[string][]byte{
		filepath.Join(root, "hooks.json"):  beforeHooks,
		filepath.Join(root, "config.toml"): beforeMCP,
	} {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("corrupt-state repair changed %s: %s (err=%v)", path, after, readErr)
		}
	}
}

func scenarioDoctorCodexRepairMissingDesiredAggregateWithCorruptStateFailsClosed(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(store.StateFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	const stateSecret = "missing-aggregate-state-payload-must-not-print"
	if err := os.WriteFile(store.StateFile(), []byte(`{"secret":"`+stateSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "managed-mcp", Resource: ir.NewMcpServer("managed-mcp", "mcp-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetCodex},
	}}
	logs := captureCodexRepairLogs(t)

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err == nil {
		t.Fatalf("missing aggregate with corrupt ownership state returned success:\n%s", out.String())
	}
	if !strings.Contains(out.String(), diag.CodeOwnershipStateCorrupt) {
		t.Fatalf("output missing %q:\n%s", diag.CodeOwnershipStateCorrupt, out.String())
	}
	if !strings.Contains(logs.String(), "code="+diag.CodeDoctorRepairFailed) {
		t.Fatalf("logs missing %q:\n%s", diag.CodeDoctorRepairFailed, logs.String())
	}
	if strings.Contains(out.String(), stateSecret) || strings.Contains(logs.String(), stateSecret) {
		t.Fatalf("repair leaked corrupt ownership payload:\noutput=%s\nlogs=%s", out.String(), logs.String())
	}
	if _, statErr := os.Stat(filepath.Join(root, "config.toml")); !os.IsNotExist(statErr) {
		t.Fatalf("repair created missing aggregate: %v", statErr)
	}
}

func scenarioDoctorCodexRepairUnreadableAggregatePathFailsClosed(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	const rootSecret = "not-a-directory-payload-must-not-print"
	root := filepath.Join(repoRoot, ".codex")
	if err := os.WriteFile(root, []byte(rootSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "managed-mcp", Resource: ir.NewMcpServer("managed-mcp", "mcp-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetCodex},
	}}
	logs := captureCodexRepairLogs(t)

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err == nil {
		t.Fatalf("non-directory Codex root returned success:\n%s", out.String())
	}
	if !strings.Contains(out.String(), diag.CodeMaterializeCodexAggregateMalformed) {
		t.Fatalf("output missing %q:\n%s", diag.CodeMaterializeCodexAggregateMalformed, out.String())
	}
	if !strings.Contains(logs.String(), "code="+diag.CodeDoctorRepairFailed) {
		t.Fatalf("logs missing %q:\n%s", diag.CodeDoctorRepairFailed, logs.String())
	}
	if strings.Contains(out.String(), rootSecret) || strings.Contains(logs.String(), rootSecret) {
		t.Fatalf("repair leaked aggregate payload:\noutput=%s\nlogs=%s", out.String(), logs.String())
	}
	if got, readErr := os.ReadFile(root); readErr != nil || string(got) != rootSecret {
		t.Fatalf("repair changed non-directory root: %q (err=%v)", got, readErr)
	}
}
