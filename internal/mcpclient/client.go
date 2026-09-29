package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Adyen/agent-marketplace/internal/codemode"
)

const protocolVersion = "2025-06-18"

var requestID int64

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type Client interface {
	Initialize(ctx context.Context) error

	ListTools(ctx context.Context) ([]codemode.ToolSchema, error)

	Call(ctx context.Context, name string, args json.RawMessage) (any, error)

	Close() error
}

type stdioClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  *stderrCapture
	secrets []string
	scanner *bufio.Scanner
	sem     chan struct{}
	dead    atomic.Bool
	inited  atomic.Bool
}

func Spawn(ctx context.Context, def *codemode.ServerDef) (*stdioClient, error) {
	if def == nil || def.Command == "" {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "server definition has no command")
	}
	cmd := exec.Command(def.Command, def.Args...)
	cmd.Env = declaredEnv(def.Env)
	stderr := newStderrCapture()
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "stdout pipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "start %q: %v", def.Command, scrubErr(err))
	}
	c := &stdioClient{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderr,
		secrets: envSecrets(def.Env),
		scanner: bufio.NewScanner(stdout),
		sem:     make(chan struct{}, 1),
	}
	c.scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	return c, nil
}

func declaredEnv(declared map[string]string) []string {
	var env []string
	keys := make([]string, 0, len(declared))
	for k := range declared {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := declared[k]
		if v == "" {
			if host, ok := os.LookupEnv(k); ok {
				v = host
			}
		}
		env = append(env, k+"="+v)
	}
	return env
}

func envSecrets(declared map[string]string) []string {
	var out []string
	for _, kv := range declaredEnv(declared) {
		if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] != "" {
			out = append(out, kv[i+1:])
		}
	}
	return out
}

type stderrCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newStderrCapture() *stderrCapture { return &stderrCapture{} }

func (s *stderrCapture) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf.Len() < 2048 {
		remain := 2048 - s.buf.Len()
		if len(p) > remain {
			s.buf.Write(p[:remain])
		} else {
			s.buf.Write(p)
		}
	}
	return len(p), nil
}

func (s *stderrCapture) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (c *stdioClient) redactedStderr() string {
	s := c.stderr.String()
	for _, secret := range c.secrets {
		s = strings.ReplaceAll(s, secret, "<redacted>")
	}
	return s
}

func (c *stdioClient) kill() {
	c.dead.Store(true)
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *stdioClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.dead.Load() {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "server exited and is not restarted")
	}
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, codemode.CodeError(codemode.ErrCodeTimeout, "server %s exceeded deadline: %v", method, ctx.Err())
	}
	if c.dead.Load() {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "server exited and is not restarted")
	}
	id := atomic.AddInt64(&requestID, 1)
	raw, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	writeCh := make(chan error, 1)
	go func() {
		_, err := c.stdin.Write(append(raw, '\n'))
		writeCh <- err
	}()
	select {
	case err := <-writeCh:
		if err != nil {
			c.kill()
			return nil, codemode.CodeError(codemode.ErrCodeToolError, "write to server: %v", scrubErr(err))
		}
	case <-ctx.Done():
		c.kill()
		return nil, codemode.CodeError(codemode.ErrCodeTimeout, "server %s exceeded deadline: %v", method, ctx.Err())
	}
	type reply struct {
		msg *rpcReply
		err error
	}
	ch := make(chan reply, 1)
	go func() {
		for c.scanner.Scan() {
			line := c.scanner.Bytes()
			var msg rpcReply
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			if msg.ID != id {
				continue
			}
			ch <- reply{msg: &msg}
			return
		}
		ch <- reply{err: io.EOF}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			c.kill()
			if !c.inited.Load() {
				return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "server exited during initialize: %s", c.redactedStderr())
			}
			return nil, codemode.CodeError(codemode.ErrCodeToolError, "server closed during %s", method)
		}
		if r.msg.Error != nil {
			return nil, codemode.CodeError(codemode.ErrCodeToolError, "%s failed: %s", method, r.msg.Error.Message)
		}
		return r.msg.Result, nil
	case <-ctx.Done():
		c.kill()
		return nil, codemode.CodeError(codemode.ErrCodeTimeout, "server %s exceeded deadline: %v", method, ctx.Err())
	}
}

func (c *stdioClient) Initialize(ctx context.Context) error {
	if c.inited.Load() {
		return nil
	}
	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-marketplace", "version": "code-mode"},
	})
	if err != nil {
		return err
	}

	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	_, _ = c.stdin.Write(append(raw, '\n'))
	c.inited.Store(true)
	return nil
}

func (c *stdioClient) ListTools(ctx context.Context) ([]codemode.ToolSchema, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	res, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []codemode.ToolSchema `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "malformed tools/list: %v", err)
	}
	return out.Tools, nil
}

func (c *stdioClient) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	params := map[string]any{"name": name}
	if len(args) > 0 {
		var parsed any
		if err := json.Unmarshal(args, &parsed); err == nil {
			params["arguments"] = parsed
		}
	}
	res, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "malformed tools/call result: %v", err)
	}
	if out.IsError {
		var parts []string
		for _, ct := range out.Content {
			parts = append(parts, ct.Text)
		}
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "%s: %s", name, strings.Join(parts, " "))
	}
	var texts []string
	for _, ct := range out.Content {
		if ct.Type == "text" {
			texts = append(texts, ct.Text)
		}
	}
	return map[string]any{"content": out.Content, "text": strings.Join(texts, "\n")}, nil
}

func (c *stdioClient) Close() error {
	c.dead.Store(true)
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

type httpClient struct {
	url       string
	headers   map[string]string
	hc        *http.Client
	sessionID string
	inited    atomic.Bool
}

func NewHTTP(def *codemode.ServerDef) (*httpClient, error) {
	if def == nil || def.URL == "" {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "server definition has no url")
	}
	u, err := url.Parse(def.URL)
	if err != nil || u.Hostname() == "" {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "invalid remote MCP endpoint url")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
	default:
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "remote MCP endpoint must be https or loopback")
	}
	return &httpClient{url: def.URL, headers: def.Headers, hc: &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirects are not followed")
		},
	}}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (c *httpClient) post(ctx context.Context, payload any, out any) error {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(raw))
	if err != nil {
		return codemode.CodeError(codemode.ErrCodeToolError, "build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.sessionID != "" {
		req.Header.Set("mcp-session-id", c.sessionID)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return codemode.CodeError(codemode.ErrCodeToolError, "request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		c.sessionID = sid
	}
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return codemode.CodeError(codemode.ErrCodeToolError, "read response: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codemode.CodeError(codemode.ErrCodeToolError, "http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}

	data := extractSSE(string(body))
	if out == nil || data == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), out)
}

func extractSSE(body string) string {
	var last string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data:") {
			last = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if last != "" {
		return last
	}
	return strings.TrimSpace(body)
}

func (c *httpClient) Initialize(ctx context.Context) error {
	if c.inited.Load() {
		return nil
	}
	var reply rpcReply
	err := c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: atomic.AddInt64(&requestID, 1), Method: "initialize", Params: map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-marketplace", "version": "code-mode"},
	}}, &reply)
	if err != nil {
		return err
	}
	_ = c.post(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, nil)
	c.inited.Store(true)
	return nil
}

func (c *httpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var reply rpcReply
	err := c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: atomic.AddInt64(&requestID, 1), Method: method, Params: params}, &reply)
	if err != nil {
		return nil, err
	}
	if reply.Error != nil {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "%s failed: %s", method, reply.Error.Message)
	}
	return reply.Result, nil
}

func (c *httpClient) ListTools(ctx context.Context) ([]codemode.ToolSchema, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	res, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []codemode.ToolSchema `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "malformed tools/list: %v", err)
	}
	return out.Tools, nil
}

func (c *httpClient) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	params := map[string]any{"name": name}
	if len(args) > 0 {
		var parsed any
		if err := json.Unmarshal(args, &parsed); err == nil {
			params["arguments"] = parsed
		}
	}
	res, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		IsError bool            `json:"isError"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "malformed tools/call result: %v", err)
	}
	if out.IsError {
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "%s returned an error", name)
	}
	var generic any
	_ = json.Unmarshal(res, &generic)
	return generic, nil
}

func (c *httpClient) Close() error { return nil }

func scrubErr(err error) string {
	var ee *exec.Error
	if errors.As(err, &ee) {
		return ee.Err.Error() + " (" + ee.Name + ")"
	}
	return err.Error()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
