// +build freebsd openbsd netbsd

package cmd

import "syscall"

func direntInode(dirent *syscall.Dirent) uint64 {
	return uint64(dirent.Fileno)
}
