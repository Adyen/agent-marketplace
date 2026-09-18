package config

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

type Management struct {
	Mode           string                       `yaml:"mode"`
	AllowedOrigins []string                     `yaml:"allowed_origins"`
	Profiles       map[string]CredentialProfile `yaml:"profiles"`
}

type CredentialProfile struct {
	AllowedOrigins []string    `yaml:"allowed_origins"`
	Credentials    Credentials `yaml:"credentials"`
	CAFile         string      `yaml:"ca_file"`
}

func (m Management) Validate() error {
	if m.Mode != "" && m.Mode != "file" && m.Mode != "database" {
		return errors.New("management.mode must be file or database")
	}
	validOrigins := func(origins []string) bool {
		for _, origin := range origins {
			u, err := url.Parse(origin)
			if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "ssh") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
				return false
			}
		}
		return true
	}
	if !validOrigins(m.AllowedOrigins) {
		return errors.New("management.allowed_origins must contain protocol and authority only")
	}
	for id, p := range m.Profiles {
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\r\n\x00 ") || !validOrigins(p.AllowedOrigins) || len(p.AllowedOrigins) == 0 {
			return errors.New("management profile identity or allowed origins invalid")
		}
		if len(validateCredentials(0, p.Credentials)) != 0 || (p.CAFile != "" && !filepath.IsAbs(p.CAFile)) {
			return errors.New("management profile credential or trust reference invalid")
		}
	}
	return nil
}
