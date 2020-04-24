package cmd

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/minio/minio/pkg/handlers"
	xhttp "github.com/minio/radio/cmd/http"
	"github.com/minio/radio/cmd/logger"

	humanize "github.com/dustin/go-humanize"
)

// IsErrIgnored returns whether given error is ignored or not.
func IsErrIgnored(err error, ignoredErrs ...error) bool {
	return IsErr(err, ignoredErrs...)
}

// IsErr returns whether given error is exact error.
func IsErr(err error, errs ...error) bool {
	for _, exactErr := range errs {
		if errors.Is(err, exactErr) {
			return true
		}
	}
	return false
}

func request2BucketObjectName(r *http.Request) (bucketName, objectName string) {
	path, err := getResource(r.URL.Path, r.Host, globalDomainNames)
	if err != nil {
		logger.CriticalIf(context.Background(), err)
	}
	return urlPath2BucketObjectName(path)
}

// Convert url path into bucket and object name.
func urlPath2BucketObjectName(path string) (bucketName, objectName string) {
	if path == "" || path == SlashSeparator {
		return "", ""
	}

	// Trim any preceding slash separator.
	urlPath := strings.TrimPrefix(path, SlashSeparator)

	// Split urlpath using slash separator into a given number of
	// expected tokens.
	tokens := strings.SplitN(urlPath, SlashSeparator, 2)
	bucketName = tokens[0]
	if len(tokens) == 2 {
		objectName = tokens[1]
	}

	// Success.
	return bucketName, objectName
}

// URI scheme constants.
const (
	httpScheme  = "http"
	httpsScheme = "https"
)

// checkValidMD5 - verify if valid md5, returns md5 in bytes.
func checkValidMD5(h http.Header) ([]byte, error) {
	md5B64, ok := h["Content-Md5"]
	if ok {
		if md5B64[0] == "" {
			return nil, fmt.Errorf("Content-Md5 header set to empty value")
		}
		return base64.StdEncoding.DecodeString(md5B64[0])
	}
	return []byte{}, nil
}

/// http://docs.aws.amazon.com/AmazonS3/latest/dev/UploadingObjects.html
const (
	// Maximum object size per PUT request is 5TB.
	// This is a divergence from S3 limit on purpose to support
	// use cases where users are going to upload large files
	// using 'curl' and presigned URL.
	globalMaxObjectSize = 5 * humanize.TiByte

	// Maximum Part size for multipart upload is 5GiB
	globalMaxPartSize = 5 * humanize.GiByte

	// Maximum Part ID for multipart upload is 10000
	// (Acceptable values range from 1 to 10000 inclusive)
	globalMaxPartID = 10000

	// Default values used while communicating with the cloud backends
	defaultDialTimeout = 30 * time.Second
)

// isMaxObjectSize - verify if max object size
func isMaxObjectSize(size int64) bool {
	return size > globalMaxObjectSize
}

// // Check if part size is more than maximum allowed size.
func isMaxAllowedPartSize(size int64) bool {
	return size > globalMaxPartSize
}

// isMaxPartNumber - Check if part ID is greater than the maximum allowed ID.
func isMaxPartID(partID int) bool {
	return partID > globalMaxPartID
}

func contains(slice interface{}, elem interface{}) bool {
	v := reflect.ValueOf(slice)
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if v.Index(i).Interface() == elem {
				return true
			}
		}
	}
	return false
}

// isFile - returns whether given path is a file or not.
func isFile(path string) bool {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().IsRegular()
	}

	return false
}

// UTCNow - returns current UTC time.
func UTCNow() time.Time {
	return time.Now().UTC()
}

// GenETag - generate UUID based ETag
func GenETag() string {
	return ToS3ETag(getMD5Hash([]byte(mustGetUUID())))
}

// ToS3ETag - return checksum to ETag
func ToS3ETag(etag string) string {
	etag = canonicalizeETag(etag)

	if !strings.HasSuffix(etag, "-1") {
		// Tools like s3cmd uses ETag as checksum of data to validate.
		// Append "-1" to indicate ETag is not a checksum.
		etag += "-1"
	}

	return etag
}

type dialContext func(ctx context.Context, network, address string) (net.Conn, error)

func newCustomDialContext(dialTimeout, dialKeepAlive time.Duration) dialContext {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: dialKeepAlive,
			DualStack: true,
		}

		return dialer.DialContext(ctx, network, addr)
	}
}

func newCustomHTTPTransport(tlsConfig *tls.Config, dialTimeout time.Duration) func() *http.Transport {
	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           newCustomDialContext(dialTimeout, 15*time.Second),
		MaxIdleConnsPerHost:   256,
		MaxIdleConns:          16,
		IdleConnTimeout:       1 * time.Minute,
		ResponseHeaderTimeout: 3 * time.Minute, // Set conservative timeouts for MinIO internode.
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSClientConfig:       tlsConfig,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}
	return func() *http.Transport {
		return tr
	}
}

// NewCustomHTTPTransport returns a new http configuration
// used while communicating with the cloud backends.
// This sets the value for MaxIdleConnsPerHost from 2 (go default)
// to 256.
func NewCustomHTTPTransport() *http.Transport {
	return newCustomHTTPTransport(&tls.Config{
		RootCAs: globalRootCAs,
	}, defaultDialTimeout)()
}

// Returns context with ReqInfo details set in the context.
func newContext(r *http.Request, w http.ResponseWriter, api string) context.Context {
	bucket, object := request2BucketObjectName(r)
	reqInfo := &logger.ReqInfo{
		DeploymentID: globalDeploymentID,
		RequestID:    w.Header().Get(xhttp.AmzRequestID),
		RemoteHost:   handlers.GetSourceIP(r),
		Host:         getHostName(r),
		UserAgent:    r.UserAgent(),
		API:          api,
		BucketName:   bucket,
		ObjectName:   object,
	}
	return logger.SetReqInfo(r.Context(), reqInfo)
}

// Used for registering with rest handlers (have a look at registerStorageRESTHandlers for usage example)
// If it is passed ["aaaa", "bbbb"], it returns ["aaaa", "{aaaa:.*}", "bbbb", "{bbbb:.*}"]
func restQueries(keys ...string) []string {
	var accumulator []string
	for _, key := range keys {
		accumulator = append(accumulator, key, "{"+key+":.*}")
	}
	return accumulator
}

// IsNetworkOrHostDown - if there was a network error or if the host is down.
func IsNetworkOrHostDown(err error) bool {
	if err == nil {
		return false
	}
	// We need to figure if the error either a timeout
	// or a non-temporary error.
	e, ok := err.(net.Error)
	if ok {
		urlErr, ok := e.(*url.Error)
		if ok {
			switch urlErr.Err.(type) {
			case *net.DNSError, *net.OpError, net.UnknownNetworkError:
				return true
			}
		}
		if e.Timeout() {
			return true
		}
	}
	ok = false
	// Fallback to other mechanisms.
	if strings.Contains(err.Error(), "Connection closed by foreign host") {
		ok = true
	} else if strings.Contains(err.Error(), "TLS handshake timeout") {
		// If error is - tlsHandshakeTimeoutError.
		ok = true
	} else if strings.Contains(err.Error(), "i/o timeout") {
		// If error is - tcp timeoutError.
		ok = true
	} else if strings.Contains(err.Error(), "connection timed out") {
		// If err is a net.Dial timeout.
		ok = true
	} else if strings.Contains(strings.ToLower(err.Error()), "503 service unavailable") {
		// Denial errors
		ok = true
	}
	return ok
}
