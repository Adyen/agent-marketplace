package harnesscompile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func sourceDefinitionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestSourceDefinitionDigestTracksIngestReads(t *testing.T) {
	cases := []struct {
		name  string
		from  Harness
		path  string
		bodyA string
		bodyB string
	}{
		{"claude settings", HarnessClaude, ".claude/settings.json",
			`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo def-A"}]}]}}`,
			`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo def-B"}]}]}}`},
		{"factory standalone", HarnessFactory, ".factory/hooks.json",
			`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo def-A"}]}]}`,
			`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo def-B"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := &fstest.MapFile{Data: []byte(tc.bodyA)}
			src := fstest.MapFS{tc.path: file}

			oldHook := ingestedOneHook(t, src, tc.from)
			if oldHook.Command != "echo def-A" {
				t.Fatalf("first ingest command = %q, want echo def-A", oldHook.Command)
			}
			if oldHook.Source.File != tc.path {
				t.Fatalf("first ingest Source.File = %q, want %q", oldHook.Source.File, tc.path)
			}
			digestA := sourceDefinitionDigest([]byte(tc.bodyA))
			if got := oldHook.SourceDefinitionDigest(); got != digestA {
				t.Fatalf("first ingest digest = %q, want %q", got, digestA)
			}

			file.Data = []byte(tc.bodyB)
			if oldHook.Command != "echo def-A" {
				t.Fatalf("post-mutation old command = %q, want echo def-A", oldHook.Command)
			}
			if got := oldHook.SourceDefinitionDigest(); got != digestA {
				t.Fatalf("post-mutation old digest = %q, want %q", got, digestA)
			}

			fresh := ingestedOneHook(t, src, tc.from)
			digestB := sourceDefinitionDigest([]byte(tc.bodyB))
			if fresh.Command != "echo def-B" {
				t.Fatalf("fresh ingest command = %q, want echo def-B", fresh.Command)
			}
			if got := fresh.SourceDefinitionDigest(); got != digestB {
				t.Fatalf("fresh ingest digest = %q, want %q", got, digestB)
			}
			if digestA == digestB {
				t.Fatalf("test requires distinct digests for distinct definitions")
			}
		})
	}
}

type scriptedReadFS struct {
	fs.FS
	path    string
	bodies  [][]byte
	readErr map[int]error
	reads   int
}

func (s *scriptedReadFS) ReadFile(name string) ([]byte, error) {
	if name != s.path {
		return fs.ReadFile(s.FS, name)
	}
	idx := s.reads
	s.reads++
	if err := s.readErr[idx]; err != nil {
		return nil, err
	}
	if idx < len(s.bodies) {
		return s.bodies[idx], nil
	}
	return s.bodies[len(s.bodies)-1], nil
}

func TestIngestSourceDefinitionUnstableReads(t *testing.T) {
	const path = ".factory/hooks.json"
	bodyA := []byte(`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo def-A"}]}]}`)
	bodyB := []byte(`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo def-B"}]}]}`)

	unstable := &scriptedReadFS{
		FS:     fstest.MapFS{path: {Data: bodyA}},
		path:   path,
		bodies: [][]byte{bodyA, bodyB},
	}
	b, err := IngestBundleWithLoader(unstable, readTwiceThenLoadFactory(path))
	if err != nil {
		t.Fatalf("IngestBundleWithLoader: %v", err)
	}
	if unstable.reads < 2 {
		t.Fatalf("factory ingest read %s %d times; the instability proof needs >= 2", path, unstable.reads)
	}
	var hooks []*ir.Hook
	for _, r := range b.Resources {
		if h, ok := r.(*ir.Hook); ok {
			hooks = append(hooks, h)
		}
	}
	if len(hooks) != 1 {
		t.Fatalf("want exactly one hook, got %d", len(hooks))
	}
	if hooks[0].Command != "echo def-B" {
		t.Fatalf("unstable ingest command = %q, want echo def-B from the final read", hooks[0].Command)
	}
	if got := hooks[0].SourceDefinitionDigest(); got != "" {
		t.Fatalf("unstable digest = %q, want empty (never attach unstable reads)", got)
	}

	stable := &scriptedReadFS{
		FS:     fstest.MapFS{path: {Data: bodyA}},
		path:   path,
		bodies: [][]byte{bodyA},
	}
	stableBundle, err := IngestBundleWithLoader(stable, readTwiceThenLoadFactory(path))
	if err != nil {
		t.Fatalf("IngestBundleWithLoader: %v", err)
	}
	if stable.reads < 2 {
		t.Fatalf("factory ingest read %s %d times; want >= 2", path, stable.reads)
	}
	var stableHooks []*ir.Hook
	for _, r := range stableBundle.Resources {
		if h, ok := r.(*ir.Hook); ok {
			stableHooks = append(stableHooks, h)
		}
	}
	if len(stableHooks) != 1 || stableHooks[0].SourceDefinitionDigest() != sourceDefinitionDigest(bodyA) {
		t.Fatalf("stable hooks = %#v, want one hook with digest %q", stableHooks, sourceDefinitionDigest(bodyA))
	}
}

// readTwiceThenLoadFactory reads path once before the factory loader reads it
// again, standing in for any second reader of the same definition file.
func readTwiceThenLoadFactory(path string) func(fs.FS) (*ingest.Bundle, error) {
	return func(src fs.FS) (*ingest.Bundle, error) {
		if _, err := fs.ReadFile(src, path); err != nil {
			return nil, err
		}
		factory, ok := ingest.For(ingest.HarnessFactory)
		if !ok {
			return nil, errors.New("factory ingester is not registered")
		}
		return factory.LoadBundle(src)
	}
}

func TestIngestSourceDefinitionReadError(t *testing.T) {
	const path = ".factory/hooks.json"
	bodyA := []byte(`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo def-A"}]}]}`)
	errRead := errors.New("scripted read failure")

	failing := &scriptedReadFS{
		FS:      fstest.MapFS{path: {Data: bodyA}},
		path:    path,
		bodies:  [][]byte{bodyA},
		readErr: map[int]error{0: errRead},
	}
	bundle, err := IngestBundle(failing, HarnessFactory)
	if err == nil {
		t.Fatalf("expected ingest failure from delegated read error, got bundle %+v", bundle)
	}
	if !errors.Is(err, errRead) {
		t.Fatalf("ingest error = %v, want it to wrap the delegated read error", err)
	}
}

func TestSourceDefinitionDigestNotSerialized(t *testing.T) {
	hook := ir.NewHook("pretooluse-command", "PreToolUse", "echo def-A", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	if got := hook.SourceDefinitionDigest(); got != "" {
		t.Fatalf("new hook digest = %q, want empty", got)
	}
	before, err := json.Marshal(hook)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	hook.SetSourceDefinitionDigest(sourceDefinitionDigest([]byte("definition")))
	if got := hook.SourceDefinitionDigest(); got == "" {
		t.Fatalf("digest after set is empty")
	}
	after, err := json.Marshal(hook)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("json.Marshal changed after digest assignment\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestSourceDefinitionDigestCanonicalPayload(t *testing.T) {
	hook := ir.NewHook("pretooluse-command", "PreToolUse", "echo def-A", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})

	before, err := artifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR before: %v", err)
	}
	hook.SetSourceDefinitionDigest(sourceDefinitionDigest([]byte("definition")))
	after, err := artifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR after: %v", err)
	}
	if !bytes.Equal(before.Payload, after.Payload) {
		t.Fatalf("canonical payload changed after digest assignment\nbefore: %s\nafter:  %s", before.Payload, after.Payload)
	}

	decoded, err := artifact.DecodeResource(after.Manifest.Kind, after.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	decodedHook, ok := decoded.(*ir.Hook)
	if !ok {
		t.Fatalf("decoded resource is %T, want *ir.Hook", decoded)
	}
	if got := decodedHook.SourceDefinitionDigest(); got != "" {
		t.Fatalf("decoded digest = %q, want empty (digest is not persisted)", got)
	}
}
