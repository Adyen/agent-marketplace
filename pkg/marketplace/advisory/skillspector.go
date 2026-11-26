package advisory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	skillSpectorVersion = "2.13.6"
	maxResponseBytes    = 128 << 10
	maxFindings         = 128
	defaultTimeout      = 15 * time.Second
	defaultBudget       = 34 * time.Second
)

const (
	// MaxFiles is the maximum number of files in a runner request.
	MaxFiles = 128
	// MaxFileBytes is the maximum decoded size of one runner input file.
	MaxFileBytes = 1 << 20
	// MaxBundleBytes is the maximum decoded size of one runner request.
	MaxBundleBytes = 4 << 20
)

var (
	errScannerUnavailable = errors.New("scanner unavailable")
	errInvalidResponse    = errors.New("invalid scanner response")
	ruleID                = regexp.MustCompile(`^(AE|AR|AS|DE|DS|EA|HC|MP|OH|PE|PI|RA|SC|SL|TM|TR|SS)[0-9]{1,3}$`)
)

// Scanner submits detached files to a supplementary analyzer.
type Scanner interface {
	Scan(context.Context, map[string][]byte) (Result, error)
}

// Result is the bounded report returned by SkillSpector.
type Result struct {
	Version  string               `json:"version"`
	Status   store.AdvisoryStatus `json:"status"`
	Findings []Finding            `json:"findings"`
}

// Finding carries only fields that can be normalized for publication.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
}

// SkillSpector applies the fixed public report policy to an injected scanner.
type SkillSpector struct {
	Scanner Scanner
	Timeout time.Duration
	Budget  time.Duration
}

// TimeoutLimit returns the per-bundle timeout, using the default when unset.
func (s SkillSpector) TimeoutLimit() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultTimeout
}

// BudgetLimit returns the aggregate per-registry budget, using the default when unset.
func (s SkillSpector) BudgetLimit() time.Duration {
	if s.Budget > 0 {
		return s.Budget
	}
	return defaultBudget
}

// Report runs one bounded scan and converts failures into non-publishing evidence.
func (s SkillSpector) Report(ctx context.Context, files map[string][]byte, supportPaths map[string]string) store.AdvisoryReport {
	report := store.AdvisoryReport{Tool: "skillspector"}
	if s.Scanner == nil {
		report.Status = store.AdvisoryNotRun
		report.Reason = "unsupported_bundle"
		return report
	}
	if err := validateBundle(files); err != nil {
		report.Status = store.AdvisoryNotRun
		report.Reason = "bundle_limits"
		return report
	}
	scanCtx, cancel := context.WithTimeout(ctx, s.TimeoutLimit())
	defer cancel()
	result, err := s.Scanner.Scan(scanCtx, files)
	if err != nil {
		report.Status = store.AdvisoryFailed
		switch {
		case errors.Is(scanCtx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			report.Reason = "timeout"
		case errors.Is(err, errInvalidResponse):
			report.Reason = "invalid_response"
		default:
			report.Reason = "unavailable"
		}
		return report
	}
	if result.Version != skillSpectorVersion || len(result.Findings) > maxFindings {
		report.Status = store.AdvisoryFailed
		report.Reason = "invalid_response"
		return report
	}
	switch result.Status {
	case store.AdvisoryComplete:
		report.Status = store.AdvisoryComplete
	case store.AdvisoryCompleteWithCaveats:
		report.Status = store.AdvisoryCompleteWithCaveats
		report.Reason = "offline_limitations"
	case store.AdvisoryPartial:
		report.Status = store.AdvisoryPartial
		report.Reason = "coverage_gaps"
	default:
		report.Status = store.AdvisoryFailed
		report.Reason = "invalid_response"
		return report
	}
	report.Version = result.Version
	report.Findings = normalizeFindings(result.Findings, files, supportPaths)
	return report
}

// NewScanner returns nil for an empty endpoint or a scanner for a fixed local
// HTTP or Unix-socket endpoint.
func NewScanner(endpoint string) (Scanner, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || strings.ContainsAny(endpoint, "?#") || u.Fragment != "" {
		return nil, errors.New("invalid SkillSpector endpoint")
	}
	transport := &http.Transport{MaxConnsPerHost: 1}
	baseURL := "http://unix"
	switch u.Scheme {
	case "http":
		port, portErr := strconv.Atoi(u.Port())
		if u.Hostname() == "" || portErr != nil || port < 1 || port > 65535 || u.Path != "" || u.Opaque != "" {
			return nil, errors.New("invalid SkillSpector endpoint")
		}
		transport.Proxy = nil
		baseURL = u.Scheme + "://" + u.Host
	case "unix":
		if u.Host != "" || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") || u.Path == "/" ||
			path.Clean(u.Path) != u.Path || strings.ContainsRune(u.Path, '\x00') {
			return nil, errors.New("invalid SkillSpector endpoint")
		}
		socket := u.Path
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}
	default:
		return nil, errors.New("invalid SkillSpector endpoint")
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return httpScanner{client: client, endpoint: baseURL}, nil
}

type httpScanner struct {
	client   *http.Client
	endpoint string
}

func (s httpScanner) Scan(ctx context.Context, files map[string][]byte) (Result, error) {
	if err := validateBundle(files); err != nil {
		return Result{}, errInvalidResponse
	}
	encoded := make(map[string]string, len(files))
	for name, data := range files {
		encoded[name] = base64.StdEncoding.EncodeToString(data)
	}
	body, err := json.Marshal(struct {
		Files map[string]string `json:"files"`
	}{Files: encoded})
	if err != nil {
		return Result{}, errInvalidResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/v1/scan", bytes.NewReader(body))
	if err != nil {
		return Result{}, errInvalidResponse
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return Result{}, errScannerUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusGatewayTimeout {
		return Result{}, context.DeadlineExceeded
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, errScannerUnavailable
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(response) > maxResponseBytes {
		return Result{}, errInvalidResponse
	}
	var decoded struct {
		Version  *string               `json:"version"`
		Status   *store.AdvisoryStatus `json:"status"`
		Findings *[]Finding            `json:"findings"`
	}
	if err := jsonv2.Unmarshal(response, &decoded); err != nil ||
		decoded.Version == nil || decoded.Status == nil || decoded.Findings == nil ||
		len(*decoded.Findings) > maxFindings {
		return Result{}, errInvalidResponse
	}
	result := Result{Version: *decoded.Version, Status: *decoded.Status, Findings: *decoded.Findings}
	return result, nil
}

func validateBundle(files map[string][]byte) error {
	if len(files) == 0 || len(files) > MaxFiles {
		return errInvalidResponse
	}
	if _, ok := files["SKILL.md"]; !ok {
		return errInvalidResponse
	}
	var total int64
	for name, data := range files {
		if !validBundlePath(name) || len(data) > MaxFileBytes {
			return errInvalidResponse
		}
		total += int64(len(data))
		if total > MaxBundleBytes {
			return errInvalidResponse
		}
	}
	return nil
}

func validBundlePath(name string) bool {
	return fs.ValidPath(name) && !strings.Contains(name, `\`) &&
		path.Clean(name) == name && !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) })
}

func normalizeFindings(findings []Finding, files map[string][]byte, supportPaths map[string]string) []store.AdvisoryFinding {
	out := make([]store.AdvisoryFinding, 0, len(findings))
	for _, finding := range findings {
		code := "SS_OTHER"
		if ruleID.MatchString(finding.Code) {
			code = finding.Code
		}
		severity := normalizedSeverity(finding.Severity)
		pathValue := ""
		if validBundlePath(finding.Path) && finding.Path != "SKILL.md" {
			if projectionPath, allowed := supportPaths[finding.Path]; allowed &&
				validBundlePath(projectionPath) && strings.HasPrefix(projectionPath, "support/") &&
				strings.TrimPrefix(projectionPath, "support/") == finding.Path {
				if _, supplied := files[finding.Path]; supplied {
					pathValue = projectionPath
				}
			}
		}
		summary := "Potential risky skill behavior."
		if code == "SC4" {
			summary = "Dependency risk detected."
		}
		out = append(out, store.AdvisoryFinding{
			Code: code, Severity: severity, Summary: summary, Path: pathValue,
		})
	}
	slices.SortFunc(out, func(a, b store.AdvisoryFinding) int {
		return strings.Compare(a.Code+"\x00"+a.Severity+"\x00"+a.Path, b.Code+"\x00"+b.Severity+"\x00"+b.Path)
	})
	return slices.CompactFunc(out, func(a, b store.AdvisoryFinding) bool {
		return a.Code == b.Code && a.Severity == b.Severity && a.Path == b.Path
	})
}

func normalizedSeverity(value string) string {
	switch strings.ToLower(value) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium", "moderate":
		return "medium"
	case "low":
		return "low"
	case "info", "informational":
		return "info"
	default:
		return "unknown"
	}
}
