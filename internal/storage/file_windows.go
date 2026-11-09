package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func validateDatabaseFile(file *os.File) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("SQLite state must be a regular file without hard links")
	}
	return nil
}
