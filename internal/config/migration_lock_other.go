//go:build !unix

package config

import "os"

const migrationLockOpenFlags = os.O_CREATE | os.O_RDWR

func lockMigrationFile(*os.File) error {
	return nil
}

func unlockMigrationFile(*os.File) error {
	return nil
}
