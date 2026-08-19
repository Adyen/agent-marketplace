package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateID returns a roughly-sortable, unique identifier without adding an
// external dependency. It is used for job IDs when the caller does not supply one.
func generateID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a timestamp-based identifier if the entropy source fails.
		return fmt.Sprintf("job-%d-%d", time.Now().UTC().UnixMicro(), time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UTC().UnixMicro(), hex.EncodeToString(b[:]))
}
