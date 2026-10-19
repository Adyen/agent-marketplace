package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func jqDir(t *testing.T) string {
	t.Helper()
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Fatalf("jq is required to exercise the TUI QA recording steps: %v", err)
	}
	return filepath.Dir(jq)
}

func TestTUIQARecordingList(t *testing.T) {
	run := findStepRun(t, loadCIWorkflow(t), "tui-qa", "List failed journey recordings")
	path := jqDir(t) + ":/usr/bin:/bin"
	for _, tc := range []struct {
		name, os string
		gifs     []string
		want     string
	}{
		{"kept recordings in order", "Linux", []string{"wheel-burst", "help-and-return", "Bad_Name"}, `recordings_linux={"os":"Linux","journey":"help-and-return"},{"os":"Linux","journey":"wheel-burst"}`},
		{"macOS output", "macOS", []string{"layout-quality"}, `recordings_macos={"os":"macOS","journey":"layout-quality"}`},
		{"no evidence", "Linux", nil, "recordings_linux="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			artifacts := filepath.Join(dir, "tui-qa-failures")
			if tc.gifs != nil {
				// A journey that failed before recording keeps only its trace,
				// and nothing outside a journey directory is a recording.
				for _, file := range []string{"setup-failure/trace-1/trace.md", "stray.gif"} {
					writeFile(t, filepath.Join(artifacts, file))
				}
			}
			for _, journey := range tc.gifs {
				writeFile(t, filepath.Join(artifacts, journey, journey+".gif"))
			}
			output := filepath.Join(dir, "output")
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{"PATH=" + path, "AM_TUI_QA_ARTIFACTS=" + artifacts, "RUNNER_OS=" + tc.os, "GITHUB_OUTPUT=" + output}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("list failed: %v\n%s", err, out)
			}
			if got, _ := os.ReadFile(output); strings.TrimSpace(string(got)) != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeGH serves canned artifact and comment listings through the real jq
// filters and records the one comment write the step may make.
const fakeGH = `#!/bin/bash
set -euo pipefail
method=GET url= filter=
while (($#)); do
  case "$1" in
    api|--paginate) ;;
    --method) method=$2; shift ;;
    --jq) filter=$2; shift ;;
    --input) shift ;;
    *) url=$1 ;;
  esac
  shift
done
case "$method $url" in
  "GET "*/artifacts\?*) jq -cr "$filter" "$GH_ARTIFACTS" ;;
  "GET "*/comments\?*) jq -cr "$filter" "$GH_COMMENTS" ;;
  *) { printf '%s %s\n' "$method" "$url"; cat; } > "$GH_WRITE" ;;
esac
`

func TestTUIQARecordingComment(t *testing.T) {
	doc := loadCIWorkflow(t)
	run := findStepRun(t, doc, "tui-qa-report", "Update pull request comment")
	path := jqDir(t) + ":/usr/bin:/bin"
	const (
		repository = "Adyen/agent-marketplace"
		runURL     = "https://github.com/Adyen/agent-marketplace/actions/runs/99"
		linux      = `{"os":"Linux","journey":"help-and-return"},{"os":"Linux","journey":"wheel-burst"}`
		macos      = `{"os":"macOS","journey":"layout-quality"},{"os":"Linux","journey":"../escape"}`
		artifacts  = `{"artifacts":[{"name":"tui-qa-failures-Linux","id":11},{"name":"tui-qa-Linux-help-and-return.gif","id":12},{"name":"tui-qa-macOS-layout-quality.gif","id":13}]}`
		comments   = `[{"id":7,"user":{"login":"someone"},"body":"<!-- agent-marketplace-tui-qa-recordings -->"},{"id":9,"user":{"login":"github-actions[bot]"},"body":"old <!-- agent-marketplace-tui-qa-recordings -->"}]`
	)
	for _, tc := range []struct {
		name, result, linux, macos, comments, headRepository string
		wantWrite                                            string
		wantBody                                             []string
		wantSummary                                          bool
	}{
		{
			name: "failure posts per-journey links", result: "failure", linux: linux, macos: macos, comments: "[]", headRepository: repository,
			wantWrite: "POST repos/Adyen/agent-marketplace/issues/42/comments",
			wantBody: []string{
				"<!-- agent-marketplace-tui-qa-recordings -->",
				"TUI QA failed on `0123456789ab` in [this run](" + runURL + ").",
				"| Linux | `help-and-return` | [GIF](" + runURL + "/artifacts/12) | [evidence](" + runURL + "/artifacts/11) |",
				"| Linux | `wheel-burst` | not uploaded | [evidence](" + runURL + "/artifacts/11) |",
				"| macOS | `layout-quality` | [GIF](" + runURL + "/artifacts/13) | not uploaded |",
			},
			wantSummary: true,
		},
		{
			name: "failure updates the bot comment", result: "failure", linux: linux, comments: comments, headRepository: repository,
			wantWrite: "PATCH repos/Adyen/agent-marketplace/issues/comments/9", wantBody: []string{"`help-and-return`"}, wantSummary: true,
		},
		{
			name: "failure without recordings", result: "failure", comments: "[]", headRepository: repository,
			wantWrite: "POST repos/Adyen/agent-marketplace/issues/42/comments", wantBody: []string{"No journey recording was kept"}, wantSummary: true,
		},
		{
			name: "success updates the bot comment", result: "success", comments: comments, headRepository: repository,
			wantWrite: "PATCH repos/Adyen/agent-marketplace/issues/comments/9", wantBody: []string{"TUI QA passed on `0123456789ab`"},
		},
		{name: "success without a comment stays quiet", result: "success", comments: "[]", headRepository: repository},
		{name: "fork only writes the summary", result: "failure", linux: linux, comments: comments, headRepository: "someone/agent-marketplace", wantSummary: true},
		{name: "cancelled stays quiet", result: "cancelled", linux: linux, comments: comments, headRepository: repository},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o700); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"artifacts.json": artifacts, "comments.json": tc.comments} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write, summary := filepath.Join(dir, "write"), filepath.Join(dir, "summary")
			cmd := exec.Command("bash", "-c", run)
			cmd.Env = []string{
				"PATH=" + bin + ":" + path,
				"GH_ARTIFACTS=" + filepath.Join(dir, "artifacts.json"),
				"GH_COMMENTS=" + filepath.Join(dir, "comments.json"),
				"GH_WRITE=" + write,
				"GITHUB_SERVER_URL=https://github.com",
				"GITHUB_STEP_SUMMARY=" + summary,
				"RUNNER_TEMP=" + dir,
				"HEAD_REPOSITORY=" + tc.headRepository,
				"HEAD_SHA=0123456789abcdef0123456789abcdef01234567",
				"PR_NUMBER=42",
				"RECORDINGS_LINUX=" + tc.linux,
				"RECORDINGS_MACOS=" + tc.macos,
				"REPOSITORY=" + repository,
				"RUN_ID=99",
				"TUI_QA_RESULT=" + tc.result,
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("report failed: %v\n%s", err, out)
			}
			written, err := os.ReadFile(write)
			if tc.wantWrite == "" {
				if err == nil {
					t.Fatalf("unexpected comment write:\n%s", written)
				}
			} else {
				if err != nil {
					t.Fatalf("missing %s: %v", tc.wantWrite, err)
				}
				request, payload, _ := strings.Cut(string(written), "\n")
				var body struct{ Body string }
				if err := json.Unmarshal([]byte(payload), &body); err != nil {
					t.Fatalf("comment payload is not JSON: %v\n%s", err, payload)
				}
				if request != tc.wantWrite {
					t.Errorf("request = %q, want %q", request, tc.wantWrite)
				}
				for _, want := range tc.wantBody {
					if !strings.Contains(body.Body, want) {
						t.Errorf("comment lacks %q:\n%s", want, body.Body)
					}
				}
				if strings.Contains(body.Body, "escape") {
					t.Errorf("comment carries an unvalidated journey:\n%s", body.Body)
				}
			}
			if _, err := os.Stat(summary); (err == nil) != tc.wantSummary {
				t.Errorf("job summary written = %v, want %v", err == nil, tc.wantSummary)
			}
		})
	}
}

// The report links artifacts by name, so the uploads must keep the names it
// looks up: one unzipped GIF per platform and journey, and one bundle per
// platform.
func TestTUIQARecordingArtifactNames(t *testing.T) {
	jobs := getMap(t, loadCIWorkflow(t), "jobs")
	with := func(job, step string) map[string]any {
		for _, s := range getSlice(t, getMap(t, jobs, job), "steps") {
			if m := stepMap(t, s); m["name"] == step {
				return getMap(t, m, "with")
			}
		}
		t.Fatalf("step %q not found in job %q", step, job)
		return nil
	}
	if got := with("tui-qa", "Upload failed journey evidence")["name"]; got != "tui-qa-failures-${{ runner.os }}" {
		t.Errorf("bundle name = %v", got)
	}
	if got := with("tui-qa-recordings", "Download failed journey evidence")["name"]; got != "tui-qa-failures-${{ matrix.os }}" {
		t.Errorf("downloaded bundle = %v", got)
	}
	upload := with("tui-qa-recordings", "Upload the recording unzipped")
	if upload["archive"] != false || filepath.Base(upload["path"].(string)) != "tui-qa-${{ matrix.os }}-${{ matrix.journey }}.gif" {
		t.Errorf("recording upload = archive %v, path %v", upload["archive"], upload["path"])
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
