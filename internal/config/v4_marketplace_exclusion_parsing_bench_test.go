package config

import (
	"errors"
	"testing"
)

func BenchmarkV4MPExclusionRefusalCPU(b *testing.B) {
	for _, exclusion := range []string{"*", "tools/*", "tools/skill/*", "tools/collection/item"} {
		b.Run(exclusion, func(b *testing.B) {
			cfg := v4MPExclusionParsingPublicFixture(b, exclusion)
			var out *Config
			var err error
			b.ReportAllocs()
			for b.Loop() {
				out, err = cfg.ToLegacyConfig(userLayer)
			}
			var refusal *V4ValidationError
			if out != nil || !errors.As(err, &refusal) || refusal.Code != "v4.marketplace-runtime-unsupported" ||
				refusal.Path != "resources.m.exclude" ||
				refusal.Msg != "Marketplace exclusions require an exact non-collection registry/kind/name locator" {
				b.Fatalf("Marketplace refusal changed: %v", err)
			}
		})
	}
}
