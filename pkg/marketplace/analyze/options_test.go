package analyze

import "testing"

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.Policy.Name != "public" {
		t.Errorf("policy = %q, want public", opts.Policy.Name)
	}
	if opts.Analyzer != "marketplace.analyze" {
		t.Errorf("analyzer = %q", opts.Analyzer)
	}
	if opts.Version != AnalyzerVersionV1 {
		t.Errorf("version = %q", opts.Version)
	}
	if opts.Limits.MaxFiles == 0 {
		t.Error("MaxFiles should be set")
	}
}

func TestLimits(t *testing.T) {
	files := FileSet{
		"big": make([]byte, 3*1024*1024),
	}
	opts := DefaultOptions()
	opts.Limits.MaxFileBytes = 1024 * 1024

	err := checkLimits(files, opts.Limits)
	if err == nil {
		t.Fatal("expected MaxFileBytes error")
	}

	opts.Limits.MaxFileBytes = 0
	opts.Limits.MaxTotalBytes = 1024
	err = checkLimits(files, opts.Limits)
	if err == nil {
		t.Fatal("expected MaxTotalBytes error")
	}
}
