package codemode

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Finding struct {
	Rule     string `json:"rule,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message"`
}

type Resource struct {
	Ref         string    `json:"ref"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags,omitempty"`
	Registry    string    `json:"registry"`
	Revision    string    `json:"revision"`
	Assessment  string    `json:"assessment"`
	Findings    []Finding `json:"findings,omitempty"`

	Tools any `json:"tools,omitempty"`

	origin *Origin `json:"-"`
}

type Origin struct {
	ContentDir string
	File       string

	Server *ServerDef

	Marketplace *MarketplaceOrigin

	SourceURL  string
	SourcePath string
}

type ServerDef struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Digest  string            `json:"digest"`

	ContentDigest string `json:"content_digest,omitempty"`
}

type Snapshot struct {
	Resources []Resource
	Stale     bool
	AsOf      time.Time

	indexOnce sync.Once
	byRef     map[string]*Resource
}

func (r *Resource) ServerDef() *ServerDef {
	if r == nil || r.origin == nil {
		return nil
	}
	return r.origin.Server
}

func (r *Resource) Origin() *Origin {
	if r == nil {
		return nil
	}
	return r.origin
}

func (r *Resource) SetOrigin(o *Origin) { r.origin = o }

func (s *Snapshot) Find(ref string) *Resource {
	if s == nil {
		return nil
	}
	s.indexOnce.Do(func() {
		s.byRef = make(map[string]*Resource, len(s.Resources))
		for i := range s.Resources {
			s.byRef[s.Resources[i].Ref] = &s.Resources[i]
		}
	})
	return s.byRef[ref]
}

type SnapshotStore struct {
	current atomic.Value
}

func (s *SnapshotStore) Load() *Snapshot {
	if v := s.current.Load(); v != nil {
		return v.(*Snapshot)
	}
	return &Snapshot{Stale: true}
}

func (s *SnapshotStore) Store(snap *Snapshot) {
	s.current.Store(snap)
}

func (s *Snapshot) catalogJS() []map[string]any {
	out := make([]map[string]any, 0, len(s.Resources))
	for i := range s.Resources {
		out = append(out, resourceJS(&s.Resources[i]))
	}
	return out
}

func resourceJS(r *Resource) map[string]any {
	m := map[string]any{
		"ref":         r.Ref,
		"kind":        r.Kind,
		"name":        r.Name,
		"description": r.Description,
		"registry":    r.Registry,
		"revision":    r.Revision,
		"assessment":  r.Assessment,
	}
	if len(r.Tags) > 0 {
		m["tags"] = r.Tags
	}
	if len(r.Findings) > 0 {
		m["findings"] = r.Findings
	}
	if r.Tools != nil {
		m["tools"] = r.Tools
	}
	return m
}

func SanitizeRef(ref string) bool {
	if ref == "" || len(ref) > 512 {
		return false
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == ".." || seg == "" {
			return false
		}
	}
	return !strings.ContainsAny(ref, "\x00\r\n")
}
