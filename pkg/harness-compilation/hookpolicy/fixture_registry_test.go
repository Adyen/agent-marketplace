package hookpolicy

import (
	"fmt"
	"maps"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func WithoutRepositoryBaseForTest(target target.Target) (restore func(), err error) {
	original := shipped
	loaded, err := original()
	if err != nil {
		return nil, err
	}
	catalogs := maps.Clone(loaded.catalogs)
	selected, ok := catalogs[target]
	if !ok {
		return nil, fmt.Errorf("no shipped hook catalog for target %s", target)
	}
	selected.RepositoryBase = nil
	catalogs[target] = selected
	shipped = func() (*registry, error) {
		return &registry{catalogs: catalogs}, nil
	}
	return func() { shipped = original }, nil
}
