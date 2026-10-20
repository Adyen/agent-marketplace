package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

var errAuditFailed = errors.New("model catalog audit found drift or unverifiable evidence; inspect the report")

func trustedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	switch u.Host {
	case "platform.claude.com", "code.claude.com", "developers.openai.com", "learn.chatgpt.com", "cursor.com", "docs.devin.ai", "docs.factory.ai", "docs.factory.com", "geminicli.com", "opencode.ai", "raw.githubusercontent.com":
		return true
	case "github.com":
		return strings.HasPrefix(u.Path, "/google-gemini/gemini-cli/") && u.Path == path.Clean(u.Path)
	}
	return false
}

func publicClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !trustedURL(req.URL.String()) {
				return fmt.Errorf("untrusted or excessive redirect")
			}
			return nil
		},
	}
}

func fetchPublic(ctx context.Context, client *http.Client, endpoint string) (fetched, error) {
	if !trustedURL(endpoint) {
		return fetched{}, fmt.Errorf("untrusted source URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("User-Agent", "agent-marketplace-model-catalog-audit/1")
	req.Header.Set("Accept", "*/*")
	response, err := client.Do(req)
	if err != nil {
		return fetched{}, fmt.Errorf("fetch source: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fetched{}, fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	body, err := readLimited(response.Body)
	if err != nil {
		return fetched{}, fmt.Errorf("read source: %w", err)
	}
	return fetched{Body: body, URL: response.Request.URL.String()}, nil
}

func run(args []string, in io.Reader, out io.Writer, fetch fetchFunc, now time.Time) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: model-catalog-audit check [--root PATH] | summary < report.json | propose [--root PATH] < report.json")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch args[0] {
	case "check":
		root := flags.String("root", ".", "Repository root (read-only)")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return fmt.Errorf("invalid check arguments")
		}
		files := os.DirFS(*root)
		body, err := readBounded(files, baselinesPath)
		if err != nil {
			return fmt.Errorf("read evidence baselines: %w", err)
		}
		var baselines map[string]baseline
		if err := json.Unmarshal(body, &baselines); err != nil {
			return fmt.Errorf("decode evidence baselines: %w", err)
		}
		if err := validateBaselines(baselines, sources); err != nil {
			return err
		}
		slog.Info("auditing public model evidence", "sources", len(sources))
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		result := audit(ctx, files, sources, baselines, fetch, now)
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("write audit report: %w", err)
		}
		slog.Info("model evidence audit finished", "failed", result.Failed, "sources", len(result.Sources))
		if result.Failed {
			return errAuditFailed
		}
		return nil
	case "summary":
		if len(args) != 1 {
			return fmt.Errorf("summary does not accept arguments")
		}
		result, err := decodeReport(in)
		if err != nil {
			return err
		}
		return summarize(out, result)
	case "propose":
		root := flags.String("root", ".", "Repository root to edit in place")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return fmt.Errorf("invalid propose arguments")
		}
		result, err := decodeReport(in)
		if err != nil {
			return err
		}
		return proposeChanges(*root, result, out)
	default:
		return fmt.Errorf("unknown command; use check, summary, or propose")
	}
}

func decodeReport(in io.Reader) (report, error) {
	var result report
	body, err := readLimited(in)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode audit report: %w", err)
	}
	if result.Schema != 1 || len(result.Sources) == 0 {
		return result, fmt.Errorf("invalid or empty audit report")
	}
	return result, nil
}

func summarize(out io.Writer, result report) error {
	var text strings.Builder
	status := "PASS (no observed drift)"
	if result.Failed {
		status = "FAIL (drift or unknown evidence)"
	}
	fmt.Fprintf(&text, "## Model catalog audit: %s\n\nChecked: %s\n\n", status, html.EscapeString(result.CheckedAt))
	text.WriteString("Read-only review findings, not proof of native acceptance or account availability. The audit itself makes no edits; a separate job opens a pull request only when it can propose actual catalog additions. Documentation-only drift and manual-entry candidates remain in the job summaries and audit artifact; evidence baselines are never refreshed automatically. An ID absent from an inventory is not automatically retired. The per-harness coverage statements below describe existing model inventory monitoring; sources listing a catalog domain monitor hook or tool documentation drift only. Offline tests bind every catalog evidence reference to a monitored source, except the commit-pinned revisions and repository documents they list exactly, which nothing here monitors; this report covers only whether the monitored sources changed.\n\n")
	for _, harness := range slices.Sorted(maps.Keys(result.Coverage)) {
		fmt.Fprintf(&text, "- **%s**: %s\n", harness, html.EscapeString(result.Coverage[harness]))
	}
	for _, s := range result.Sources {
		fmt.Fprintf(&text, "\n### %s: %s\n\n", html.EscapeString(s.Name), html.EscapeString(s.Status))
		fmt.Fprintf(&text, "Source: %s\n\n", html.EscapeString(s.URL))
		if s.CatalogDomain != "" {
			fmt.Fprintf(&text, "Catalog domain: %s\n\n", html.EscapeString(s.CatalogDomain))
		}
		for i, f := range s.Findings {
			if i == 20 {
				fmt.Fprintf(&text, "- %d additional findings in the JSON artifact.\n", len(s.Findings)-i)
				break
			}
			fmt.Fprintf(&text, "- **%s** %s: %s\n", html.EscapeString(f.Code), html.EscapeString(f.Model), html.EscapeString(f.Message))
		}
		if len(s.Findings) == 0 {
			text.WriteString("No changes detected within this source's stated coverage.\n")
		}
	}
	text.WriteString("\nReview the JSON artifact's observed evidence and hashes. Update catalogs only with resource-format evidence and independent conversion fixtures, then run `make model-catalog-check`. Update baseline hashes only after reviewing the source change; a baseline is a change detector, not a completeness attestation.\n")
	_, err := io.WriteString(out, text.String())
	return err
}

func main() {
	client := publicClient()
	if err := run(os.Args[1:], os.Stdin, os.Stdout, func(ctx context.Context, endpoint string) (fetched, error) {
		return fetchPublic(ctx, client, endpoint)
	}, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "model-catalog-audit:", err)
		os.Exit(1)
	}
}
