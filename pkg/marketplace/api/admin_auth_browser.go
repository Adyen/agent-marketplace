package api

import (
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const adminAuthErrorPage = "/marketplace/auth/error"

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
			if err != nil {
				return false
			}
			q := 1.0
			if raw, ok := params["q"]; ok {
				q, err = strconv.ParseFloat(raw, 64)
				if err != nil || !(q >= 0 && q <= 1) {
					return false
				}
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

func writeAdminOIDCError(w http.ResponseWriter, r *http.Request, status int, code, message, next string) {
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
		if safe, ok := validAdminNext([]string{next}); ok {
			query.Set("next", safe)
		}
	}
	http.Redirect(w, r, adminAuthErrorPage+"?"+query.Encode(), http.StatusSeeOther)
}
