// Package ingest resolves the configured analyzer set for a registry revision.
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// resolvedAnalyzer pairs a resolved registration with the canonical config digest
// that identifies its inputs.
type resolvedAnalyzer struct {
	Registration analyze.Registration
	ConfigDigest store.PolicyDigest
}

// resolvedAnalyzerSet is the complete ordered analyzer configuration for one
// ingest run.
type resolvedAnalyzerSet struct {
	Entries         []resolvedAnalyzer
	AuditSetVersion string
}

// Registrations returns the analyzer registrations in order.
func (s resolvedAnalyzerSet) Registrations() []analyze.Registration {
	regs := make([]analyze.Registration, len(s.Entries))
	for i, e := range s.Entries {
		regs[i] = e.Registration
	}
	return regs
}

const rulepackID = "marketplace.rulepack"

// fsReader abstracts filesystem reads so resolveAnalyzers can be tested without
// touching the real filesystem.
type fsReader interface {
	readFile(path string, maxBytes int64) ([]byte, error)
}

type osFS struct{}

func (osFS) readFile(path string, maxBytes int64) ([]byte, error) {
	return readBoundedFile(path, maxBytes)
}

// resolveAnalyzers translates the operator analyzer configuration into a concrete
// ordered set of analyzer registrations and their config digests.
//
// nil policy.Analyzers resolves all built-ins in canonical order. An explicit
// empty slice resolves none. A configured list preserves authored order after
// dropping enabled:false entries.
func resolveAnalyzers(policy config.Policy, effective analyze.Policy, limits config.Limits) (resolvedAnalyzerSet, error) {
	return resolveAnalyzersFS(policy, effective, limits, osFS{})
}

func resolveAnalyzersFS(policy config.Policy, effective analyze.Policy, limits config.Limits, fs fsReader) (resolvedAnalyzerSet, error) {
	if policy.Analyzers == nil {
		return resolveDefaults(effective)
	}
	if len(policy.Analyzers) == 0 {
		return resolvedAnalyzerSet{AuditSetVersion: computeAuditSetVersion(nil)}, nil
	}

	seen := make(map[string]struct{})
	var entries []resolvedAnalyzer
	for _, cfg := range policy.Analyzers {
		if _, ok := seen[cfg.ID]; ok {
			return resolvedAnalyzerSet{}, fmt.Errorf("duplicate analyzer id %q", cfg.ID)
		}
		seen[cfg.ID] = struct{}{}

		if cfg.Enabled != nil && !*cfg.Enabled {
			continue
		}

		var ra resolvedAnalyzer
		var err error
		switch {
		case isBuiltIn(cfg.ID):
			ra, err = resolveBuiltin(cfg, effective)
		case cfg.ID == rulepackID:
			ra, err = resolveRulepack(cfg, effective, limits, fs)
		default:
			ra, err = resolveRunner(cfg, effective, limits)
		}
		if err != nil {
			return resolvedAnalyzerSet{}, err
		}
		entries = append(entries, ra)
	}

	return resolvedAnalyzerSet{
		Entries:         entries,
		AuditSetVersion: computeAuditSetVersion(entries),
	}, nil
}

func resolveDefaults(effective analyze.Policy) (resolvedAnalyzerSet, error) {
	builtins := analyze.Builtins()
	registrations := analyze.DefaultRegistrations()
	analyzerByID := make(map[string]analyze.Analyzer, len(registrations))
	for _, r := range registrations {
		analyzerByID[r.Analyzer.ID()] = r.Analyzer
	}

	var entries []resolvedAnalyzer
	for _, b := range builtins {
		a, ok := analyzerByID[b.ID]
		if !ok {
			return resolvedAnalyzerSet{}, fmt.Errorf("built-in analyzer %q not found", b.ID)
		}
		ra, err := makeResolvedBuiltin(a, b, effective)
		if err != nil {
			return resolvedAnalyzerSet{}, err
		}
		entries = append(entries, ra)
	}

	return resolvedAnalyzerSet{
		Entries:         entries,
		AuditSetVersion: computeAuditSetVersion(entries),
	}, nil
}

func makeResolvedBuiltin(a analyze.Analyzer, meta analyze.BuiltinMetadata, effective analyze.Policy) (resolvedAnalyzer, error) {
	reg := analyze.Registration{
		Analyzer: a,
		Role:     meta.DefaultRole,
		Config:   nil,
		Policy:   effective,
	}
	digest, err := builtinConfigDigest(meta.ID, meta.Version, meta.DefaultRole, nil, effective, meta.FindingCodes)
	if err != nil {
		return resolvedAnalyzer{}, err
	}
	return resolvedAnalyzer{Registration: reg, ConfigDigest: digest}, nil
}

func resolveBuiltin(cfg config.Analyzer, effective analyze.Policy) (resolvedAnalyzer, error) {
	builtins := analyze.Builtins()
	var meta analyze.BuiltinMetadata
	for _, b := range builtins {
		if b.ID == cfg.ID {
			meta = b
			break
		}
	}
	if meta.ID == "" {
		return resolvedAnalyzer{}, fmt.Errorf("unknown built-in analyzer %q", cfg.ID)
	}

	registrations := analyze.DefaultRegistrations()
	var a analyze.Analyzer
	for _, r := range registrations {
		if r.Analyzer.ID() == cfg.ID {
			a = r.Analyzer
			break
		}
	}
	if a == nil {
		return resolvedAnalyzer{}, fmt.Errorf("built-in analyzer %q not found", cfg.ID)
	}

	role := meta.DefaultRole
	if cfg.Role != "" {
		role = cfg.Role
	}

	reg := analyze.Registration{
		Analyzer: a,
		Role:     role,
		Config:   cloneAnalyzerConfig(cfg.Config),
		Policy:   effective,
	}
	digest, err := builtinConfigDigest(meta.ID, meta.Version, role, cfg.Config, effective, meta.FindingCodes)
	if err != nil {
		return resolvedAnalyzer{}, err
	}
	return resolvedAnalyzer{Registration: reg, ConfigDigest: digest}, nil
}

func resolveRulepack(cfg config.Analyzer, effective analyze.Policy, limits config.Limits, fs fsReader) (resolvedAnalyzer, error) {
	if cfg.Rulepack == nil || cfg.Rulepack.Path == "" {
		return resolvedAnalyzer{}, fmt.Errorf("rulepack analyzer %q requires a rulepack path", cfg.ID)
	}

	data, err := fs.readFile(cfg.Rulepack.Path, limits.FileBytes)
	if err != nil {
		return resolvedAnalyzer{}, fmt.Errorf("read rulepack %q: %w", cfg.Rulepack.Path, err)
	}
	fileDigest := sha256Hex(data)

	version := analyze.AnalyzerRulepackVersionV1
	rp, err := analyze.NewRulepack(rulepackID, version, data)
	if err != nil {
		return resolvedAnalyzer{}, fmt.Errorf("compile rulepack %q: %w", cfg.Rulepack.Path, err)
	}

	role := cfg.Role
	if role == "" {
		role = analyze.RoleGating
	}

	// The rulepack's default policy warns for every rule; the operator's
	// effective policy may override specific codes.
	regPolicy := mergePolicies(effective, rp.Policy())

	reg := analyze.Registration{
		Analyzer: rp,
		Role:     role,
		Config:   cloneAnalyzerConfig(cfg.Config),
		Policy:   regPolicy,
	}

	codes := rp.Codes()
	digest, err := rulepackConfigDigest(cfg.ID, version, role, cfg.Config, fileDigest, codes, regPolicy)
	if err != nil {
		return resolvedAnalyzer{}, err
	}
	return resolvedAnalyzer{Registration: reg, ConfigDigest: digest}, nil
}

func resolveRunner(cfg config.Analyzer, effective analyze.Policy, limits config.Limits) (resolvedAnalyzer, error) {
	if cfg.Run == nil {
		return resolvedAnalyzer{}, fmt.Errorf("analyzer %q requires a run block", cfg.ID)
	}
	if cfg.Role == "" {
		return resolvedAnalyzer{}, fmt.Errorf("analyzer %q requires a role", cfg.ID)
	}

	timeout := time.Duration(cfg.Run.Timeout)
	if timeout <= 0 {
		return resolvedAnalyzer{}, fmt.Errorf("runner %q timeout must be positive", cfg.ID)
	}

	maxOutput := limits.FileBytes
	if maxOutput <= 0 {
		maxOutput = 1 << 20
	}

	rc := analyze.RunnerConfig{
		Command:              cfg.Run.Command,
		Args:                 append([]string(nil), cfg.Run.Args...),
		Timeout:              timeout,
		EnvPassthrough:       append([]string(nil), cfg.Run.EnvPassthrough...),
		ImplementationDigest: cfg.Run.ImplementationDigest,
		MaxOutputBytes:       maxOutput,
	}

	runner, err := analyze.NewRunner(cfg.ID, cfg.Version, rc)
	if err != nil {
		return resolvedAnalyzer{}, fmt.Errorf("runner %q: %w", cfg.ID, err)
	}

	reg := analyze.Registration{
		Analyzer: runner,
		Role:     cfg.Role,
		Config:   cloneAnalyzerConfig(cfg.Config),
		Policy:   effective,
	}

	digest, err := runnerConfigDigest(cfg.ID, cfg.Version, cfg.Role, cfg.Config, rc)
	if err != nil {
		return resolvedAnalyzer{}, err
	}
	return resolvedAnalyzer{Registration: reg, ConfigDigest: digest}, nil
}

func isBuiltIn(id string) bool {
	for _, b := range analyze.Builtins() {
		if b.ID == id {
			return true
		}
	}
	return false
}

func mergePolicies(overrides, base analyze.Policy) analyze.Policy {
	outcomes := make(map[string]string)
	if base.Outcomes != nil {
		for k, v := range base.Outcomes {
			outcomes[k] = v
		}
	}
	if overrides.Outcomes != nil {
		for k, v := range overrides.Outcomes {
			outcomes[k] = v
		}
	}
	return analyze.Policy{Name: overrides.Name, Outcomes: outcomes}
}

func cloneAnalyzerConfig(cfg map[string]any) analyze.AnalyzerConfig {
	if cfg == nil {
		return nil
	}
	return deepCloneMap(cfg)
}

func deepCloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCloneValue(v)
	}
	return out
}

func deepCloneList(list []any) []any {
	out := make([]any, len(list))
	for i, v := range list {
		out[i] = deepCloneValue(v)
	}
	return out
}

func deepCloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return deepCloneMap(x)
	case []any:
		return deepCloneList(x)
	default:
		// Preserve scalar values as-is. JSON/YAML decoders may produce bool,
		// string, int, uint, float64, or nil.
		return v
	}
}

type codeOutcome struct {
	Code    string `json:"code"`
	Outcome string `json:"outcome"`
}

type analyzerPolicyEntry struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Role         string `json:"role"`
	ConfigDigest string `json:"config_digest"`
}

func builtinConfigDigest(id, version, role string, cfg map[string]any, effective analyze.Policy, codes []string) (store.PolicyDigest, error) {
	sortedCodes := append([]string(nil), codes...)
	sort.Strings(sortedCodes)

	outcomes := make([]codeOutcome, 0, len(sortedCodes))
	for _, code := range sortedCodes {
		outcomes = append(outcomes, codeOutcome{Code: code, Outcome: effective.OutcomeFor(code)})
	}

	input := struct {
		ID       string         `json:"id"`
		Version  string         `json:"version"`
		Role     string         `json:"role"`
		Config   map[string]any `json:"config,omitempty"`
		Outcomes []codeOutcome  `json:"outcomes"`
	}{
		ID:       id,
		Version:  version,
		Role:     role,
		Config:   cloneAnalyzerConfig(cfg),
		Outcomes: outcomes,
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal builtin config digest: %w", err)
	}
	sum := sha256.Sum256(payload)
	return store.PolicyDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func rulepackConfigDigest(id, version, role string, cfg map[string]any, fileDigest string, codes []string, policy analyze.Policy) (store.PolicyDigest, error) {
	sortedCodes := append([]string(nil), codes...)
	sort.Strings(sortedCodes)

	outcomes := make([]codeOutcome, 0, len(sortedCodes))
	for _, code := range sortedCodes {
		outcomes = append(outcomes, codeOutcome{Code: code, Outcome: policy.OutcomeFor(code)})
	}

	input := struct {
		ID      string         `json:"id"`
		Version string         `json:"version"`
		Role    string         `json:"role"`
		Config  map[string]any `json:"config,omitempty"`
		Runtime struct {
			FileDigest string        `json:"file_digest"`
			Outcomes   []codeOutcome `json:"outcomes"`
		} `json:"runtime"`
	}{
		ID:      id,
		Version: version,
		Role:    role,
		Config:  cloneAnalyzerConfig(cfg),
	}
	input.Runtime.FileDigest = fileDigest
	input.Runtime.Outcomes = outcomes

	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal rulepack config digest: %w", err)
	}
	sum := sha256.Sum256(payload)
	return store.PolicyDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func runnerConfigDigest(id, version, role string, cfg map[string]any, rc analyze.RunnerConfig) (store.PolicyDigest, error) {
	input := struct {
		ID      string         `json:"id"`
		Version string         `json:"version"`
		Role    string         `json:"role"`
		Config  map[string]any `json:"config,omitempty"`
		Runtime struct {
			Command              string   `json:"command"`
			Args                 []string `json:"args"`
			TimeoutNs            int64    `json:"timeout_ns"`
			Env                  []string `json:"env"`
			ImplementationDigest string   `json:"implementation_digest"`
			MaxOutputBytes       int64    `json:"max_output_bytes"`
		} `json:"runtime"`
	}{
		ID:      id,
		Version: version,
		Role:    role,
		Config:  cloneAnalyzerConfig(cfg),
	}
	input.Runtime.Command = rc.Command
	input.Runtime.Args = append([]string(nil), rc.Args...)
	input.Runtime.TimeoutNs = rc.Timeout.Nanoseconds()
	env := append([]string(nil), rc.EnvPassthrough...)
	sort.Strings(env)
	input.Runtime.Env = env
	input.Runtime.ImplementationDigest = rc.ImplementationDigest
	input.Runtime.MaxOutputBytes = rc.MaxOutputBytes

	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal runner config digest: %w", err)
	}
	sum := sha256.Sum256(payload)
	return store.PolicyDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func computeAuditSetVersion(entries []resolvedAnalyzer) string {
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		items = append(items, fmt.Sprintf("%s@%s+%s",
			e.Registration.Analyzer.ID(),
			e.Registration.Analyzer.Version(),
			e.ConfigDigest,
		))
	}
	sort.Strings(items)
	h := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return "audits:sha256:" + hex.EncodeToString(h[:])
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
