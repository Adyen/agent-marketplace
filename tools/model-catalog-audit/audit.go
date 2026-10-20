package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxSourceBytes = 2 << 20

type baseline struct {
	Checked string `json:"checked"`
	SHA256  string `json:"sha256"`
}

type fetched struct {
	Body []byte
	URL  string
}

type fetchFunc func(context.Context, string) (fetched, error)

type finding struct {
	Code    string `json:"code"`
	Model   string `json:"model,omitempty"`
	Message string `json:"message"`
}

type sourceReport struct {
	Name           string            `json:"name"`
	Harness        string            `json:"harness"`
	CatalogDomain  string            `json:"catalog_domain,omitempty"`
	URL            string            `json:"url"`
	ResolvedURL    string            `json:"resolved_url,omitempty"`
	Status         string            `json:"status"`
	BodySHA256     string            `json:"body_sha256,omitempty"`
	ObservedSHA256 string            `json:"observed_sha256,omitempty"`
	CatalogSHA256  string            `json:"catalog_sha256,omitempty"`
	Baseline       baseline          `json:"baseline"`
	Observed       observation       `json:"observed"`
	FamilyPins     map[string]string `json:"family_pins,omitempty"`
	Findings       []finding         `json:"findings"`
}

type report struct {
	Schema    int               `json:"schema"`
	CheckedAt string            `json:"checked_at"`
	Failed    bool              `json:"failed"`
	Coverage  map[string]string `json:"coverage"`
	Sources   []sourceReport    `json:"sources"`
}

type catalogRecord struct {
	Model     string   `yaml:"model"`
	Aliases   []string `yaml:"aliases"`
	Pin       string   `yaml:"pin"`
	Reasoning map[string]struct {
		Model  string `yaml:"model"`
		Effort string `yaml:"effort"`
	} `yaml:"reasoning"`
}

type catalog struct {
	Schema   int    `yaml:"schema"`
	Harness  string `yaml:"harness"`
	Evidence struct {
		Refs []string `yaml:"refs"`
	} `yaml:"evidence"`
	Models   map[string]catalogRecord `yaml:"models"`
	Families map[string]catalogRecord `yaml:"families"`
	Surfaces map[string]any           `yaml:"surfaces"`
}

const (
	catalogDir    = "pkg/harness-compilation/modelpolicy/catalogs"
	baselinesPath = "tools/model-catalog-audit/baselines.json"
)

func catalogPath(harness string) string {
	return catalogDir + "/" + harness + ".yaml"
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func readBounded(files fs.FS, name string) ([]byte, error) {
	f, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
		return nil, fmt.Errorf("input is not a bounded regular file")
	}
	return readLimited(f)
}

func readLimited(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxSourceBytes+1))
	if err == nil && len(body) > maxSourceBytes {
		err = fmt.Errorf("input exceeds %d bytes", maxSourceBytes)
	}
	return body, err
}

func audit(ctx context.Context, files fs.FS, sources []source, baselines map[string]baseline, fetch fetchFunc, now time.Time) report {
	result := report{Schema: 1, CheckedAt: now.UTC().Format(time.RFC3339), Coverage: coverage, Sources: []sourceReport{}}
	for _, s := range sources {
		r := sourceReport{Name: s.Name, Harness: s.Harness, CatalogDomain: s.Domain, URL: s.URL, Status: "no-drift", Baseline: baselines[s.Name], Findings: []finding{}}
		if s.Domain != "" {
			auditDocumentSource(ctx, files, s, &r, fetch)
			result.Failed = result.Failed || r.Status != "no-drift"
			result.Sources = append(result.Sources, r)
			continue
		}
		body, err := readBounded(files, catalogPath(s.Harness))
		var c catalog
		if err != nil || yaml.Unmarshal(body, &c) != nil || c.Schema != 1 || c.Harness != s.Harness || len(c.Surfaces) == 0 || !slices.Contains(c.Evidence.Refs, s.URL) {
			r.add("unknown", "catalog.invalid", "", "Cannot read a valid model catalog for this harness with matching first-party evidence.")
		} else {
			r.CatalogSHA256 = digest(body)
			for id, family := range c.Families {
				if family.Pin != "" {
					if r.FamilyPins == nil {
						r.FamilyPins = map[string]string{}
					}
					r.FamilyPins[id] = family.Pin
				}
			}
			auditSource(ctx, s, c, &r, fetch)
		}
		result.Failed = result.Failed || r.Status != "no-drift"
		result.Sources = append(result.Sources, r)
	}
	return result
}

func (r *sourceReport) add(status, code, model, message string) {
	if r.Status != "unknown" {
		r.Status = status
	}
	r.Findings = append(r.Findings, finding{Code: code, Model: model, Message: message})
}

func auditSource(ctx context.Context, s source, c catalog, r *sourceReport, fetch fetchFunc) {
	response, err := fetch(ctx, s.URL)
	if err != nil {
		r.add("unknown", "source.fetch-failed", "", "Public source unavailable; no freshness conclusion. Retry or inspect the source URL.")
		return
	}
	r.ResolvedURL, r.BodySHA256 = response.URL, digest(response.Body)
	observed, err := extract(s, response.Body)
	if err != nil {
		r.add("unknown", "source.parse-failed", "", "Expected inventory or documentation content was not found; review the source adapter.")
		return
	}
	r.Observed = observed
	if observed.Text != "" {
		r.ObservedSHA256 = digest([]byte(observed.Text))
	} else {
		encoded, _ := json.Marshal(observed.Models)
		r.ObservedSHA256 = digest(encoded)
	}
	if observed.Text != "" && r.Baseline.SHA256 == "" {
		r.add("unknown", "source.baseline-missing", "", "Documentation monitoring requires a reviewed baseline.")
	} else if r.Baseline.SHA256 != "" && r.Baseline.SHA256 != r.ObservedSHA256 {
		r.add("drift", "source.evidence-changed", "", "Inventory or resource documentation changed; review observed evidence before updating the baseline.")
	}
	if len(observed.Models) > 0 {
		compareInventory(c, observed.Models, r)
	}
}

func compareInventory(c catalog, models []observedModel, r *sourceReport) {
	known := map[string]bool{}
	for _, records := range []map[string]catalogRecord{c.Models, c.Families} {
		for _, record := range records {
			known[record.Model] = true
			for _, alias := range record.Aliases {
				known[alias] = true
			}
			for _, encoding := range record.Reasoning {
				known[encoding.Model] = true
			}
		}
	}
	upstream := map[string]observedModel{}
	for _, model := range models {
		upstream[model.ID] = model
		if !known[model.ID] {
			r.add("drift", "catalog.model-missing", model.ID, "Upstream inventory ID is absent from the catalog; verify resource support before adding it.")
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.Models)) {
		record := c.Models[id]
		if record.Model == "" {
			continue
		}
		model, present := upstream[record.Model]
		if !present {
			r.add("drift", "catalog.model-not-observed", record.Model, "Catalog ID is not in this source's inventory; this is not proof of retirement. Review scope and first-party evidence; do not automatically remove it.")
			continue
		}
		if len(model.Efforts) > 0 {
			for _, level := range slices.Sorted(maps.Keys(record.Reasoning)) {
				effort := record.Reasoning[level].Effort
				if effort != "" && !slices.Contains(model.Efforts, effort) {
					r.add("drift", "catalog.reasoning-not-observed", record.Model, "Catalog reasoning value "+effort+" is not listed upstream; review the resource-format contract.")
				}
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.Families)) {
		pin := c.Families[id].Pin
		if pin != "" {
			if _, present := upstream[c.Models[pin].Model]; !present {
				r.add("drift", "catalog.family-pin-review", id, "Pinned model "+pin+" is not observed upstream; review this explicit conversion policy without silently floating exact selections.")
			}
		}
	}
}

func validateBaselines(baselines map[string]baseline, sources []source) error {
	for _, s := range sources {
		b, ok := baselines[s.Name]
		if !ok {
			return fmt.Errorf("missing baseline for %s", s.Name)
		}
		if _, err := time.Parse("2006-01-02", b.Checked); err != nil {
			return fmt.Errorf("invalid baseline date for %s", s.Name)
		}
		if !validDigest(b.SHA256) {
			return fmt.Errorf("invalid baseline digest for %s", s.Name)
		}
	}
	if len(baselines) != len(sources) {
		return fmt.Errorf("unexpected baseline entries")
	}
	return nil
}
