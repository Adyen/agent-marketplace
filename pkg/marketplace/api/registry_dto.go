package api

import (
	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// RegistryDTO is the public representation of an approved registry. It deliberately
// contains only discovery metadata: credentials, policy internals, and refresh
// diagnostics are server-side fields and are not part of the public API.
type RegistryDTO struct {
	ID          store.RegistryID      `json:"id"`
	Slug        string                `json:"slug"`
	Aliases     []string              `json:"aliases,omitempty"`
	Origins     []string              `json:"origins"`
	Ref         string                `json:"ref"`
	Roots       []store.RegistryRoot  `json:"roots"`
	Acquisition store.AcquisitionMode `json:"acquisition"`
	Publisher   *PublisherDTO         `json:"publisher,omitempty"`
	License     string                `json:"license,omitempty"`
	Listed      bool                  `json:"listed"`
}

// PublisherDTO is the public publisher metadata. Unsafe legacy URLs are
// omitted while the non-sensitive publisher name remains available.
type PublisherDTO struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// PublicRegistry is retained as a descriptive alias for callers that prefer the
// security boundary to be explicit in their code.
type PublicRegistry = RegistryDTO

func registryDTO(src store.Registry) RegistryDTO {
	origins := make([]string, len(src.Origins))
	for i, origin := range src.Origins {
		origins[i] = publicurl.Redact(origin)
	}
	roots := make([]store.RegistryRoot, len(src.Roots))
	copy(roots, src.Roots)
	var publisher *PublisherDTO
	publisherURL := ""
	if publicurl.Validate(src.Publisher.URL) == nil {
		publisherURL = publicurl.Sanitize(src.Publisher.URL)
	}
	if src.Publisher.Name != "" || publisherURL != "" {
		publisher = &PublisherDTO{Name: src.Publisher.Name, URL: publisherURL}
	}
	return RegistryDTO{
		ID:          src.ID,
		Slug:        src.Slug,
		Aliases:     append([]string(nil), src.Aliases...),
		Origins:     origins,
		Ref:         src.Ref,
		Roots:       roots,
		Acquisition: src.Acquisition,
		Publisher:   publisher,
		License:     src.License,
		Listed:      src.Listed,
	}
}
