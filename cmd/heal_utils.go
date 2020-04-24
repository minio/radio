package cmd

import (
	"net/http"

	xhttp "github.com/minio/minio/cmd/http"
)

// Converts  object info to metadata.
func objectInfoToMetadata(oi ObjectInfo) map[string]string {
	meta := make(map[string]string)
	meta["content-type"] = oi.ContentType
	if oi.ContentEncoding != "" {
		meta["content-encoding"] = oi.ContentEncoding
	}
	if oi.StorageClass != "" {
		meta[xhttp.AmzStorageClass] = oi.StorageClass
	}
	if oi.Expires != timeSentinel {
		meta["expires"] = oi.Expires.UTC().Format(http.TimeFormat)
	}
	for k, v := range oi.UserDefined {
		meta[k] = v
	}
	return meta
}
