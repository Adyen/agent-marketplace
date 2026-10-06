package api

import (
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type adminBrowserRoutes struct {
	admin     string
	authError string
}

func resolveAdminBrowserRoutes(browserBase string) adminBrowserRoutes {
	root := "/marketplace"
	if parsed, err := url.Parse(strings.TrimSpace(browserBase)); err == nil && parsed.IsAbs() && parsed.Host != "" {
		candidate := strings.TrimRight(parsed.EscapedPath(), "/")
		if !strings.HasPrefix(candidate, "//") && !strings.ContainsAny(candidate, "\\\r\n") {
			root = candidate
		}
	}
	if root == "" {
		return adminBrowserRoutes{admin: "/admin", authError: "/auth/error"}
	}
	return adminBrowserRoutes{admin: root + "/admin", authError: root + "/auth/error"}
}

var adminAcceptQualityPattern = regexp.MustCompile(`^(?:0(?:\.[0-9]{0,3})?|1(?:\.0{0,3})?)$`)

var adminBrowserErrorCodes = map[string]bool{
	"authentication_rejected":        true,
	"authentication_unavailable":     true,
	"forbidden":                      true,
	"authentication_request_invalid": true,
	"admin_unavailable":              true,
	"origin_rejected":                true,
}

func prefersAdminHTML(r *http.Request) bool {
	var html float64
	jsonExact, jsonTypeWildcard, jsonWildcard := -1.0, -1.0, -1.0
	for _, header := range r.Header.Values("Accept") {
		for _, value := range strings.Split(header, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			mediaType, params, err := mime.ParseMediaType(value)
			if err != nil || !strings.Contains(mediaType, "/") {
				return false
			}
			q := 1.0
			if raw, ok := params["q"]; ok {
				if !adminAcceptQualityPattern.MatchString(raw) {
					return false
				}
				q, _ = strconv.ParseFloat(raw, 64)
			}
			switch mediaType {
			case "text/html":
				if q > html {
					html = q
				}
			case "application/json":
				if q > jsonExact {
					jsonExact = q
				}
			case "application/*":
				if q > jsonTypeWildcard {
					jsonTypeWildcard = q
				}
			case "*/*":
				if q > jsonWildcard {
					jsonWildcard = q
				}
			}
		}
	}
	json := jsonExact
	if json < 0 {
		json = jsonTypeWildcard
	}
	if json < 0 {
		json = jsonWildcard
	}
	if json < 0 {
		json = 0
	}
	return html > 0 && html > json
}

func writeAdminOIDCError(w http.ResponseWriter, r *http.Request, routes adminBrowserRoutes, status int, code, message, next string) {
	if r.Method != http.MethodGet || !prefersAdminHTML(r) {
		writeAdminError(w, status, code, message)
		return
	}
	adminAuthHeaders(w)
	if !adminBrowserErrorCodes[code] {
		code = "sign_in_failed"
	}
	query := url.Values{"code": {code}}
	if next != "" {
		if safe, ok := validAdminNext([]string{next}, routes.admin); ok {
			query.Set("next", safe)
		}
	}
	http.Redirect(w, r, routes.authError+"?"+query.Encode(), http.StatusSeeOther)
}
