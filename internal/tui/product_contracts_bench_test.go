package tui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/charmbracelet/glamour"
)

func BenchmarkSanitizeTerminalData(b *testing.B) {
	cases := []struct {
		name  string
		value string
		mode  terminalDataMode
	}{
		{name: "safe ASCII", value: "registry-name/skills/audit", mode: terminalSingleLine},
		{name: "safe Unicode", value: "缓存/技能/A界e\u0301", mode: terminalSingleLine},
		{name: "hostile single line", value: "name\n\x1b]0;owned\x07\u202e\x1b[2J", mode: terminalSingleLine},
		{name: "hostile Markdown", value: "# Heading\n\nbody\tvalue\x1b[31m\u2066 &#27;]52;c;YmVuY2g=&#7; &NewLine;", mode: terminalMarkdownBody},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = sanitizeTerminalData(tc.value, tc.mode)
			}
		})
	}
}

func BenchmarkRenderHostileMarkdown(b *testing.B) {
	fragment := "plain &#27;]52;c;YmVuY2g=&#7; &#27;[?1049h &#129; &#x202E; &NewLine;\n" +
		"*emphasis &#x1B;[2J* `code &#157;52;c;YmVuY2g=&#7;` " +
		`[link](https://example.invalid/&#27;[2J "title &#x202E;") ` +
		"<span>raw &#27;]52;c;YmVuY2g=&#7;</span>\n"
	cases := []struct {
		name   string
		width  int
		repeat int
	}{
		{name: "constrained-50-columns-4KiB", width: 50, repeat: 19},
		{name: "expanded-120-columns-128KiB", width: 120, repeat: 596},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			content := strings.Repeat(fragment, tc.repeat)
			renderer, err := glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(tc.width),
			)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(content)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := renderer.Render(sanitizeMarkdownForTerminal(content)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRenderDetailFullContentHostile measures the bounded production
// detail path (bounded read + line splitting + sanitized glamour render +
// rendered-line cap) against hostile sources at the two frame extremes. It
// has no base equivalent in /tmp/tui-qa-01-before.txt: the path was
// unbounded (unlimited read, no line splitting, fixed 80-column render).
func BenchmarkRenderDetailFullContentHostile(b *testing.B) {
	longToken := strings.Repeat("a", 300) // unbroken token: worst case for word wrap
	hostileLine := "&#27;]52;c;YmVuY2g=&#7; \u202e &#x1B;[2J " + longToken + "\n"
	cases := []struct {
		name   string
		width  int
		repeat int
	}{
		{name: "constrained-50-columns", width: 50, repeat: 4000}, // ≈1.3 MiB source, cut by the read cap
		{name: "expanded-120-columns", width: 120, repeat: 4000},  // same source, wider render
		{name: "minified-single-token", width: 80, repeat: 0},     // one giant unbroken line
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var source string
			if tc.repeat == 0 {
				source = strings.Repeat("a", maxDetailSourceBytes+65536)
			} else {
				source = "# hostile\n\n" + strings.Repeat(hostileLine, tc.repeat)
			}
			path := filepath.Join(b.TempDir(), "hostile.md")
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				b.Fatal(err)
			}
			r := mkRow("hostile", withProject)
			r.Artifact.Path = path
			b.SetBytes(int64(min(len(source), maxDetailSourceBytes)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if out := renderDetailFullContent(r, tc.width); out == "" {
					b.Fatal("empty detail render")
				}
			}
		})
	}
}

// BenchmarkReadDetailSourceBounded proves the capped read stays O(cap) even
// when the on-disk source is much larger. No base equivalent: the previous
// implementation read the whole file.
func BenchmarkReadDetailSourceBounded(b *testing.B) {
	path := filepath.Join(b.TempDir(), "huge.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 1<<20)), 0o644); err != nil { // ≈5 MiB
		b.Fatal(err)
	}
	b.SetBytes(maxDetailSourceBytes)
	b.ReportAllocs()
	for b.Loop() {
		content, truncated, err := readDetailSource(path)
		if err != nil || !truncated {
			b.Fatalf("readDetailSource = %d bytes truncated=%t err=%v", len(content), truncated, err)
		}
	}
}

// BenchmarkRenderMarkdownConcurrent measures the width-keyed production
// renderer cache under parallel load (detail loads run on background
// goroutines). No base equivalent: the previous single renderer had no
// synchronization at all.
func BenchmarkRenderMarkdownConcurrent(b *testing.B) {
	initRenderer()
	content := "# Concurrent\n\nConcurrent **rendering** through the shared width-keyed cache.\n"
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := renderMarkdownAtWidth(content, 80); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDiagnosticStoreReplaceSnapshot(b *testing.B) {
	diagnostics := make([]tuiDiagnostic, 0, 40)
	for i := 0; i < 20; i++ {
		diagnostic := snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, benchmarkError(i))
		diagnostics = append(diagnostics, diagnostic, diagnostic)
	}
	var store diagnosticStore
	b.ReportAllocs()
	for b.Loop() {
		store.replaceSnapshot(diagnostics)
	}
}

func BenchmarkRequestGeneration(b *testing.B) {
	var requests requestState
	b.ReportAllocs()
	for b.Loop() {
		generation := requests.begin(requestSnapshot)
		_ = generation == requests.active(requestSnapshot)
	}
}

func BenchmarkRenderSupportedMinimum(b *testing.B) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.loading = false
	m.driftAssessment = driftReady
	m.width = minimumTerminalWidth
	m.height = minimumTerminalHeight
	m.rows = []row{mkRow("selected", withProject, withRegistry("registry.git", "registry"))}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkRenderResizeRequired(b *testing.B) {
	m := Model{width: 49, height: 11}
	b.ReportAllocs()
	for b.Loop() {
		_ = renderResizeRequired(m)
	}
}

func BenchmarkReadSnapshotEmpty(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.Level(100)})))
	previousOrg, previousGlobal := loadOrgConfig, loadGlobalConfig
	previousRepo, previousLocal := loadRepoConfig, loadLocalConfig
	previousMaterialize := runMaterialize
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) { return nil, nil }
	loadLocalConfig = func() (*config.LocalConfig, error) { return nil, nil }
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{}, nil
	}
	b.Cleanup(func() {
		slog.SetDefault(previousLogger)
		loadOrgConfig, loadGlobalConfig = previousOrg, previousGlobal
		loadRepoConfig, loadLocalConfig = previousRepo, previousLocal
		runMaterialize = previousMaterialize
	})
	repoRoot, homeDir := b.TempDir(), b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		result := readSnapshot(repoRoot, homeDir, catalogLoadCached)
		if result.driftAssessment != driftReady {
			b.Fatalf("unexpected drift assessment %v", result.driftAssessment)
		}
	}
}

// BenchmarkRunUpdateCmdComplete measures the whole TUI update operation —
// pipeline.Sync (refresh, target-set check, backup scan, materialization)
// plus the authoritative post-operation snapshot — after the bespoke
// materialization was replaced by the shared pipeline. It has no base
// equivalent in /tmp/tui-qa-01-before.txt: the pre-change update path was
// never benchmarked end to end.
func BenchmarkRunUpdateCmdComplete(b *testing.B) {
	for _, tc := range []struct {
		name       string
		staleCache bool
	}{
		{name: "clean"},
		{name: "stale_cache_fallback"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.Level(100)})))
			b.Cleanup(func() { slog.SetDefault(previousLogger) })

			registryRoot := b.TempDir()
			writeRegistrySkill(b, registryRoot, "alpha")
			repoRoot, homeDir := writeUpdateTestConfigs(b, v2Header+`registries:
  - url: `+registryRoot+`
    skills:
      - alpha
`)
			if tc.staleCache {
				// The registry is served from the warm cache while the
				// fetch reports failure: the typed fallback path.
				restoreDetailed := swapCloneOrPullDetailed(func(url, ref string) (string, *cache.StaleCacheFallbackWarning, error) {
					return registryRoot, &cache.StaleCacheFallbackWarning{URL: url, Ref: ref, Err: errors.New("network unreachable")}, nil
				})
				b.Cleanup(restoreDetailed)
				restorePlain := swapCloneOrPull(func(url, ref string) (string, error) { return registryRoot, nil })
				b.Cleanup(restorePlain)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				msg, ok := runUpdateCmd(context.Background(), repoRoot, homeDir)().(updateDoneMsg)
				if !ok {
					b.Fatal("runUpdateCmd did not return updateDoneMsg")
				}
				if msg.err != nil {
					b.Fatalf("update failed: %v", msg.err)
				}
				if tc.staleCache && len(msg.staleCaches) != 1 {
					b.Fatalf("stale-cache fallbacks = %v, want 1", msg.staleCaches)
				}
			}
		})
	}
}

type benchmarkError int

func (e benchmarkError) Error() string { return "benchmark diagnostic" }

func (e benchmarkError) DedupKey() string { return string(rune(e)) }
