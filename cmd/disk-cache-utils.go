package cmd

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio/pkg/disk"
)

// Beginning of unix time is treated as sentinel value here.
var timeSentinel = time.Unix(0, 0).UTC()

type cacheControl struct {
	expiry       time.Time
	maxAge       int
	sMaxAge      int
	minFresh     int
	maxStale     int
	noStore      bool
	onlyIfCached bool
	noCache      bool
}

func (c cacheControl) isEmpty() bool {
	return c == cacheControl{}

}

func (c cacheControl) isStale(modTime time.Time) bool {
	if c.isEmpty() {
		return false
	}
	// response will never be stale if only-if-cached is set
	if c.onlyIfCached {
		return false
	}
	// Cache-Control value no-store indicates never cache
	if c.noStore {
		return true
	}
	// Cache-Control value no-cache indicates cache entry needs to be revalidated before
	// serving from cache
	if c.noCache {
		return true
	}
	now := time.Now()

	if c.sMaxAge > 0 && c.sMaxAge < int(now.Sub(modTime).Seconds()) {
		return true
	}
	if c.maxAge > 0 && c.maxAge < int(now.Sub(modTime).Seconds()) {
		return true
	}

	if !c.expiry.Equal(time.Time{}) && c.expiry.Before(time.Now().Add(time.Duration(c.maxStale))) {
		return true
	}

	if c.minFresh > 0 && c.minFresh <= int(now.Sub(modTime).Seconds()) {
		return true
	}

	return false
}

// returns struct with cache-control settings from user metadata.
func cacheControlOpts(o ObjectInfo) (c cacheControl) {
	m := o.UserDefined
	if o.Expires != timeSentinel {
		c.expiry = o.Expires
	}

	var headerVal string
	for k, v := range m {
		if strings.ToLower(k) == "cache-control" {
			headerVal = v
		}

	}
	if headerVal == "" {
		return
	}
	headerVal = strings.ToLower(headerVal)
	headerVal = strings.TrimSpace(headerVal)

	vals := strings.Split(headerVal, ",")
	for _, val := range vals {
		val = strings.TrimSpace(val)

		if val == "no-store" {
			c.noStore = true
			continue
		}
		if val == "only-if-cached" {
			c.onlyIfCached = true
			continue
		}
		if val == "no-cache" {
			c.noCache = true
			continue
		}
		p := strings.Split(val, "=")

		if len(p) != 2 {
			continue
		}
		if p[0] == "max-age" ||
			p[0] == "s-maxage" ||
			p[0] == "min-fresh" ||
			p[0] == "max-stale" {
			i, err := strconv.Atoi(p[1])
			if err != nil {
				return cacheControl{}
			}
			if p[0] == "max-age" {
				c.maxAge = i
			}
			if p[0] == "s-maxage" {
				c.sMaxAge = i
			}
			if p[0] == "min-fresh" {
				c.minFresh = i
			}
			if p[0] == "max-stale" {
				c.maxStale = i
			}
		}
	}
	return c
}

// backendDownError returns true if err is due to backend failure or faulty disk if in server mode
func backendDownError(err error) bool {
	_, backendDown := err.(BackendDown)
	return backendDown || IsErr(err, baseErrs...)
}

// Is a one place function which converts all os.PathError
// into a more FS object layer friendly form, converts
// known errors into their typed form for top level
// interpretation.
func osErrToFSFileErr(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return errFileNotFound
	}
	if os.IsPermission(err) {
		return errFileAccessDenied
	}
	if isSysErrNotDir(err) {
		return errFileNotFound
	}
	if isSysErrPathNotFound(err) {
		return errFileNotFound
	}
	if isSysErrTooManyFiles(err) {
		return errTooManyOpenFiles
	}
	return err
}

// getDiskInfo returns given disk information.
func getDiskInfo(diskPath string) (di disk.Info, err error) {
	if err = checkPathLength(diskPath); err == nil {
		di, err = disk.GetInfo(diskPath)
	}

	switch {
	case os.IsNotExist(err):
		err = errDiskNotFound
	case isSysErrTooLong(err):
		err = errFileNameTooLong
	case isSysErrIO(err):
		err = errFaultyDisk
	}

	return di, err
}

// checkPathLength - returns error if given path name length more than 255
func checkPathLength(pathName string) error {
	// Apple OS X path length is limited to 1016
	if runtime.GOOS == "darwin" && len(pathName) > 1016 {
		return errFileNameTooLong
	}

	if runtime.GOOS == "windows" {
		// Convert any '\' to '/'.
		pathName = filepath.ToSlash(pathName)
	}

	// Check each path segment length is > 255
	for len(pathName) > 0 && pathName != "." && pathName != SlashSeparator {
		dir, file := path.Dir(pathName), path.Base(pathName)

		if len(file) > 255 {
			return errFileNameTooLong
		}

		pathName = dir
	} // Success.
	return nil
}

// reads file cached on disk from offset upto length
func readCacheFileStream(filePath string, offset, length int64) (io.ReadCloser, error) {
	if filePath == "" || offset < 0 {
		return nil, errInvalidArgument
	}
	if err := checkPathLength(filePath); err != nil {
		return nil, err
	}

	fr, err := os.Open(filePath)
	if err != nil {
		return nil, osErrToFSFileErr(err)
	}
	// Stat to get the size of the file at path.
	st, err := fr.Stat()
	if err != nil {
		err = osErrToFSFileErr(err)
		return nil, err
	}

	if err = os.Chtimes(filePath, time.Now(), st.ModTime()); err != nil {
		return nil, err
	}

	// Verify if its not a regular file, since subsequent Seek is undefined.
	if !st.Mode().IsRegular() {
		return nil, errIsNotRegular
	}

	if err = os.Chtimes(filePath, time.Now(), st.ModTime()); err != nil {
		return nil, err
	}

	// Seek to the requested offset.
	if offset > 0 {
		_, err = fr.Seek(offset, io.SeekStart)
		if err != nil {
			return nil, err
		}
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(fr, length), Closer: fr}, nil
}
