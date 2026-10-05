package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
)

func TestRepoInstallStagesMissingConfigSkeletonAndSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "lower-layer-install.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [claude]\nregistries:\n  - url: "+origin+"\n    ref: main\n")

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	canonicalRepoConfig, err := lifecycle.CanonicalizeFuturePath(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop before dependency commit")
	originalCommit := lifecycle.CommitDependencyTransaction
	lifecycle.CommitDependencyTransaction = func(_ context.Context, _ string, desired []dependencytxn.DesiredFile, _ ...dependencytxn.Option) (dependencytxn.Result, error) {
		if _, err := os.Stat(repoConfig); !os.IsNotExist(err) {
			t.Fatalf("real repo config exists when dependency reconcile starts: %v", err)
		}
		for _, file := range desired {
			if file.Path != canonicalRepoConfig {
				continue
			}
			var staged struct {
				APIVersion string `yaml:"apiVersion"`
				Sources    map[string]struct {
					Git string `yaml:"git"`
					Ref string `yaml:"ref"`
				} `yaml:"sources"`
				Resources map[string]struct {
					Include []string `yaml:"include"`
				} `yaml:"resources"`
			}
			if err := yaml.Unmarshal(file.Bytes, &staged); err != nil {
				t.Fatalf("parse staged repo config: %v", err)
			}
			if staged.APIVersion != "agent-marketplace/v4" || len(staged.Sources) != 1 || len(staged.Resources) != 1 {
				t.Fatalf("staged repo config lacks v4 schema, source, or resource:\n%s", file.Bytes)
			}
			for name, source := range staged.Sources {
				gitURL, err := url.Parse(source.Git)
				if err != nil {
					t.Fatalf("parse staged source URL: %v", err)
				}
				actualOrigin, err := filepath.EvalSymlinks(gitURL.Path)
				if err != nil {
					t.Fatalf("resolve staged source path: %v", err)
				}
				expectedOrigin, err := filepath.EvalSymlinks(origin)
				if err != nil {
					t.Fatalf("resolve lower-layer registry path: %v", err)
				}
				selection, ok := staged.Resources[name]
				if gitURL.Scheme != "file" || gitURL.Host != "" || actualOrigin != expectedOrigin || source.Ref != "main" || !ok || len(selection.Include) != 1 || selection.Include[0] != "skill/review" {
					t.Fatalf("staged repo config lacks lower-layer source or skill selection:\n%s", file.Bytes)
				}
			}
			return dependencytxn.Result{}, stop
		}
		t.Fatalf("staged repo config missing from desired transaction: %#v", desired)
		return dependencytxn.Result{}, stop
	}
	t.Cleanup(func() { lifecycle.CommitDependencyTransaction = originalCommit })

	_, err = executeCommand("install", "review")
	if !errors.Is(err, stop) {
		t.Fatalf("install error = %v, want %v", err, stop)
	}
	if _, err := os.Stat(repoConfig); !os.IsNotExist(err) {
		t.Fatalf("real repo config published after failed reconcile: %v", err)
	}
}
