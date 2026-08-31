//go:build !unix

package receiver

import (
	"fmt"
	"os"
)

const leaseOpenFlags = os.O_CREATE | os.O_RDWR

func tryLeaseFile(*os.File) error {
	return fmt.Errorf("receiver storage locking is unsupported on this platform")
}

func unlockLeaseFile(*os.File) error {
	return nil
}
