//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

func validateDatabaseFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return errors.New("SQLite state must be a regular file without hard links")
	}
	return nil
}
