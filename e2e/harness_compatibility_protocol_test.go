package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runCompatibilityProtocol(e *Env, binary, protocol string, timeout time.Duration) compatibilityCommand {
	r := compatibilityCommand{Args: []string{binary, "app-server"}, ExitCode: -1}
	method := map[string]string{"codex-skills": "skills/list", "codex-hooks": "hooks/list", "claude-initialize": "initialize", "factory-skills": "droid.list_skills", "factory-commands": "droid.list_commands", "factory-droids": "droid.get_context_breakdown"}[protocol]
	if protocol == "claude-initialize" {
		r.Args = append([]string{binary}, "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--setting-sources", "user,project,local")
	}
	if strings.HasPrefix(protocol, "factory-") {
		r.Args = append([]string{binary}, "exec", "--input-format", "stream-jsonrpc", "--output-format", "stream-jsonrpc")
	}
	if method == "" {
		r.Error = "unsupported native inspection protocol"
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := compatibilityProcess(ctx, e, binary, r.Args[1:])
	if err != nil {
		r.Error = err.Error()
		return r
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		r.Error = "cannot open native protocol input"
		return r
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		r.Error = "cannot open native protocol output"
		return r
	}
	defer output.Close()
	var stdout, stderr compatibilityOutput
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		r.Error = "configured protocol executable could not be started"
		return r
	}
	reader := io.LimitReader(io.TeeReader(output, &stdout), compatibilityOutputLimit+1)
	encoder, decoder := json.NewEncoder(input), json.NewDecoder(reader)
	var result json.RawMessage
	var exchangeErr error
	switch {
	case protocol == "claude-initialize":
		result, exchangeErr = inspectClaudeMetadata(encoder, decoder)
	case strings.HasPrefix(protocol, "factory-"):
		result, exchangeErr = inspectFactoryMetadata(encoder, decoder, e.workspace, method)
	default:
		result, exchangeErr = inspectCodexMetadata(encoder, decoder, e.workspace, method)
	}
	_ = input.Close()
	if exchangeErr != nil {
		cancel()
	} else {
		_, _ = io.Copy(io.Discard, reader)
	}
	err = cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	r.ExitCode = cmd.ProcessState.ExitCode()
	r.output = string(result)
	r.Diagnostics = compatibilityDiagnostics(string(stderr.data))
	r.OutputBytes = stdout.total + stderr.total
	r.OutputTruncated = r.OutputBytes > compatibilityOutputLimit
	switch {
	case r.OutputTruncated:
		r.Error = "native protocol output exceeded bounded capture limit"
	case ctx.Err() == context.DeadlineExceeded:
		r.TimedOut = true
		r.Error = "native protocol inspection timed out"
	case exchangeErr != nil:
		r.Error = exchangeErr.Error()
	case err != nil:
		r.Error = "native protocol process did not exit successfully"
	default:
		r.Observed = []string{"protocol response: " + method}
	}
	return r
}

func inspectFactoryMetadata(encoder *json.Encoder, decoder *json.Decoder, workspace, method string) (json.RawMessage, error) {
	const initializeID = "00000000-0000-4000-8000-000000000001"
	const inspectID = "00000000-0000-4000-8000-000000000002"
	request := map[string]any{"jsonrpc": "2.0", "factoryApiVersion": "1.0.0", "factoryProtocolVersion": "1.192.0", "type": "request", "id": initializeID, "method": "droid.initialize_session", "params": map[string]any{"machineId": "amcompat-fixture", "cwd": workspace}}
	if err := encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("write native session initialization request")
	}
	initialized, err := readCompatibilityRPCResponse(decoder, initializeID)
	if err != nil {
		return nil, err
	}
	var session struct {
		ID string `json:"sessionId"`
	}
	if json.Unmarshal(initialized, &session) != nil || session.ID == "" {
		return nil, fmt.Errorf("native session initialization omitted session identity")
	}
	request["id"], request["method"], request["params"] = inspectID, method, map[string]any{}
	if err := encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("write native resource inspection request")
	}
	return readCompatibilityRPCResponse(decoder, inspectID)
}

func inspectClaudeMetadata(encoder *json.Encoder, decoder *json.Decoder) (json.RawMessage, error) {
	const id = "amcompat-initialize"
	if err := encoder.Encode(map[string]any{"type": "control_request", "request_id": id, "request": map[string]string{"subtype": "initialize"}}); err != nil {
		return nil, fmt.Errorf("write native initialization request")
	}
	for {
		var message struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string          `json:"subtype"`
				RequestID string          `json:"request_id"`
				Response  json.RawMessage `json:"response"`
				Error     json.RawMessage `json:"error"`
			} `json:"response"`
		}
		if err := decoder.Decode(&message); err != nil {
			return nil, fmt.Errorf("native control protocol ended without initialization metadata")
		}
		if message.Type == "system" {
			continue
		}
		if message.Type != "control_response" || message.Response.RequestID != id {
			return nil, fmt.Errorf("native control protocol returned an unexpected message or permission request")
		}
		if message.Response.Subtype != "success" {
			return nil, compatibilityProtocolRejection(message.Response.Error)
		}
		if len(message.Response.Response) == 0 || string(message.Response.Response) == "null" {
			return nil, fmt.Errorf("native control protocol omitted initialization metadata")
		}
		return message.Response.Response, nil
	}
}

func compatibilityProtocolRejection(raw json.RawMessage) error {
	codes := compatibilityDiagnostics(string(raw))
	if len(codes) == 0 {
		return fmt.Errorf("native protocol rejected the inspection request")
	}
	return fmt.Errorf("native protocol rejected the inspection request: %s", strings.Join(codes, ", "))
}

func inspectCodexMetadata(encoder *json.Encoder, decoder *json.Decoder, workspace, method string) (json.RawMessage, error) {
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "agent_marketplace_validation", "version": "1.0.0"}}}); err != nil {
		return nil, fmt.Errorf("write native initialization request")
	}
	if _, err := readCompatibilityRPCResponse(decoder, 1); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return nil, fmt.Errorf("write native initialization notification")
	}
	params := map[string]any{"cwds": []string{workspace}}
	if method == "skills/list" {
		params["forceReload"] = true
	}
	if err := encoder.Encode(map[string]any{"id": 2, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("write native metadata inspection request")
	}
	return readCompatibilityRPCResponse(decoder, 2)
}

func readCompatibilityRPCResponse(decoder *json.Decoder, id any) (json.RawMessage, error) {
	expected, err := json.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("invalid native inspection request identity")
	}
	for {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			return nil, fmt.Errorf("native protocol ended without a valid response")
		}
		if response.ID == nil && response.Method != "" {
			continue
		}
		if string(response.ID) != string(expected) || response.Method != "" {
			return nil, fmt.Errorf("native protocol returned an unexpected response or server request")
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			return nil, compatibilityProtocolRejection(response.Error)
		}
		if len(response.Result) == 0 || string(response.Result) == "null" {
			return nil, fmt.Errorf("native protocol omitted the inspection result")
		}
		return response.Result, nil
	}
}

func TestCompatibilityProtocolReadsFactoryResourcesWithoutUserMessages(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-droid")
	e.WriteFile(binary, `#!/bin/sh
IFS= read -r request || exit 70
case "$request" in *'"method":"droid.initialize_session"'*) ;; *) exit 71;; esac
case "$request" in *'"factoryApiVersion":"1.0.0"'*'"factoryProtocolVersion":"1.192.0"'*'"type":"request"'*) ;; *) exit 75;; esac
printf '%s\n' '{"jsonrpc":"2.0","type":"response","id":"00000000-0000-4000-8000-000000000001","result":{"sessionId":"fixture-session"}}'
IFS= read -r request || exit 72
case "$request" in *'"method":"droid.list_skills"'*) ;; *) exit 73;; esac
printf '%s\n' '{"jsonrpc":"2.0","type":"response","id":"00000000-0000-4000-8000-000000000002","result":{"skills":[{"name":"fixture-skill","description":"Fixture description","enabled":true,"filePath":"fixture-path","location":"project"}]}}'
if IFS= read -r request; then exit 74; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runCompatibilityProtocol(e, binary, "factory-skills", 2*time.Second)
	if err := requireCompatibilityObservation(&result, false, "fixture-skill"); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityProtocolTimeoutIsBounded(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-hanging-protocol")
	e.WriteFile(binary, "#!/bin/sh\nIFS= read -r request\nsleep 30\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result := runCompatibilityProtocol(e, binary, "codex-skills", 100*time.Millisecond)
	if !result.TimedOut || result.Error == "" || time.Since(started) > 3*time.Second {
		t.Fatalf("protocol timeout did not stop the process group promptly: %+v", result)
	}
}

func TestCompatibilityProtocolRejectsErrorsAndUnexpectedRequests(t *testing.T) {
	for _, raw := range []string{
		`{"id":1,"error":{"message":"authentication required PRIVATE-VALUE"}}`,
		`{"id":2,"result":{}}`,
		`{"id":1,"method":"request_permission","params":{}}`,
		`{"id":1,"result":null}`,
		`{"id":1}`,
		`{`,
	} {
		_, err := readCompatibilityRPCResponse(json.NewDecoder(strings.NewReader(raw)), 1)
		if err == nil || strings.Contains(err.Error(), "PRIVATE-VALUE") {
			t.Fatal("invalid protocol response was accepted or leaked raw error data")
		}
	}
	if _, err := readCompatibilityRPCResponse(json.NewDecoder(strings.NewReader(`{"id":1,"result":{}}`)), 1); err != nil {
		t.Fatal("valid response rejected")
	}
}

func TestCompatibilityProtocolInitializesClaudeWithoutAPrompt(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-claude")
	e.WriteFile(binary, `#!/bin/sh
IFS= read -r request || exit 70
case "$request" in *'"subtype":"initialize"'*) ;; *) exit 71;; esac
printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[{"name":"fixture-skill","description":"Fixture skill"}],"agents":[{"name":"fixture-agent","description":"Fixture agent"}]}}}'
if IFS= read -r request; then exit 72; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runCompatibilityProtocol(e, binary, "claude-initialize", 2*time.Second)
	if err := requireCompatibilityObservation(&result, false, "fixture-skill", "fixture-agent"); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityProtocolReadsHooksWithoutRunningThem(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-codex")
	e.WriteFile(binary, `#!/bin/sh
IFS= read -r request || exit 70
printf '%s\n' '{"id":1,"result":{}}'
IFS= read -r request || exit 71
IFS= read -r request || exit 72
case "$request" in *'"method":"hooks/list"'*) ;; *) exit 73;; esac
printf '%s\n' '{"id":2,"result":{"data":[{"cwd":"fixture-workspace","hooks":[{"eventName":"preToolUse","handlerType":"command","command":"fixture-hook","sourcePath":"fixture-hooks.json","enabled":false,"trustStatus":"untrusted"}],"errors":[],"warnings":[]}]}}'
if IFS= read -r request; then exit 74; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runCompatibilityProtocol(e, binary, "codex-hooks", 2*time.Second)
	if err := requireCompatibilityObservation(&result, false, "fixture-hook", "preToolUse"); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityProtocolReadsSkillsWithoutStartingATurn(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-codex")
	e.WriteFile(binary, `#!/bin/sh
[ "$*" = 'app-server' ] || exit 70
IFS= read -r request || exit 71
case "$request" in *'"method":"initialize"'*) ;; *) exit 72;; esac
printf '%s\n' '{"id":1,"result":{"userAgent":"internal-fake"}}'
IFS= read -r request || exit 73
[ "$request" = '{"method":"initialized"}' ] || exit 74
IFS= read -r request || exit 75
case "$request" in *'"method":"skills/list"'*) ;; *) exit 76;; esac
printf '%s\n' '{"method":"skills/changed","params":{}}' '{"id":2,"result":{"data":[{"cwd":"fixture-workspace","skills":[{"name":"fixture-skill","description":"Fixture description","path":"fixture-path","scope":"repo","enabled":true}],"errors":[]}]}}'
if IFS= read -r request; then exit 77; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runCompatibilityProtocol(e, binary, "codex-skills", 2*time.Second)
	if err := requireCompatibilityObservation(&result, false, "fixture-skill", "Fixture description"); err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.TimedOut {
		t.Fatalf("protocol did not close cleanly after metadata inspection: %+v", result)
	}
}
