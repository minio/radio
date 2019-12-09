package cmd

import "sync"

const (
	bucketMetaPrefix = "buckets"

	// ETag (hex encoded md5sum) of empty string.
	emptyETag = "d41d8cd98f00b204e9800998ecf8427e"
)

// Global object layer mutex, used for safely updating object layer.
var globalObjLayerMutex *sync.RWMutex

// Global object layer, only accessed by globalObjectAPI.
var globalObjectAPI ObjectLayer

//Global cacheObjects, only accessed by newCacheObjectsFn().
var globalCacheObjectAPI CacheObjectLayer

func init() {
	// Initialize this once per server initialization.
	globalObjLayerMutex = &sync.RWMutex{}
}

// Checks if the object is a directory, this logic uses
// if size == 0 and object ends with SlashSeparator then
// returns true.
func isObjectDir(object string, size int64) bool {
	return HasSuffix(object, SlashSeparator) && size == 0
}

// Converts just bucket, object metadata into ObjectInfo datatype.
func dirObjectInfo(bucket, object string, size int64, metadata map[string]string) ObjectInfo {
	// This is a special case with size as '0' and object ends with
	// a slash separator, we treat it like a valid operation and
	// return success.
	etag := metadata["etag"]
	delete(metadata, "etag")
	if etag == "" {
		etag = emptyETag
	}

	return ObjectInfo{
		Bucket:      bucket,
		Name:        object,
		ModTime:     UTCNow(),
		ContentType: "application/octet-stream",
		IsDir:       true,
		Size:        size,
		ETag:        etag,
		UserDefined: metadata,
	}
}
