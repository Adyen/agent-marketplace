package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSecretScanCoversPullRequestRange(t *testing.T) {
	doc := loadWorkflow(t, "trufflehog.yml")
	job := getMap(t, getMap(t, doc, "jobs"), "trufflehog")
	var guardEnv map[string]any
	for _, step := range getSlice(t, job, "steps") {
		m := stepMap(t, step)
		switch m["name"] {
		case "Checkout":
			if got := getMap(t, m, "with")["fetch-depth"]; got != 0 {
				t.Fatalf("checkout fetch-depth = %v, want 0", got)
			}
		case "Require a non-empty scan":
			guardEnv = getMap(t, m, "env")
		}
	}
	if guardEnv["BASE_SHA"] != "${{ github.event.merge_group.base_sha || github.event.pull_request.base.sha }}" ||
		guardEnv["HEAD_SHA"] != "${{ github.event.merge_group.head_sha || github.event.pull_request.head.sha }}" {
		t.Fatalf("scan guard range = %v", guardEnv)
	}
	capture := findStepRun(t, doc, "trufflehog", "Capture TruffleHog report")
	guard := findStepRun(t, doc, "trufflehog", "Require a non-empty scan")

	repo := t.TempDir()
	setupRepo := exec.Command("/bin/bash", "-c", `set -e; git init -q; git config user.name Test; git config user.email test@example.com; printf base > change; git add change; git commit -qm base; git rev-parse HEAD; printf head > change; git commit -qam head; git rev-parse HEAD`)
	setupRepo.Dir = repo
	revs, err := setupRepo.Output()
	if err != nil || len(strings.Fields(string(revs))) != 2 {
		t.Fatalf("create git range: %v\n%s", err, revs)
	}
	base, head := strings.Fields(string(revs))[0], strings.Fields(string(revs))[1]

	for _, tc := range []struct {
		name, base, head, report string
		pass                     bool
	}{
		{"non-empty zero", base, head, `finished scanning {"chunks": 0, "bytes": 0}`, false},
		{"non-empty scanned", base, head, `finished scanning {"chunks": 1, "bytes": 12}`, true},
		{"empty range", head, head, `finished scanning {"chunks": 0, "bytes": 0}`, true},
		{"non-empty missing report", base, head, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stub := filepath.Join(dir, "docker")
			if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\"\n[ -z \"$REPORT\" ] || printf '%s\\n' \"$REPORT\" >&2\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			githubPath := filepath.Join(dir, "github-path")
			setup := exec.Command("/bin/bash", "-c", capture)
			setup.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "RUNNER_TEMP=" + dir, "GITHUB_PATH=" + githubPath}
			if out, err := setup.CombinedOutput(); err != nil {
				t.Fatalf("capture step: %v\n%s", err, out)
			}
			path, err := os.ReadFile(githubPath)
			if err != nil {
				t.Fatal(err)
			}
			action := exec.Command("/bin/bash", "-c", "docker run x")
			action.Env = []string{"PATH=" + strings.TrimSpace(string(path)) + ":" + dir + ":/usr/bin:/bin", "REPORT=" + tc.report}
			var stdout, stderr bytes.Buffer
			action.Stdout, action.Stderr = &stdout, &stderr
			if err := action.Run(); err != nil {
				t.Fatalf("docker wrapper: %v", err)
			}
			if stdout.String() != "run x\n" {
				t.Fatalf("docker stdout = %q", stdout.String())
			}
			if tc.report != "" && !strings.Contains(stderr.String(), tc.report) {
				t.Fatalf("docker stderr = %q, want report %q", stderr.String(), tc.report)
			}
			check := exec.Command("/bin/bash", "-c", guard)
			check.Dir = repo
			check.Env = append(os.Environ(), "BASE_SHA="+tc.base, "HEAD_SHA="+tc.head, "RUNNER_TEMP="+dir)
			out, err := check.CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("guard error = %v, want pass %t; output: %s", err, tc.pass, out)
			}
			if !tc.pass && !strings.Contains(string(out), "::error::") {
				t.Fatalf("guard output = %q, want ::error::", out)
			}
		})
	}
}

// spec: marketplace-delivery-security/secret-scanner-failure-fails-the-job
func TestSecretScanFailsClosed(t *testing.T) {
	jobs := getMap(t, loadWorkflow(t, "trufflehog.yml"), "jobs")
	job := getMap(t, jobs, "trufflehog")
	if job["continue-on-error"] != nil && job["continue-on-error"] != false {
		t.Fatal("secret scan job must not tolerate failure")
	}
	found := false
	for _, step := range getSlice(t, job, "steps") {
		m := stepMap(t, step)
		if condition, ok := m["if"].(string); ok && strings.Contains(condition, "steps.trufflehog.outcome") {
			t.Errorf("step %v must not repair the scanner's failure status", m["name"])
		}
		if m["name"] != "TruffleHog OSS" {
			continue
		}
		if m["continue-on-error"] != nil && m["continue-on-error"] != false {
			t.Error("the scanner itself must fail closed")
		}
		if m["if"] != nil {
			t.Error("the scanner must run unconditionally")
		}
		if !strings.HasPrefix(m["uses"].(string), "trufflesecurity/trufflehog@") {
			t.Error("secret scanner action is missing")
		}
		found = true
	}
	if !found {
		t.Fatal("TruffleHog step missing")
	}
}

// spec: marketplace-delivery-security/web-runtime-is-independent-of-the-builder
// spec: marketplace-delivery-security/web-healthcheck-has-an-explicit-request-deadline
func TestWebRuntimeIsolation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "web", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, want := range []string{
		"ARG NODE_IMAGE=node:24-trixie-slim",
		"ARG NODE_RUNTIME_IMAGE=gcr.io/distroless/nodejs24-debian13:nonroot",
		"FROM ${NODE_IMAGE} AS build",
		"FROM ${NODE_RUNTIME_IMAGE} AS runtime",
		"USER 1000:1000",
		`ENTRYPOINT ["/nodejs/bin/node"]`,
		`CMD [".output/server/index.mjs"]`,
		`CMD ["/nodejs/bin/node", "-e",`,
		"signal: AbortSignal.timeout(4000)",
		".catch(() => process.exit(1))",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("web Dockerfile missing %q", want)
		}
	}
}

// spec: marketplace-delivery-security/helm-refuses-an-unspecified-image-version
// spec: marketplace-delivery-security/helm-digests-take-precedence-for-each-enabled-workload
func TestHelmImageSelection(t *testing.T) {
	if os.Getenv("AM_HELM_CHECK") != "1" {
		t.Skip("the Helm chart gate enables real chart rendering")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "agent-marketplace")
	digest := "sha256:" + strings.Repeat("a", 64)
	webDigest := "sha256:" + strings.Repeat("b", 64)
	docsDigest := "sha256:" + strings.Repeat("c", 64)
	for _, tc := range []struct {
		name   string
		values string
		set    []string
		want   []string
		fail   bool
	}{
		{name: "missing version", fail: true},
		{name: "explicit tag", set: []string{"image.tag=dev"}, want: []string{"agent-marketplace:dev"}},
		{name: "digest wins", set: []string{"image.tag=ignored", "image.digest=" + digest}, want: []string{"agent-marketplace@" + digest}},
		{name: "web needs its own version", set: []string{"image.tag=dev", "web.enabled=true"}, fail: true},
		{name: "docs needs its own version", set: []string{"image.tag=dev", "docs.enabled=true"}, fail: true},
		{name: "null web digest cannot inherit the backend digest", set: []string{"image.digest=" + digest, "web.enabled=true", "web.image.digest=null"}, fail: true},
		{name: "null docs digest cannot inherit the backend digest", set: []string{"image.digest=" + digest, "docs.enabled=true", "docs.image.digest=null"}, fail: true},
		{
			name: "split digest inheritance and frontend isolation",
			set: []string{
				"mode=split", "image.tag=ignored", "image.digest=" + digest,
				"api.image.repository=example.invalid/api",
				"web.enabled=true", "web.image.digest=" + webDigest,
				"docs.enabled=true", "docs.image.digest=" + docsDigest,
			},
			want: []string{"example.invalid/api@" + digest, "agent-marketplace@" + digest, "agent-marketplace-web@" + webDigest, "agent-marketplace-docs@" + docsDigest},
		},
		{
			name: "split role overrides",
			set:  []string{"mode=split", "api.image.tag=api-dev", "worker.image.digest=" + digest},
			want: []string{"agent-marketplace:api-dev", "agent-marketplace@" + digest},
		},
		{name: "previous values require explicit versions", values: "helm-pre-hardening-values.yaml", fail: true},
		{name: "previous values need a web version", values: "helm-pre-hardening-values.yaml", set: []string{"image.digest=" + digest}, fail: true},
		{name: "previous values need a docs version", values: "helm-pre-hardening-values.yaml", set: []string{"image.digest=" + digest, "web.image.digest=" + webDigest}, fail: true},
		{
			name:   "previous values upgraded with explicit digests",
			values: "helm-pre-hardening-values.yaml",
			set:    []string{"image.digest=" + digest, "web.image.digest=" + webDigest, "docs.image.digest=" + docsDigest},
			want:   []string{"example.invalid/marketplace-worker@" + digest, "example.invalid/marketplace-web@" + webDigest, "example.invalid/marketplace-docs@" + docsDigest},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "marketplace", chart}
			if tc.values != "" {
				args = append(args, "--values", filepath.Join("testdata", tc.values))
			}
			for _, value := range tc.set {
				args = append(args, "--set", value)
			}
			out, err := exec.Command("helm", args...).CombinedOutput()
			if tc.fail {
				if err == nil {
					t.Fatal("expected missing-version refusal, rendering succeeded")
				}
				if !strings.Contains(string(out), "set an explicit image.tag or image.digest") {
					t.Fatalf("expected missing-version refusal, got %v:\n%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("render chart: %v:\n%s", err, out)
			}
			decoder := yaml.NewDecoder(strings.NewReader(string(out)))
			var images []string
			for {
				var doc struct {
					Kind string
					Spec struct {
						Template struct {
							Spec struct {
								Containers []struct{ Image string }
							}
						}
					}
				}
				if err := decoder.Decode(&doc); err != nil {
					if !errors.Is(err, io.EOF) {
						t.Fatal(err)
					}
					break
				}
				if doc.Kind == "Deployment" {
					for _, container := range doc.Spec.Template.Spec.Containers {
						images = append(images, container.Image)
					}
				}
			}
			if len(images) != len(tc.want) {
				t.Fatalf("images = %v, want %v", images, tc.want)
			}
			for _, want := range tc.want {
				if !slices.Contains(images, want) {
					t.Errorf("images = %v, missing %q", images, want)
				}
			}
		})
	}
}

// TestChartPublisherUsesExplicitImageVersion keeps release validation independent
// of the intentionally empty deployment defaults.
func TestChartPublisherUsesExplicitImageVersion(t *testing.T) {
	jobs := getMap(t, loadWorkflow(t, "marketplace-chart.yml"), "jobs")
	for _, step := range getSlice(t, getMap(t, jobs, "publish"), "steps") {
		m := stepMap(t, step)
		if m["name"] != "Package release chart" {
			continue
		}
		run := m["run"].(string)
		for _, command := range []string{
			`helm lint --strict "$chart_dir" --set-string image.tag="$RELEASE_TAG"`,
			`helm template marketplace "$package" --namespace marketplace --set-string image.tag="$RELEASE_TAG"`,
		} {
			if !strings.Contains(run, command) {
				t.Errorf("publisher must validate with an explicit version: missing %q", command)
			}
		}
		return
	}
	t.Fatal("release chart packaging step missing")
}
