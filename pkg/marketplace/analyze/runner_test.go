package analyze

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func newRunnerScript(t *testing.T, content string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runner.sh")
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	digest, err := sha256Hex(path)
	if err != nil {
		t.Fatalf("sha256Hex: %v", err)
	}
	return path, "sha256:" + digest
}

func defaultRunnerConfig(script, digest string, env []string) RunnerConfig {
	return RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
		EnvPassthrough:       env,
	}
}

type fakeResource struct {
	kind string
	name string
}

func (f *fakeResource) Kind() ir.ResourceKind {
	switch f.kind {
	case "command":
		return ir.KindCommand
	case "subagent":
		return ir.KindSubagent
	case "hook":
		return ir.KindHook
	case "mcpserver":
		return ir.KindMcpServer
	default:
		return ir.KindSkill
	}
}
func (f *fakeResource) Name() string { return f.name }

func TestRunnerSuccess(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"found a dangerous URL","findings":[{"code":"acme.runner.dangerous-url","message":"dangerous URL","source_range":{"file":"body","line":1,"column":1}}]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	audit, err := r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Files:    nil,
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if audit.Verdict != VerdictWarn {
		t.Fatalf("verdict = %q, want warn", audit.Verdict)
	}
	if len(audit.Findings) != 1 || audit.Findings[0].Code != "acme.runner.dangerous-url" {
		t.Fatalf("findings = %v", audit.Findings)
	}
}

func TestRunnerVersionMismatch(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"2.0.0","summary":"x","findings":[]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
	if !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerIDMismatch(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"wrong.runner","analyzer_version":"1.0.0","summary":"x","findings":[]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected id mismatch error")
	}
	if !strings.Contains(err.Error(), "id mismatch") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerMalformedResponse(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf 'this is not json'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected malformed response error")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerTrailingJSON(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","summary":"x","findings":[]}{"extra":1}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected trailing JSON error")
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerOversizedResponse(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
awk 'BEGIN{for(i=0;i<200;i++)printf "x"; exit}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       128,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected oversized response error")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerOversizedStderr(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
# valid stdout
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}'
# large stderr
awk 'BEGIN{for(i=0;i<5000;i++)printf "x"; exit}' >&2
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected oversized stderr error")
	}
	if !strings.Contains(err.Error(), "stderr") || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerOversizedStdout(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
awk 'BEGIN{for(i=0;i<5000;i++)printf "x"; exit}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected oversized stdout error")
	}
	if !strings.Contains(err.Error(), "stdout") || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerStderrOverflowNoDeadlock(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}'
awk 'BEGIN{for(i=0;i<131072;i++)printf "x"; exit}' >&2
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	start := time.Now()
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected stderr overflow error")
	}
	if !strings.Contains(err.Error(), "stderr") || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("error = %q", err.Error())
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("overflow handling took too long: %v", d)
	}
}

func TestRunnerNonzeroExit(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
echo "something on stderr" >&2
exit 1
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected nonzero exit error")
	}
	if !strings.Contains(err.Error(), "exited with code 1") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerTimeout(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
sleep 5
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              100 * time.Millisecond,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerStderrRedaction(t *testing.T) {
	t.Setenv("MY_SECRET", "super-secret-token")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
echo "MY_SECRET=$MY_SECRET" >&2
exit 1
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, []string{"MY_SECRET"}))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	engine := NewEngineWith([]Registration{{Analyzer: r, Role: RoleGating, Policy: PublicPolicy()}})
	report, err := engine.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("engine Analyze: %v", err)
	}
	if len(report.Audits) != 1 {
		t.Fatalf("expected 1 audit, got %d", len(report.Audits))
	}
	a := report.Audits[0]
	if !strings.Contains(a.Error, "exited with code") {
		t.Fatalf("expected redacted error, got %q", a.Error)
	}
	if strings.Contains(a.Error, "super-secret-token") {
		t.Fatalf("error text leaks secret: %q", a.Error)
	}
	if !strings.Contains(a.Error, "[REDACTED]") {
		t.Fatalf("error text did not redact secret: %q", a.Error)
	}
	if a.Findings[0].Consentable == nil || *a.Findings[0].Consentable {
		t.Fatalf("audit-unavailable must be non-consentable")
	}
}

func TestRunnerUndeclaredEnvIsolation(t *testing.T) {
	t.Setenv("TEST_SECRET", "should-not-pass")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
if [ -z "${TEST_SECRET+x}" ]; then
  printf '"NOTSET"'
else
  printf '"%s"' "$TEST_SECRET"
fi
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected malformed JSON because only NOTSET was on stdout")
	}
	if strings.Contains(err.Error(), "should-not-pass") {
		t.Fatalf("undeclared env leaked: %q", err.Error())
	}
}

func TestRunnerDeclaredEnvPassed(t *testing.T) {
	t.Setenv("TEST_TOKEN", "declared-secret")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
if [ -n "${TEST_TOKEN}" ]; then
  printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"x","findings":[{"code":"acme.runner.dangerous-url","message":"TEST_TOKEN is present","source_range":{"file":"body","line":1,"column":1}}]}'
else
  printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}'
fi
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
		EnvPassthrough:       []string{"TEST_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	audit, err := r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !strings.Contains(audit.Findings[0].Message, "TEST_TOKEN is present") {
		t.Fatalf("declared env not passed: %q", audit.Findings[0].Message)
	}
	if strings.Contains(audit.Findings[0].Message, "declared-secret") {
		t.Fatalf("declared env secret leaked into finding: %q", audit.Findings[0].Message)
	}
}

func TestRunnerRedactsEchoedSecret(t *testing.T) {
	t.Setenv("TEST_TOKEN", "super-secret-xyz")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"x","findings":[{"code":"acme.runner.dangerous-url","message":"the token value is %s","source_range":{"file":"body","line":1,"column":1}}]}' "$TEST_TOKEN"
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
		EnvPassthrough:       []string{"TEST_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	audit, err := r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !strings.Contains(audit.Findings[0].Message, "the token value is [REDACTED]") {
		t.Fatalf("secret was not redacted: %q", audit.Findings[0].Message)
	}
	if strings.Contains(audit.Findings[0].Message, "super-secret-xyz") {
		t.Fatalf("secret value leaked: %q", audit.Findings[0].Message)
	}
}

func TestRunnerDigestMismatch(t *testing.T) {
	script, _ := newRunnerScript(t, `#!/bin/sh
printf 'x'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, "sha256:"+strings.Repeat("0", 64), nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected digest mismatch")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("error = %q", err.Error())
	}
}

// TestRunnerExecutesStagedCopy proves Analyze executes the private staged copy
// inside the isolated run directory rather than re-opening the configured
// command path after hashing.
func TestRunnerExecutesStagedCopy(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","summary":"%s","findings":[]}' "$0"
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	audit, err := r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if audit.Summary == script {
		t.Fatalf("runner executed the configured path %q instead of a staged copy", script)
	}
	if !strings.Contains(audit.Summary, "am-analyzer-") || !strings.HasSuffix(audit.Summary, "runner-executable") {
		t.Fatalf("runner did not execute the staged copy, executed: %q", audit.Summary)
	}
}

// TestRunnerStageVerifiedExecutable proves the staged copy carries exactly the
// bytes that were hashed and an executable mode, so implementation_digest binds
// the executed file identity.
func TestRunnerStageVerifiedExecutable(t *testing.T) {
	content := "#!/bin/sh\nread -r _\nexit 0\n"
	script, digest := newRunnerScript(t, content)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	dir := t.TempDir()
	staged, err := r.stageVerifiedExecutable(dir, 0)
	if err != nil {
		t.Fatalf("stageVerifiedExecutable: %v", err)
	}
	if filepath.Dir(staged) != dir {
		t.Fatalf("staged copy escaped the run dir: %q", staged)
	}
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("ReadFile staged: %v", err)
	}
	if string(got) != content {
		t.Fatalf("staged bytes differ from hashed bytes: %q", got)
	}
	fi, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("Stat staged: %v", err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Fatalf("staged copy is not executable: %v", fi.Mode().Perm())
	}

	// Mutating the configured path after staging must not affect the copy.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err = os.ReadFile(staged)
	if err != nil {
		t.Fatalf("ReadFile staged after mutation: %v", err)
	}
	if string(got) != content {
		t.Fatalf("staged copy changed with the configured path: %q", got)
	}
}

func TestRunnerRequiresAbsoluteCommand(t *testing.T) {
	_, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig("relative-script", "sha256:"+strings.Repeat("0", 64), nil))
	if err == nil {
		t.Fatal("expected error for relative command")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerInvalidVerdict(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"nope","findings":[]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected invalid verdict error")
	}
	if !strings.Contains(err.Error(), "verdict") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerInputBounds(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ctx := context.Background()
	base := Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		samples: []textSample{
			{path: "body", content: strings.Repeat("x", 10)},
		},
		Files: nil,
	}

	if _, err := r.Analyze(ctx, base, nil); err != nil {
		t.Fatalf("exact MaxFileBytes should pass: %v", err)
	}

	over := base
	over.samples = []textSample{{path: "body", content: strings.Repeat("x", 11)}}
	over.Limits = Limits{MaxFileBytes: 10, MaxTotalBytes: 1024}
	if _, err := r.Analyze(ctx, over, nil); err == nil || !strings.Contains(err.Error(), "MaxFileBytes") {
		t.Fatalf("expected MaxFileBytes error for one over, got %v", err)
	}

	total := base
	total.Files = FileSet{"support.txt": []byte(strings.Repeat("x", 10))}
	total.Limits = Limits{MaxFileBytes: 100, MaxTotalBytes: 20}
	if _, err := r.Analyze(ctx, total, nil); err != nil {
		t.Fatalf("exact MaxTotalBytes should pass: %v", err)
	}

	total.Files = FileSet{"support.txt": []byte(strings.Repeat("x", 11))}
	if _, err := r.Analyze(ctx, total, nil); err == nil || !strings.Contains(err.Error(), "MaxTotalBytes") {
		t.Fatalf("expected MaxTotalBytes error for one over, got %v", err)
	}
}

func TestRunnerCanonicalDigest(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
printf 'x'
`)
	plain := strings.TrimPrefix(digest, "sha256:")

	if _, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil)); err != nil {
		t.Fatalf("sha256: prefix should be accepted: %v", err)
	}
	if _, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, plain, nil)); err == nil {
		t.Fatal("expected error for plain hex digest")
	}
	if _, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, "md5:"+strings.Repeat("0", 32), nil)); err == nil {
		t.Fatal("expected error for non-sha256 digest")
	}
	if _, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, "sha256:not-hex", nil)); err == nil {
		t.Fatal("expected error for invalid hex digest")
	}
}

func TestRunnerRejectsWrongCodeNamespace(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"x","findings":[{"code":"marketplace.find.dangerous-url","message":"x"}]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected rejection for wrong code namespace")
	}
	if !strings.Contains(err.Error(), "not namespaced") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerAnalyzeChecksCommandExists(t *testing.T) {
	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              "/nonexistent/runner",
		Timeout:              30 * time.Second,
		ImplementationDigest: "sha256:" + strings.Repeat("0", 64),
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "regular executable file") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunnerAnalyzeChecksCommandExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              path,
		Timeout:              30 * time.Second,
		ImplementationDigest: "sha256:" + strings.Repeat("0", 64),
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-executable command")
	}
	if !strings.Contains(err.Error(), "executable") {
		t.Fatalf("error = %q", err.Error())
	}
}

// TestRunnerChildEnvironmentCaptured dumps the child's real environment back
// through the stderr diagnostic channel and asserts undeclared parent
// variables (including ambient ones like HOME) are absent while a declared
// passthrough name is present.
func TestRunnerChildEnvironmentCaptured(t *testing.T) {
	t.Setenv("UNDECLARED_SECRET", "undeclared-parent-value")
	t.Setenv("DECLARED_TOKEN", "declared-parent-value")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
env | sort >&2
exit 1
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              30 * time.Second,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
		EnvPassthrough:       []string{"DECLARED_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err == nil {
		t.Fatal("expected nonzero exit error carrying captured env")
	}
	if strings.Contains(err.Error(), "UNDECLARED_SECRET") || strings.Contains(err.Error(), "undeclared-parent-value") {
		t.Fatalf("undeclared parent env leaked into child: %q", err.Error())
	}
	if strings.Contains(err.Error(), "HOME=") {
		t.Fatalf("ambient parent env leaked into child: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "DECLARED_TOKEN=") {
		t.Fatalf("declared passthrough missing from child env: %q", err.Error())
	}
	if strings.Contains(err.Error(), "declared-parent-value") {
		t.Fatalf("declared passthrough value must be redacted: %q", err.Error())
	}
}

// TestRunnerTimeoutDescendantHoldingPipes proves Analyze returns within the
// configured timeout even when a spawned descendant keeps the inherited
// stdout/stderr pipes open after the direct child is killed.
func TestRunnerTimeoutDescendantHoldingPipes(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
# Descendant inherits stdout/stderr pipes and outlives any direct kill.
sleep 60 &
sleep 60
`)

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              script,
		Timeout:              300 * time.Millisecond,
		ImplementationDigest: digest,
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	start := time.Now()
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q", err.Error())
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Analyze blocked on descendant-held pipes: %v", elapsed)
	}
}

// TestRunnerRedactsProtocolErrorStrings proves the runner redacts declared
// passthrough secrets from every runner-controlled protocol error string:
// schema version, analyzer id/version, verdict, finding code, and JSON decode
// errors.
func TestRunnerRedactsProtocolErrorStrings(t *testing.T) {
	t.Setenv("TEST_TOKEN", "super-secret-xyz")

	cases := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "schema version",
			payload: `{"schema_version":"super-secret-xyz","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}`,
			wantErr: "schema version mismatch",
		},
		{
			name:    "analyzer id",
			payload: `{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"super-secret-xyz","analyzer_version":"1.0.0","verdict":"pass","findings":[]}`,
			wantErr: "id mismatch",
		},
		{
			name:    "analyzer version",
			payload: `{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"super-secret-xyz","verdict":"pass","findings":[]}`,
			wantErr: "version mismatch",
		},
		{
			name:    "verdict",
			payload: `{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"super-secret-xyz","findings":[]}`,
			wantErr: "verdict is not valid",
		},
		{
			name:    "finding code",
			payload: `{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"x","findings":[{"code":"other.super-secret-xyz","message":"x"}]}`,
			wantErr: "not namespaced",
		},
		{
			name:    "malformed JSON",
			payload: `{"schema_version":super-secret-xyz`,
			wantErr: "not valid JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, digest := newRunnerScript(t, "#!/bin/sh\nread -r _\nprintf '%s' '"+tc.payload+"'\n")

			r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, []string{"TEST_TOKEN"}))
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			_, err = r.Analyze(context.Background(), Subject{
				Resource: &fakeResource{kind: "skill", name: "test"},
				Limits:   DefaultLimits(),
			}, nil)
			if err == nil {
				t.Fatalf("expected %q error", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
			if strings.Contains(err.Error(), "super-secret-xyz") {
				t.Fatalf("declared secret leaked into protocol error: %q", err.Error())
			}
		})
	}
}

// TestRunnerRedactsFindingCodeAndSourceFile proves declared passthrough
// secrets cannot persist in runner-controlled finding metadata. parseResponse
// redacts Code and SourceRange.File before storing the audit.
func TestRunnerRedactsFindingCodeAndSourceFile(t *testing.T) {
	t.Setenv("TEST_TOKEN", "super-secret-xyz")

	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"warn","summary":"x","findings":[{"code":"acme.runner.%s","message":"m","source_range":{"file":"dir/%s.txt","line":1,"column":1}}]}' "$TEST_TOKEN" "$TEST_TOKEN"
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, []string{"TEST_TOKEN"}))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	audit, err := r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
		Limits:   DefaultLimits(),
	}, nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %v", audit.Findings)
	}
	f := audit.Findings[0]
	if strings.Contains(f.Code, "super-secret-xyz") {
		t.Fatalf("secret persisted in finding code: %q", f.Code)
	}
	if f.Code != "acme.runner.[REDACTED]" {
		t.Fatalf("finding code = %q, want redacted namespaced code", f.Code)
	}
	if f.SourceRange == nil || strings.Contains(f.SourceRange.File, "super-secret-xyz") {
		t.Fatalf("secret persisted in source range file: %+v", f.SourceRange)
	}
	if !strings.Contains(f.SourceRange.File, "[REDACTED]") {
		t.Fatalf("source range file not redacted: %q", f.SourceRange.File)
	}
}

// TestRunnerRejectsOversizedExecutable proves staging rejects an executable
// larger than the input bound instead of allocating its full size.
func TestRunnerRejectsOversizedExecutable(t *testing.T) {
	script, digest := newRunnerScript(t, `#!/bin/sh
read -r _
printf '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"acme.runner","analyzer_version":"1.0.0","verdict":"pass","findings":[]}'
`)

	r, err := NewRunner("acme.runner", "1.0.0", defaultRunnerConfig(script, digest, nil))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if _, err := r.stageVerifiedExecutable(t.TempDir(), 4); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized executable rejection, got %v", err)
	}
}

// TestRunnerRejectsOversizedExecutableFallback proves the strict documented
// maximum bounds staged executable input through Analyze.
func TestRunnerRejectsOversizedExecutableFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge-runner")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteAt([]byte{'x'}, maxRunnerExecutableBytes); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewRunner("acme.runner", "1.0.0", RunnerConfig{
		Command:              path,
		Timeout:              30 * time.Second,
		ImplementationDigest: "sha256:" + strings.Repeat("0", 64),
		MaxOutputBytes:       64 * 1024,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_, err = r.Analyze(context.Background(), Subject{
		Resource: &fakeResource{kind: "skill", name: "test"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected strict-maximum rejection, got %v", err)
	}
}
