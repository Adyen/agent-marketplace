package ingest

import "github.com/Adyen/agent-marketplace/internal/catalog"

var ErrHookSupportUnsafe = catalog.ErrHookSupportUnsafe

func readHookSupportFile(root, relative string) ([]byte, bool, error) {
	return catalog.ReadHookSupportFile(root, relative)
}
