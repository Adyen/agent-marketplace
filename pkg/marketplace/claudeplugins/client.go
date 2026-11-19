package claudeplugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AnthropicVersion = "2026-10-21"
	PluginsBeta      = "ce-plugins-2026-09-01"
	defaultBaseURL   = "https://api.anthropic.com"
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 8 << 20
	maxRequestBytes  = 200 << 20
)

type APIKey struct {
	value string
}

func NewAPIKey(value string) APIKey {
	return APIKey{value: value}
}

func (APIKey) String() string { return "[redacted]" }

func (APIKey) GoString() string { return "[redacted]" }

func (APIKey) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, "[redacted]")
}

type Options struct {
	BaseURL        string
	APIKey         APIKey
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	OnResponse     func(endpoint string, status int)
}

type Client struct {
	baseURL    string
	apiKey     APIKey
	http       *http.Client
	timeout    time.Duration
	onResponse func(endpoint string, status int)

	settingLocksMu sync.Mutex
	settingLocks   map[string]*sync.Mutex
}

func (c *Client) String() string { return "claudeplugins.Client{api_key:[redacted]}" }

func (c *Client) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, c.String())
}

func NewClient(options Options) (*Client, error) {
	baseURL := strings.TrimRight(options.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("claude plugins API base URL must be an HTTP(S) origin")
	}
	if options.APIKey.value == "" {
		return nil, errors.New("claude plugins API key is required")
	}
	timeout := options.RequestTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport}
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		baseURL:      baseURL,
		apiKey:       options.APIKey,
		http:         client,
		timeout:      timeout,
		onResponse:   options.OnResponse,
		settingLocks: make(map[string]*sync.Mutex),
	}, nil
}

type Owner struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type Actor struct {
	Type     string `json:"type"`
	APIKeyID string `json:"api_key_id,omitempty"`
}

type Component struct {
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type ContentScan struct {
	Status     string  `json:"status"`
	Assessment string  `json:"assessment,omitempty"`
	Reason     *string `json:"reason"`
}

type Plugin struct {
	Type                               string      `json:"type"`
	ID                                 string      `json:"id"`
	Name                               string      `json:"name"`
	DisplayName                        string      `json:"display_name"`
	Description                        string      `json:"description"`
	ServedVersionID                    string      `json:"served_version_id"`
	ServedVersionPinned                bool        `json:"served_version_pinned"`
	LatestVersionID                    string      `json:"latest_version_id"`
	ManifestVersion                    string      `json:"manifest_version"`
	Owner                              Owner       `json:"owner"`
	MarketplaceID                      string      `json:"marketplace_id"`
	CreatedBy                          *Actor      `json:"created_by"`
	OrganizationInstallationPreference string      `json:"organization_installation_preference"`
	OrganizationInstallationInherited  bool        `json:"organization_installation_preference_inherited"`
	ContentScan                        ContentScan `json:"content_scan"`
	Components                         []Component `json:"components"`
	Reach                              string      `json:"reach"`
	CreatedAt                          time.Time   `json:"created_at"`
	UpdatedAt                          time.Time   `json:"updated_at"`
}

type PluginVersion struct {
	Type            string      `json:"type"`
	ID              string      `json:"id"`
	PluginID        string      `json:"plugin_id,omitempty"`
	DisplayName     string      `json:"display_name"`
	Description     string      `json:"description"`
	ManifestVersion string      `json:"manifest_version"`
	ReleaseNotes    string      `json:"release_notes"`
	CreatedBy       *Actor      `json:"created_by"`
	ContentScan     ContentScan `json:"content_scan"`
	Components      []Component `json:"components"`
	Reach           string      `json:"reach"`
	CreatedAt       time.Time   `json:"created_at"`
}

type PluginMarketplace struct {
	Type                               string `json:"type"`
	ID                                 string `json:"id"`
	Name                               string `json:"name"`
	Source                             string `json:"source"`
	Owner                              Owner  `json:"owner"`
	OrganizationInstallationPreference string `json:"organization_installation_preference"`
}

type InstallationSetting struct {
	InstallationPreference string `json:"installation_preference"`
	Target                 Owner  `json:"target"`
}

type PluginListQuery struct {
	OwnerType     string
	OwnerUserID   string
	MarketplaceID string
	Limit         int
}

type MarketplaceListQuery struct {
	OwnerType string
	Source    string
	Limit     int
}

type RateLimitHeaders struct {
	RequestsLimit     string
	RequestsRemaining string
	RequestsReset     string
}

type APIError struct {
	StatusCode      int
	Code            string
	PluginID        string
	PluginVersionID string
	SkillName       string
	ShouldRetry     *bool
	RetryAfter      time.Duration
	RateLimits      RateLimitHeaders
}

func (e *APIError) Error() string {
	return fmt.Sprintf("claude plugins API returned HTTP %d", e.StatusCode)
}

func (e *APIError) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, e.Error())
}

type ThrottledError struct {
	APIError   *APIError
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	if e == nil || e.APIError == nil {
		return "claude plugins API request was rate limited"
	}
	return e.APIError.Error()
}

func (e *ThrottledError) Unwrap() error { return e.APIError }

func (e *ThrottledError) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, e.Error())
}

type transportError struct {
	cause error
}

func (e transportError) Error() string { return "claude plugins API request failed" }

func (e transportError) Unwrap() error { return e.cause }

func (e transportError) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, e.Error())
}

type pageEnvelope[T any] struct {
	Data     []T     `json:"data"`
	NextPage *string `json:"next_page"`
}

func (c *Client) ListPlugins(ctx context.Context, query PluginListQuery) ([]Plugin, error) {
	values := make(url.Values)
	setQuery(values, "owner_type", query.OwnerType)
	setQuery(values, "owner_user_id", query.OwnerUserID)
	setQuery(values, "marketplace_id", query.MarketplaceID)
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	return listPages[Plugin](ctx, c, "/v1/organizations/plugins", values)
}

func (c *Client) GetPlugin(ctx context.Context, pluginID string) (Plugin, error) {
	var plugin Plugin
	err := c.requestJSON(ctx, http.MethodGet, pluginPath(pluginID), nil, "", &plugin)
	return plugin, err
}

func (c *Client) GetVersion(ctx context.Context, pluginID, versionID string) (PluginVersion, error) {
	var version PluginVersion
	err := c.requestJSON(ctx, http.MethodGet, pluginPath(pluginID)+"/versions/"+url.PathEscape(versionID), nil, "", &version)
	return version, err
}

func (c *Client) ListMarketplaces(ctx context.Context, query MarketplaceListQuery) ([]PluginMarketplace, error) {
	values := make(url.Values)
	setQuery(values, "owner_type", query.OwnerType)
	setQuery(values, "source", query.Source)
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	return listPages[PluginMarketplace](ctx, c, "/v1/organizations/plugin_marketplaces", values)
}

func (c *Client) GetMarketplace(ctx context.Context, marketplaceID string) (PluginMarketplace, error) {
	var marketplace PluginMarketplace
	err := c.requestJSON(ctx, http.MethodGet, "/v1/organizations/plugin_marketplaces/"+url.PathEscape(marketplaceID), nil, "", &marketplace)
	return marketplace, err
}

func (c *Client) CreatePlugin(ctx context.Context, archive []byte, marketplaceID, releaseNotes string) (Plugin, error) {
	body, contentType, err := uploadBody(archive, marketplaceID, releaseNotes)
	if err != nil {
		return Plugin{}, err
	}
	var plugin Plugin
	err = c.requestJSON(ctx, http.MethodPost, "/v1/organizations/plugins", body, contentType, &plugin)
	return plugin, err
}

func (c *Client) CreateVersion(ctx context.Context, pluginID string, archive []byte, releaseNotes string) (PluginVersion, error) {
	body, contentType, err := uploadBody(archive, "", releaseNotes)
	if err != nil {
		return PluginVersion{}, err
	}
	var version PluginVersion
	err = c.requestJSON(ctx, http.MethodPost, pluginPath(pluginID)+"/versions", body, contentType, &version)
	return version, err
}

func (c *Client) SetInstallationSetting(ctx context.Context, pluginID, preference string) (InstallationSetting, error) {
	lock := c.settingLock(pluginID)
	lock.Lock()
	defer lock.Unlock()

	body, err := json.Marshal(struct {
		InstallationPreference string `json:"installation_preference"`
	}{InstallationPreference: preference})
	if err != nil {
		return InstallationSetting{}, errors.New("encode claude plugins API installation setting")
	}
	var setting InstallationSetting
	err = c.requestJSON(ctx, http.MethodPost, pluginPath(pluginID)+"/installation_settings/organization", body, "application/json", &setting)
	return setting, err
}

func (c *Client) settingLock(pluginID string) *sync.Mutex {
	c.settingLocksMu.Lock()
	defer c.settingLocksMu.Unlock()
	lock := c.settingLocks[pluginID]
	if lock == nil {
		lock = &sync.Mutex{}
		c.settingLocks[pluginID] = lock
	}
	return lock
}

func listPages[T any](ctx context.Context, client *Client, endpoint string, query url.Values) ([]T, error) {
	var rows []T
	for {
		values := make(url.Values, len(query)+1)
		for key, items := range query {
			values[key] = append([]string(nil), items...)
		}
		var page pageEnvelope[T]
		if err := client.requestJSON(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil, "", &page); err != nil {
			return nil, err
		}
		rows = append(rows, page.Data...)
		if page.NextPage == nil {
			return rows, nil
		}
		query.Set("page", *page.NextPage)
	}
}

func setQuery(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func pluginPath(pluginID string) string {
	return "/v1/organizations/plugins/" + url.PathEscape(pluginID)
}

func uploadBody(archive []byte, marketplaceID, releaseNotes string) ([]byte, string, error) {
	if len(archive) > maxRequestBytes {
		return nil, "", errors.New("claude plugins API upload exceeds the request size limit")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files[]", "plugin.zip")
	if err != nil {
		return nil, "", errors.New("encode claude plugins API archive")
	}
	if _, err := part.Write(archive); err != nil {
		return nil, "", errors.New("encode claude plugins API archive")
	}
	if marketplaceID != "" {
		if err := writer.WriteField("marketplace_id", marketplaceID); err != nil {
			return nil, "", errors.New("encode claude plugins API marketplace")
		}
	}
	if releaseNotes != "" {
		if err := writer.WriteField("release_notes", releaseNotes); err != nil {
			return nil, "", errors.New("encode claude plugins API release notes")
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", errors.New("encode claude plugins API upload")
	}
	if body.Len() > maxRequestBytes {
		return nil, "", errors.New("claude plugins API upload exceeds the request size limit")
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func (c *Client) requestJSON(ctx context.Context, method, endpoint string, body []byte, contentType string, result any) error {
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(requestContext, method, c.baseURL+endpoint, bytes.NewReader(body))
		if err != nil {
			return errors.New("construct claude plugins API request")
		}
		request.Header.Set("x-api-key", c.apiKey.value)
		request.Header.Set("anthropic-version", AnthropicVersion)
		request.Header.Set("anthropic-beta", PluginsBeta)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, err := c.http.Do(request)
		if err != nil {
			return transportError{cause: err}
		}
		if c.onResponse != nil {
			c.onResponse(metricEndpoint(endpoint), response.StatusCode)
		}
		if response.StatusCode == http.StatusTooManyRequests {
			apiErr := apiError(response)
			if attempt == 0 && apiErr.RetryAfter >= 0 && response.Header.Get("retry-after") != "" {
				if err := wait(requestContext, apiErr.RetryAfter); err != nil {
					_ = response.Body.Close()
					return &ThrottledError{APIError: apiErr, RetryAfter: apiErr.RetryAfter}
				}
				_ = response.Body.Close()
				continue
			}
			_ = response.Body.Close()
			return &ThrottledError{APIError: apiErr, RetryAfter: apiErr.RetryAfter}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			apiErr := apiError(response)
			_ = response.Body.Close()
			return apiErr
		}
		if result == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
			_ = response.Body.Close()
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		_ = response.Body.Close()
		if err != nil {
			return errors.New("read claude plugins API response")
		}
		if len(data) > maxResponseBytes {
			return errors.New("claude plugins API response exceeds the size limit")
		}
		if err := json.Unmarshal(data, result); err != nil {
			return errors.New("decode claude plugins API response")
		}
		return nil
	}
	return errors.New("claude plugins API rate-limit retry did not complete")
}

func metricEndpoint(endpoint string) string {
	switch {
	case strings.HasSuffix(endpoint, "/installation_settings/organization"):
		return "installation_settings"
	case strings.Contains(endpoint, "/versions/"):
		return "plugin_version"
	case strings.HasSuffix(endpoint, "/versions"):
		return "plugin_versions"
	case strings.HasPrefix(endpoint, "/v1/organizations/plugins"):
		return "plugins"
	case strings.HasPrefix(endpoint, "/v1/organizations/plugin_marketplaces"):
		return "plugin_marketplaces"
	default:
		return "other"
	}
}

func apiError(response *http.Response) *APIError {
	data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	var payload struct {
		Error struct {
			Details struct {
				Code            string `json:"error_code"`
				PluginID        string `json:"plugin_id"`
				PluginVersionID string `json:"plugin_version_id"`
				SkillName       string `json:"skill_name"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &payload)
	var shouldRetry *bool
	if header := response.Header.Get("x-should-retry"); header != "" {
		if parsed, err := strconv.ParseBool(header); err == nil {
			shouldRetry = &parsed
		}
	}
	return &APIError{
		StatusCode:      response.StatusCode,
		Code:            payload.Error.Details.Code,
		PluginID:        payload.Error.Details.PluginID,
		PluginVersionID: payload.Error.Details.PluginVersionID,
		SkillName:       payload.Error.Details.SkillName,
		ShouldRetry:     shouldRetry,
		RetryAfter:      parseRetryAfter(response.Header.Get("retry-after"), time.Now()),
		RateLimits: RateLimitHeaders{
			RequestsLimit:     response.Header.Get("anthropic-ratelimit-requests-limit"),
			RequestsRemaining: response.Header.Get("anthropic-ratelimit-requests-remaining"),
			RequestsReset:     response.Header.Get("anthropic-ratelimit-requests-reset"),
		},
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return -1
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return -1
		}
		if int64(seconds) > int64(time.Duration(1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if len(value) > 0 {
		integer := true
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				integer = false
				break
			}
		}
		if integer {
			return time.Duration(1<<63 - 1)
		}
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		delay := retryAt.Sub(now)
		if delay > 0 {
			return delay
		}
		return 0
	}
	return -1
}

func wait(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
