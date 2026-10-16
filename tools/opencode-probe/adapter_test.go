package opencodeprobe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	compiler "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/opencode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// adapterDriver exercises the compiled adapter's tool.execute.before callback
// directly in a Node runtime, with a serialized input larger than the
// operating-system pipe buffer but below the adapter's 1 MiB input limit. It
// prints exactly ALLOW or REFUSE.
const adapterDriver = `import { pathToFileURL } from "node:url";
const workspace = process.env.PROBE_WORKSPACE;
const module = await import(pathToFileURL(workspace + "/plugins/agent-marketplace.js").href);
const hooks = await module.AgentMarketplace({ directory: workspace });
const filler = "x".repeat(Number(process.env.PROBE_FILLER_BYTES));
const input = { sessionID: "synthetic-session", callID: "synthetic-call", tool: "bash" };
const output = { args: { command: "true", payload: filler } };
try {
  await hooks["tool.execute.before"](input, output);
  console.log("ALLOW");
} catch {
  console.log("REFUSE");
}
`

// spec: opencode-hooks/a-successful-handler-ignores-a-large-valid-input
func TestAdapterAllowsSuccessfulHandlerThatDeclinesStdin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("adapter runtime harness requires node; the pinned native gate covers the same contract end to end")
	}
	for _, tc := range []struct {
		name    string
		command string
		allow   bool
	}{
		// The handler exits successfully without reading any of the large
		// input: closing the pipe must not turn that success into a refusal.
		{"successful handler ignores input", `printf '{}'`, true},
		// A handler that consumes its input keeps the same allow semantics.
		{"successful handler consumes input", `cat > /dev/null; printf '{}'`, true},
		// Nonzero exit, blocking output, and malformed output still refuse.
		{"nonzero exit", `printf '{}' >&2; exit 3`, false},
		{"blocking output", `cat > /dev/null; printf '%s' '{"decision":"block"}'`, false},
		{"malformed output", `cat > /dev/null; printf malformed`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			hook := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", tc.command, "command", nil, ir.SourceRef{})
			artifacts, diagnostics, err := (compiler.Compiler{}).Translate(hook)
			if err != nil || len(diagnostics) != 0 || len(artifacts) != 2 {
				t.Fatalf("compile hook: %v %v %v", artifacts, diagnostics, err)
			}
			for _, artifact := range artifacts {
				destination := filepath.Join(workspace, filepath.FromSlash(artifact.Path))
				if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(destination, artifact.Bytes, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			driver := filepath.Join(root, "driver.mjs")
			if err := os.WriteFile(driver, []byte(adapterDriver), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, driver)
			cmd.Env = append(os.Environ(), "PROBE_WORKSPACE="+workspace, "PROBE_FILLER_BYTES=262144")
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("adapter runtime exceeded its bound: %v", ctx.Err())
			}
			if err != nil {
				t.Fatalf("adapter runtime: %v: %s", err, strings.TrimSpace(string(output)))
			}
			switch got := strings.TrimSpace(string(output)); got {
			case "ALLOW":
				if !tc.allow {
					t.Fatal("handler failure was treated as permission")
				}
			case "REFUSE":
				if tc.allow {
					t.Fatalf("a successful handler that declined stdin refused the effect: %s", tc.name)
				}
			default:
				t.Fatalf("unexpected adapter runtime output %q", got)
			}
		})
	}
}
