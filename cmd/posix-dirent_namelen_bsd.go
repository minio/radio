// +build darwin freebsd openbsd netbsd

package cmd

import "syscall"

func direntNamlen(dirent *syscall.Dirent) (uint64, error) {
	return uint64(dirent.Namlen), nil
}
