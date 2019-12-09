package cmd

import (
	"os"
	"time"
)

// VolInfo - represents volume stat information.
type VolInfo struct {
	// Name of the volume.
	Name string

	// Date and time when the volume was created.
	Created time.Time
}

// FilesInfo represent a list of files, additionally
// indicates if the list is last.
type FilesInfo struct {
	Files       []FileInfo
	IsTruncated bool
}

// FileInfo - represents file stat information.
type FileInfo struct {
	// Name of the volume.
	Volume string

	// Name of the file.
	Name string

	// Date and time when the file was last modified.
	ModTime time.Time

	// Total file size.
	Size int64

	// File mode bits.
	Mode os.FileMode

	// File metadata
	Metadata map[string]string

	// All the parts per object.
	Parts []ObjectPartInfo

	Quorum int
}
