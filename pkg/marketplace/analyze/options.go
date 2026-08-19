package analyze

// Source records where the analyzed content came from.
type Source struct {
	RegistryURL string
	Ref         string
	Path        string
	ID          string
	Slug        string
	License     string
}

// Limits cap analyzer inputs so hostile archives cannot exhaust memory.
type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFiles      int
	MaxFindings   int
}

// Options configures an Analyze run.
type Options struct {
	Policy   Policy
	Source   Source
	Limits   Limits
	Analyzer string
	Version  string
}

// DefaultLimits returns conservative analyzer input limits.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:  2 * 1024 * 1024,
		MaxTotalBytes: 16 * 1024 * 1024,
		MaxFiles:      256,
		MaxFindings:   1000,
	}
}

// DefaultOptions returns a public-policy analyzer configuration.
func DefaultOptions() Options {
	return Options{
		Policy:   PublicPolicy(),
		Limits:   DefaultLimits(),
		Analyzer: "marketplace.analyze",
		Version:  AnalyzerVersionV1,
	}
}
