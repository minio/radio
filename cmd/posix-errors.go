package cmd

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

// Check if the given error corresponds to ENOTDIR (is not a directory).
func isSysErrNotDir(err error) bool {
	return errors.Is(err, syscall.ENOTDIR)
}

// Check if the given error corresponds to ENOTEMPTY for unix
// and ERROR_DIR_NOT_EMPTY for windows (directory not empty).
func isSysErrNotEmpty(err error) bool {
	if errors.Is(err, syscall.ENOTEMPTY) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if runtime.GOOS == globalWindowsOSName {
			var errno syscall.Errno
			if errors.As(pathErr.Err, &errno) {
				// ERROR_DIR_NOT_EMPTY
				return errno == 0x91
			}
		}
	}
	return false
}

// Check if the given error corresponds to the specific ERROR_PATH_NOT_FOUND for windows
func isSysErrPathNotFound(err error) bool {
	if runtime.GOOS != globalWindowsOSName {
		return false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) {
			// ERROR_PATH_NOT_FOUND
			return errno == 0x03
		}
	}
	return false
}
