package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

const (
	homebrewBranch = "heads/homebrew"
	formulaPath    = "Formula/agent-marketplace.rb"
	versionPath    = "VERSION"
)

var ghExec = func(args []string, stdin []byte) ([]byte, string, int) {
	cmd := exec.Command("gh", args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
	}
	return stdout.Bytes(), stderr.String(), code
}

var httpStatusPattern = regexp.MustCompile(`HTTP (\d{3})`)

func ghAPI(method, endpoint string, payload any) ([]byte, int, error) {
	args := []string{"api", endpoint}
	var stdin []byte
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		stdin = data
		args = []string{"api", "--method", method, endpoint, "--input", "-"}
	}
	out, stderr, code := ghExec(args, stdin)
	if code == 0 {
		return out, 0, nil
	}
	status := 0
	if m := httpStatusPattern.FindStringSubmatch(stderr); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			status = n
		}
	}
	return nil, status, fmt.Errorf("gh api %s %s: %s", method, endpoint, stderr)
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type ownedOutputs struct {
	version string
	formula string
}

func readOwned(inputDir string) (ownedOutputs, error) {
	var out ownedOutputs
	v, err := os.ReadFile(filepath.Join(inputDir, versionPath))
	if err != nil {
		return out, fmt.Errorf("read VERSION: %w", err)
	}
	f, err := os.ReadFile(filepath.Join(inputDir, formulaPath))
	if err != nil {
		return out, fmt.Errorf("read formula: %w", err)
	}
	out.version, out.formula = string(v), string(f)
	return out, nil
}

func readBlob(repo, sha string) (string, error) {
	out, _, err := ghAPI("GET", "repos/"+repo+"/git/blobs/"+sha, nil)
	if err != nil {
		return "", err
	}
	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(out, &blob); err != nil {
		return "", fmt.Errorf("parse blob %s: %w", sha, err)
	}
	if blob.Encoding != "base64" {
		return "", fmt.Errorf("blob %s has unexpected encoding %q", sha, blob.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(blob.Content)
	if err != nil {
		return "", fmt.Errorf("decode blob %s: %w", sha, err)
	}
	return string(data), nil
}

type branchState struct {
	head    string
	treeSHA string
	owned   ownedOutputs
}

func readBranch(repo string) (*branchState, error) {
	out, status, err := ghAPI("GET", "repos/"+repo+"/git/ref/"+homebrewBranch, nil)
	if err != nil {
		if status == 404 {
			return nil, nil
		}
		return nil, err
	}
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := json.Unmarshal(out, &ref); err != nil || ref.Object.SHA == "" {
		return nil, fmt.Errorf("malformed ref response for %s", homebrewBranch)
	}
	if ref.Object.Type != "commit" {
		return nil, fmt.Errorf("ref %s points to %q, not a commit", homebrewBranch, ref.Object.Type)
	}
	st := &branchState{head: ref.Object.SHA}
	out, _, err = ghAPI("GET", "repos/"+repo+"/git/commits/"+st.head, nil)
	if err != nil {
		return nil, err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(out, &commit); err != nil || commit.Tree.SHA == "" {
		return nil, fmt.Errorf("malformed commit response for %s", st.head)
	}
	st.treeSHA = commit.Tree.SHA
	out, _, err = ghAPI("GET", "repos/"+repo+"/git/trees/"+st.treeSHA+"?recursive=1", nil)
	if err != nil {
		return nil, err
	}
	var tree struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, fmt.Errorf("malformed tree response for %s", st.treeSHA)
	}
	if tree.Truncated {
		return nil, fmt.Errorf("tree %s response is truncated", st.treeSHA)
	}
	entries := map[string]treeEntry{}
	for _, e := range tree.Tree {
		entries[e.Path] = e
	}
	for _, path := range []string{versionPath, formulaPath} {
		e, ok := entries[path]
		if !ok {
			return nil, fmt.Errorf("existing %s is missing required owned entry %s", homebrewBranch, path)
		}
		if e.Type != "blob" || (e.Mode != "100644" && e.Mode != "100755") {
			return nil, fmt.Errorf("owned entry %s is not a regular blob (type %q mode %q)", path, e.Type, e.Mode)
		}
		content, err := readBlob(repo, e.SHA)
		if err != nil {
			return nil, err
		}
		if path == versionPath {
			st.owned.version = content
		} else {
			st.owned.formula = content
		}
	}
	return st, nil
}

func publish(repo, inputDir string) error {
	if err := validateRepository(repo); err != nil {
		return err
	}
	owned, err := readOwned(inputDir)
	if err != nil {
		return err
	}
	incoming, err := parseVersionNumber(owned.version)
	if err != nil {
		return err
	}
	st, err := readBranch(repo)
	if err != nil {
		return err
	}
	if st != nil {
		existing, err := parseVersionNumber(st.owned.version)
		if err != nil {
			return fmt.Errorf("existing %s on %s: %w", versionPath, homebrewBranch, err)
		}
		switch compareVersions(incoming, existing) {
		case -1:
			fmt.Printf("homebrew: branch already at newer %s; skipping\n", st.owned.version)
			return nil
		case 0:
			if st.owned.version == owned.version && st.owned.formula == owned.formula {
				fmt.Println("homebrew: identical owned outputs already published; no-op")
				return nil
			}
			return fmt.Errorf("version %s already published with different owned outputs", owned.version)
		}
	}
	var treeBody map[string]any
	entries := []map[string]any{
		{"path": versionPath, "mode": "100644", "type": "blob", "content": owned.version},
		{"path": formulaPath, "mode": "100644", "type": "blob", "content": owned.formula},
	}
	if st != nil {
		treeBody = map[string]any{"base_tree": st.treeSHA, "tree": entries}
	} else {
		treeBody = map[string]any{"tree": entries}
	}
	out, _, err := ghAPI("POST", "repos/"+repo+"/git/trees", treeBody)
	if err != nil {
		return err
	}
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(out, &newTree); err != nil || newTree.SHA == "" {
		return fmt.Errorf("malformed tree creation response")
	}
	commitBody := map[string]any{
		"message": fmt.Sprintf("Publish Homebrew formula %s", owned.version),
		"tree":    newTree.SHA,
	}
	if st != nil {
		commitBody["parents"] = []string{st.head}
	}
	out, _, err = ghAPI("POST", "repos/"+repo+"/git/commits", commitBody)
	if err != nil {
		return err
	}
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(out, &newCommit); err != nil || newCommit.SHA == "" {
		return fmt.Errorf("malformed commit creation response")
	}
	if st == nil {
		_, _, err = ghAPI("POST", "repos/"+repo+"/git/refs", map[string]any{
			"ref": "refs/" + homebrewBranch,
			"sha": newCommit.SHA,
		})
	} else {
		_, _, err = ghAPI("PATCH", "repos/"+repo+"/git/refs/"+homebrewBranch, map[string]any{
			"sha":   newCommit.SHA,
			"force": false,
		})
	}
	return err
}
