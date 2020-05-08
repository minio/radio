// +build linux darwin
// +build !appengine

package cmd

import "syscall"

func direntInode(dirent *syscall.Dirent) uint64 {
	return uint64(dirent.Ino)
}
