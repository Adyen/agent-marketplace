package opencodeprobe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const nativeVersion = "1.18.34"
const nativeARM64Digest = "415487fcd9b3f06addd6877435783f3d5a236c029066b0dd6597d64980b15351"

// This fixture tests the prerequisite for a command adapter, not the complete
// proposed handler protocol. A callback that never activates cannot enforce it.
const pluginFixture = `export const Probe = async ({ directory }) => {
  await Bun.write(directory + "/initialized", "yes");
  if (Bun.env.PROBE_MODE === "init-error") throw new Error("synthetic initialization failure");
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool !== "bash") return;
      await Bun.write(directory + "/hook-input.json", JSON.stringify({ input, output }));
      if (Bun.env.PROBE_MODE === "block") throw new Error("synthetic blocking decision");
    }
  };
};
`

func TestNativeActivationControls(t *testing.T) {
	binary := requireNative(t)
	for _, mode := range []string{"allow", "block"} {
		t.Run(mode, func(t *testing.T) {
			result := runNative(t, binary, mode)
			if !result.initialized || !result.called {
				t.Fatal("active callback was not reached")
			}
			if result.effect != (mode == "allow") {
				t.Fatalf("protected effect = %v for %s callback", result.effect, mode)
			}
		})
	}
}

// spec: opencode-hooks/activation-failure-cannot-become-successful-enforcement
func TestNativeRequiredActivationGate(t *testing.T) {
	binary := requireNative(t)
	// Each case first proves the same tool can run and the active callback can
	// block it, so an unrelated setup failure cannot certify enforcement.
	for _, mode := range []string{"absent", "disabled", "init-error"} {
		t.Run(mode, func(t *testing.T) {
			allow := runNative(t, binary, "allow")
			block := runNative(t, binary, "block")
			if !allow.effect || !allow.called || block.effect || !block.called {
				t.Fatal("allow/block controls did not establish tool reachability")
			}
			result := runNative(t, binary, mode)
			if result.called {
				t.Fatal("inactive callback unexpectedly ran")
			}
			if result.initialized != (mode == "init-error") {
				t.Fatal("initialization control did not reach the intended failure")
			}
			if !result.effect {
				t.Error("native inactive-plugin behavior changed; revise the compatibility matrix")
			}
		})
	}
}

func requireNative(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("OPENCODE_PROBE_BINARY")
	if binary == "" {
		if os.Getenv("OPENCODE_PROBE_REQUIRE") == "1" {
			t.Fatal("required pinned native executable is missing")
		}
		t.Skip("dedicated offline native gate; run make opencode-probe")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		t.Fatal("initial feasibility gate requires Linux arm64")
	}
	var err error
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal("cannot resolve the pinned native executable")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			t.Fatal("native probe refuses an active external network interface")
		}
	}
	f, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != nativeARM64Digest {
		t.Fatal("native executable does not match the pinned release identity")
	}
	t.Logf("native-version=%s native-sha256=%s", nativeVersion, nativeARM64Digest)
	return binary
}

type nativeResult struct {
	initialized bool
	called      bool
	effect      bool
	input       []byte
	handwritten bool
	output      string
	runs        string
}

func runNative(t *testing.T, binary, mode string) nativeResult {
	return runNativeArtifacts(t, binary, mode, nil)
}

func runNativeArtifacts(t *testing.T, binary, mode string, artifacts map[string][]byte, setup ...func(workspace, root string)) nativeResult {
	return runNativeCommand(t, binary, mode, nil, artifacts, setup...)
}

func runNativeCommand(t *testing.T, binary, mode string, arguments []string, artifacts map[string][]byte, setup ...func(workspace, root string)) nativeResult {
	t.Helper()
	return runNativeCommandIn(t, binary, mode, "", arguments, artifacts, setup...)
}

// runNativeCommandIn starts OpenCode in subdir of the workspace. Protected
// effects land in that instance directory; hook input is read from the
// workspace, where the fixtures' handlers write it.
func runNativeCommandIn(t *testing.T, binary, mode, subdir string, arguments []string, artifacts map[string][]byte, setup ...func(workspace, root string)) nativeResult {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	config := filepath.Join(root, "config", "opencode")
	for _, dir := range []string{workspace, config, filepath.Join(workspace, ".opencode"), filepath.Join(root, "home"), filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// The pinned loader checks node_modules plus declared/locked dependencies
	// before installing. The dependency-free callback imports no package.
	dependencyDirs := []string{config, filepath.Join(workspace, ".opencode")}
	const manifest = `{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}`
	const lock = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}}}`
	for _, dir := range dependencyDirs {
		writeFixture(t, filepath.Join(dir, "package.json"), manifest)
		writeFixture(t, filepath.Join(dir, "package-lock.json"), lock)
		if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tool, toolArguments := "bash", `{"command":"printf protected > effect.txt","description":"Write synthetic protected sentinel"}`
	if mode == "todowrite" {
		tool, toolArguments = "todowrite", `{"todos":[{"content":"Synthetic task","status":"pending","priority":"medium"}]}`
	}
	model := newSyntheticModelForTool(t, tool, toolArguments)
	configValue := map[string]any{
		"model": "probe/model", "small_model": "probe/model", "share": "disabled",
		"enabled_providers": []string{"probe"},
		"permission":        map[string]string{"*": "deny", "bash": "allow"},
		"provider": map[string]any{"probe": map[string]any{
			"npm":     "@ai-sdk/openai-compatible",
			"options": map[string]any{"baseURL": model.URL + "/v1", "apiKey": "synthetic-probe-key"},
			"models":  map[string]any{"model": map[string]any{"name": "Synthetic probe", "limit": map[string]int{"context": 8192, "output": 1024}}},
		}},
	}
	if mode == "todowrite" {
		configValue["permission"] = map[string]string{"*": "deny", "todowrite": "allow"}
	}
	if mode == "permission-deny" {
		configValue["permission"] = map[string]string{"*": "deny"}
	}
	if artifacts != nil {
		for file, data := range artifacts {
			writeFixture(t, filepath.Join(workspace, ".opencode", filepath.FromSlash(file)), string(data))
		}
	} else if mode != "absent" {
		plugin := filepath.Join(workspace, ".opencode", "plugins", "probe.js")
		writeFixture(t, plugin, pluginFixture)
	}
	data, err := json.Marshal(configValue)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(workspace, "opencode.json"), string(data))
	writeFixture(t, filepath.Join(root, "models.json"), `{}`)
	env := []string{
		"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"TMPDIR=" + filepath.Join(root, "tmp"), "LANG=C.UTF-8", "TERM=dumb",
		"OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_MODELS_PATH=" + filepath.Join(root, "models.json"),
		"OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER=1", "PROBE_MODE=" + mode,
		"PROBE_NATIVE_REF=synthetic-native-value",
	}
	if mode == "disabled" {
		env = append(env, "OPENCODE_PURE=1")
	}
	for _, prepare := range setup {
		prepare(workspace, root)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	modelTurn := arguments == nil
	if modelTurn {
		arguments = []string{"run", "--model", "probe/model", "--format", "json", "Run the supplied synthetic tool operation once."}
	}
	instance := filepath.Join(workspace, filepath.FromSlash(subdir))
	if err := os.MkdirAll(instance, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, arguments...)
	cmd.Dir, cmd.Env = instance, env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var output boundedOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err = cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // Clean any attempt-owned descendants.
	}
	if ctx.Err() != nil || output.overflow {
		t.Fatal("native attempt exceeded its time or output bound")
	}
	if err != nil {
		t.Fatalf("native attempt failed: %v (raw native output withheld)", err)
	}
	if modelTurn && model.calls() == 0 {
		t.Fatal("synthetic model was not reached (raw native output withheld)")
	}
	if modelTurn && model.offeredTools() == 0 && mode != "permission-deny" {
		t.Fatal("native attempt was not offered the protected bash operation; enforcement is unproven")
	}
	current, readErr := os.ReadFile(filepath.Join(workspace, "opencode.json"))
	var actualConfig map[string]any
	if readErr != nil || json.Unmarshal(current, &actualConfig) != nil {
		t.Fatal("native configuration became unreadable")
	}
	for key, expected := range configValue {
		want, _ := json.Marshal(expected)
		got, _ := json.Marshal(actualConfig[key])
		if !bytes.Equal(want, got) {
			t.Fatalf("native attempt changed configuration field %s", key)
		}
	}
	for _, dir := range dependencyDirs {
		for name, want := range map[string]string{"package.json": manifest, "package-lock.json": lock} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(data) != want {
				t.Fatal("native dependency preflight changed a synthetic manifest")
			}
		}
		entries, err := os.ReadDir(filepath.Join(dir, "node_modules"))
		if err != nil || len(entries) != 0 {
			t.Fatal("native dependency preflight populated node_modules")
		}
	}
	result := nativeResult{
		output:      output.String(),
		initialized: fixtureExists(t, filepath.Join(workspace, "initialized")),
		handwritten: fixtureExists(t, filepath.Join(workspace, "handwritten-active")),
		called:      fixtureExists(t, filepath.Join(workspace, "hook-input.json")),
		effect:      fixtureExists(t, filepath.Join(instance, "effect.txt")),
	}
	if result.called {
		result.input, err = os.ReadFile(filepath.Join(workspace, "hook-input.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if fixtureExists(t, filepath.Join(workspace, "hook-runs.txt")) {
		data, err := os.ReadFile(filepath.Join(workspace, "hook-runs.txt"))
		if err != nil {
			t.Fatal(err)
		}
		result.runs = string(data)
	}
	if result.effect {
		data, err := os.ReadFile(filepath.Join(instance, "effect.txt"))
		if err != nil || string(data) != "protected" {
			t.Fatal("protected effect did not contain the synthetic tool payload")
		}
	}
	t.Logf("mode=%s initialized=%v callback=%v protected-effect=%v", mode, result.initialized, result.called, result.effect)
	return result
}

func writeFixture(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureExists(t *testing.T, name string) bool {
	t.Helper()
	_, err := os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

type boundedOutput struct {
	mu sync.Mutex
	bytes.Buffer
	overflow bool
}

func (o *boundedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.Len()+len(p) > 1<<20 {
		o.overflow = true
		return 0, io.ErrShortWrite
	}
	return o.Buffer.Write(p)
}

type syntheticModel struct {
	*httptest.Server
	mu       sync.Mutex
	requests int
	tools    int
}

func newSyntheticModel(t *testing.T) *syntheticModel {
	return newSyntheticModelForTool(t, "bash", `{"command":"printf protected > effect.txt","description":"Write synthetic protected sentinel"}`)
}

func newSyntheticModelForTool(t *testing.T, toolName, arguments string) *syntheticModel {
	t.Helper()
	m := new(syntheticModel)
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer synthetic-probe-key" {
			http.Error(w, "unexpected synthetic request", http.StatusBadRequest)
			return
		}
		var body struct {
			Messages []struct{ Role string }
			Tools    []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		callTool := false
		for _, tool := range body.Tools {
			callTool = callTool || tool.Function.Name == toolName
		}
		for _, message := range body.Messages {
			if message.Role == "tool" {
				callTool = false
			}
		}
		m.mu.Lock()
		m.requests++
		if callTool {
			m.tools++
		}
		m.mu.Unlock()
		delta := map[string]any{"role": "assistant", "content": "Synthetic probe complete."}
		finish := "stop"
		if callTool {
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"index": 0, "id": "synthetic-call", "type": "function",
				"function": map[string]string{"name": toolName, "arguments": arguments},
			}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, err := json.Marshal(map[string]any{"id": "probe", "object": "chat.completion.chunk", "created": 1, "model": "model", "choices": []any{choice}})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return // Native process cancellation may close the connection.
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *syntheticModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

func (m *syntheticModel) offeredTools() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tools
}

func TestSyntheticModelToolEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"offered bash", `{"tools":[{"function":{"name":"bash"}}],"messages":[{"role":"user"}]}`, 1},
		{"no bash", `{"tools":[],"messages":[{"role":"user"}]}`, 0},
		{"after tool result", `{"tools":[{"function":{"name":"bash"}}],"messages":[{"role":"tool"}]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newSyntheticModel(t)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, model.URL+"/v1/chat/completions", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer synthetic-probe-key")
			client := model.Client()
			client.Timeout = 5 * time.Second
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatal("synthetic model response failed")
			}
			if model.calls() != 1 || model.offeredTools() != tc.want || strings.Contains(string(data), "tool_calls") != (tc.want == 1) {
				t.Fatal("tool evidence does not distinguish an offered operation from an ordinary completion")
			}
		})
	}
}

func TestBoundedOutput(t *testing.T) {
	var output boundedOutput
	if _, err := output.Write([]byte(strings.Repeat("x", 1<<20))); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("x")); !errors.Is(err, io.ErrShortWrite) || !output.overflow {
		t.Fatal("excess native output must refuse")
	}
}
