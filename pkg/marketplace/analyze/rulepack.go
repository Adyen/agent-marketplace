package analyze

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// Rulepack is an in-process analyzer that loads operator-authored YAML rules
// and matches them against public text samples and support files.
type Rulepack struct {
	id      string
	version string
	rules   []compiledRule
	policy  Policy
}

// NewRulepack compiles an analyzer from a YAML rulepack. It validates the
// schema, compiles every regular expression, and pre-computes a default policy
// that maps each rule code to OutcomeWarn. The engine applies the registration
// policy after the rulepack returns its raw findings.
func NewRulepack(id, version string, data []byte) (*Rulepack, error) {
	if err := safeAnalyzerIdentifier(id); err != nil {
		return nil, fmt.Errorf("rulepack id: %w", err)
	}
	if version == "" {
		return nil, fmt.Errorf("rulepack version must not be empty")
	}
	rp := &Rulepack{
		id:      id,
		version: version,
	}
	doc, err := parseRulepackYAML(data)
	if err != nil {
		return nil, err
	}
	if doc.APIVersion != "agent-marketplace.analyzer/v1" {
		return nil, fmt.Errorf("unsupported apiVersion %q", doc.APIVersion)
	}

	seen := make(map[string]bool)
	outcomes := make(map[string]string)
	for _, r := range doc.Rules {
		code := r.Code
		if !safeCodeIdentifier(code) {
			return nil, fmt.Errorf("invalid code identifier %q", code)
		}
		if !strings.HasPrefix(code, "marketplace.rulepack.") {
			code = "marketplace.rulepack." + code
		}
		if seen[code] {
			return nil, fmt.Errorf("duplicate code %q", code)
		}
		seen[code] = true

		if r.Message == "" {
			return nil, fmt.Errorf("rule %q has empty message", code)
		}
		if r.Pattern == "" {
			return nil, fmt.Errorf("rule %q has empty pattern", code)
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %q has invalid pattern: %w", code, err)
		}

		var kinds map[ir.ResourceKind]bool
		for _, k := range r.Kinds {
			kind, err := kindFromString(k)
			if err != nil {
				return nil, fmt.Errorf("rule %q has invalid kind %q: %w", code, k, err)
			}
			if kinds == nil {
				kinds = make(map[ir.ResourceKind]bool)
			}
			kinds[kind] = true
		}

		rp.rules = append(rp.rules, compiledRule{
			code:    code,
			re:      re,
			message: r.Message,
			kinds:   kinds,
		})
		outcomes[code] = OutcomeWarn
	}
	rp.policy = Policy{Outcomes: outcomes}

	return rp, nil
}

func (r *Rulepack) ID() string               { return r.id }
func (r *Rulepack) Version() string          { return r.version }
func (r *Rulepack) Kinds() []ir.ResourceKind { return nil }

// Codes returns the sorted rule codes compiled into the rulepack.
func (r *Rulepack) Codes() []string {
	codes := make([]string, 0, len(r.rules))
	for _, rule := range r.rules {
		codes = append(codes, rule.code)
	}
	sort.Strings(codes)
	return codes
}

// Policy returns a deep copy of the rulepack's default per-code warn policy.
// Callers may pass this to a Registration or supply their own policy.
func (r *Rulepack) Policy() Policy {
	outcomes := make(map[string]string, len(r.policy.Outcomes))
	for k, v := range r.policy.Outcomes {
		outcomes[k] = v
	}
	return Policy{Name: r.policy.Name, Outcomes: outcomes}
}

func (r *Rulepack) Analyze(ctx context.Context, s Subject, _ AnalyzerConfig) (Audit, error) {
	if ctx.Err() != nil {
		return Audit{}, ctx.Err()
	}
	if err := validateSubject(&s); err != nil {
		return Audit{}, err
	}

	start := time.Now()
	kind := s.Resource.Kind()

	maxFindings := s.Limits.MaxFindings
	if maxFindings <= 0 {
		maxFindings = DefaultLimits().MaxFindings
	}

	var findings []Finding
samples:
	for _, sample := range s.samples {
		for _, rule := range r.rules {
			if len(findings) >= maxFindings {
				break samples
			}
			if rule.kinds != nil && !rule.kinds[kind] {
				continue
			}
			for _, loc := range boundedMatchLocations(rule.re, sample.content, maxFindings-len(findings)) {
				findings = append(findings, Finding{
					Analyzer:        r.id,
					AnalyzerVersion: r.version,
					Code:            rule.code,
					Message:         rule.message,
					SourceRange:     SourceRangeAt(sample.path, sample.content, loc[0]),
				})
			}
		}
	}

	duration := time.Since(start)
	slog.Debug("rulepack audit raw",
		"analyzer", r.id,
		"rules", len(r.rules),
		"findings", len(findings))

	return Audit{
		AnalyzerID:      r.id,
		AnalyzerVersion: r.version,
		Findings:        findings,
		Duration:        duration,
	}, nil
}

// boundedMatchLocations enumerates match offsets with bounded offset-stepping
// so a broad pattern cannot allocate an unbounded match slice before the
// findings cap is applied. Empty matches advance by one byte to guarantee
// progress.
func boundedMatchLocations(re *regexp.Regexp, content string, max int) [][]int {
	if max <= 0 {
		return nil
	}
	// Match count cannot exceed len(content)+1, so never pre-allocate beyond it.
	capacity := max
	if len(content)+1 < capacity {
		capacity = len(content) + 1
	}
	locs := make([][]int, 0, capacity)
	for offset := 0; offset <= len(content) && len(locs) < max; {
		loc := re.FindStringIndex(content[offset:])
		if loc == nil {
			break
		}
		locs = append(locs, []int{loc[0] + offset, loc[1] + offset})
		if loc[1] == 0 {
			offset += loc[0] + 1
		} else {
			offset += loc[1]
		}
	}
	return locs
}

type compiledRule struct {
	code    string
	re      *regexp.Regexp
	message string
	kinds   map[ir.ResourceKind]bool
}

type rulepackDoc struct {
	APIVersion string
	Rules      []rulepackRule
}

type rulepackRule struct {
	Code    string
	Pattern string
	Message string
	Kinds   []string
}

// parseRulepackYAML is a strict YAML decoder that rejects unknown fields,
// duplicate keys, trailing documents, and invalid scalar types while preserving
// rule order.
func parseRulepackYAML(data []byte) (rulepackDoc, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return rulepackDoc{}, err
	}
	var next yaml.Node
	if err := dec.Decode(&next); err != io.EOF {
		return rulepackDoc{}, fmt.Errorf("rulepack contains trailing documents")
	}
	if len(root.Content) == 0 {
		return rulepackDoc{}, fmt.Errorf("empty rulepack document")
	}
	if len(root.Content) > 1 {
		return rulepackDoc{}, fmt.Errorf("rulepack contains trailing documents")
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return rulepackDoc{}, fmt.Errorf("rulepack root must be a mapping")
	}

	doc := rulepackDoc{}
	topSeen := make(map[string]bool)
	for i := 0; i < len(m.Content); i += 2 {
		key := m.Content[i].Value
		if key == "" {
			return rulepackDoc{}, fmt.Errorf("rulepack top-level key is not a scalar")
		}
		if topSeen[key] {
			return rulepackDoc{}, fmt.Errorf("duplicate rulepack field %q", key)
		}
		topSeen[key] = true

		val := m.Content[i+1]
		switch key {
		case "apiVersion":
			if val.Kind != yaml.ScalarNode {
				return rulepackDoc{}, fmt.Errorf("apiVersion must be a scalar")
			}
			doc.APIVersion = val.Value
		case "rules":
			if val.Kind != yaml.SequenceNode {
				return rulepackDoc{}, fmt.Errorf("rules must be a sequence")
			}
			for _, rn := range val.Content {
				if rn.Kind != yaml.MappingNode {
					return rulepackDoc{}, fmt.Errorf("each rule must be a mapping")
				}
				r := rulepackRule{}
				ruleSeen := make(map[string]bool)
				for j := 0; j < len(rn.Content); j += 2 {
					rkey := rn.Content[j].Value
					if rkey == "" {
						return rulepackDoc{}, fmt.Errorf("rule key is not a scalar")
					}
					if ruleSeen[rkey] {
						return rulepackDoc{}, fmt.Errorf("duplicate rule field %q", rkey)
					}
					ruleSeen[rkey] = true

					rval := rn.Content[j+1]
					switch rkey {
					case "code":
						if rval.Kind != yaml.ScalarNode {
							return rulepackDoc{}, fmt.Errorf("rule code must be a scalar")
						}
						r.Code = rval.Value
					case "pattern":
						if rval.Kind != yaml.ScalarNode {
							return rulepackDoc{}, fmt.Errorf("rule pattern must be a scalar")
						}
						r.Pattern = rval.Value
					case "message":
						if rval.Kind != yaml.ScalarNode {
							return rulepackDoc{}, fmt.Errorf("rule message must be a scalar")
						}
						r.Message = rval.Value
					case "kinds":
						if rval.Kind != yaml.SequenceNode {
							return rulepackDoc{}, fmt.Errorf("rule kinds must be a sequence of strings")
						}
						for _, kn := range rval.Content {
							if kn.Kind != yaml.ScalarNode {
								return rulepackDoc{}, fmt.Errorf("rule kinds must be a sequence of strings")
							}
							r.Kinds = append(r.Kinds, kn.Value)
						}
					default:
						return rulepackDoc{}, fmt.Errorf("unknown rule field %q", rkey)
					}
				}
				doc.Rules = append(doc.Rules, r)
			}
		default:
			return rulepackDoc{}, fmt.Errorf("unknown rulepack field %q", key)
		}
	}
	return doc, nil
}

func safeCodeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isSafeCodeIdentifierByte(c) {
			return false
		}
	}
	return true
}

func isSafeCodeIdentifierByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
}

func kindFromString(s string) (ir.ResourceKind, error) {
	switch strings.ToLower(s) {
	case "skill":
		return ir.KindSkill, nil
	case "command":
		return ir.KindCommand, nil
	case "subagent":
		return ir.KindSubagent, nil
	case "hook":
		return ir.KindHook, nil
	case "mcpserver", "mcp":
		return ir.KindMcpServer, nil
	case "collection":
		return ir.KindCollection, nil
	case "rule":
		return ir.KindRule, nil
	}
	return 0, fmt.Errorf("unknown resource kind %q", s)
}
