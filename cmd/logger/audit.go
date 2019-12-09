package logger

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio/cmd/logger/message/audit"
)

// ResponseWriter - is a wrapper to trap the http response status code.
type ResponseWriter struct {
	http.ResponseWriter
	StatusCode int
	// Response body should be logged
	LogBody         bool
	TimeToFirstByte time.Duration
	StartTime       time.Time
	// number of bytes written
	bytesWritten int
	// Internal recording buffer
	headers bytes.Buffer
	body    bytes.Buffer
	// Indicate if headers are written in the log
	headersLogged bool
}

// NewResponseWriter - returns a wrapped response writer to trap
// http status codes for auditiing purposes.
func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
		StartTime:      time.Now().UTC(),
	}
}

func (lrw *ResponseWriter) Write(p []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(p)
	lrw.bytesWritten += n
	if lrw.TimeToFirstByte == 0 {
		lrw.TimeToFirstByte = time.Now().UTC().Sub(lrw.StartTime)
	}
	if !lrw.headersLogged {
		// We assume the response code to be '200 OK' when WriteHeader() is not called,
		// that way following Golang HTTP response behavior.
		lrw.writeHeaders(&lrw.headers, http.StatusOK, lrw.Header())
		lrw.headersLogged = true
	}
	if lrw.StatusCode >= http.StatusBadRequest || lrw.LogBody {
		// Always logging error responses.
		lrw.body.Write(p)
	}
	if err != nil {
		return n, err
	}
	return n, err
}

// Write the headers into the given buffer
func (lrw *ResponseWriter) writeHeaders(w io.Writer, statusCode int, headers http.Header) {
	n, _ := fmt.Fprintf(w, "%d %s\n", statusCode, http.StatusText(statusCode))
	lrw.bytesWritten += n
	for k, v := range headers {
		n, _ := fmt.Fprintf(w, "%s: %s\n", k, v[0])
		lrw.bytesWritten += n
	}
}

// BodyPlaceHolder returns a dummy body placeholder
var BodyPlaceHolder = []byte("<BODY>")

// Body - Return response body.
func (lrw *ResponseWriter) Body() []byte {
	// If there was an error response or body logging is enabled
	// then we return the body contents
	if lrw.StatusCode >= http.StatusBadRequest || lrw.LogBody {
		return lrw.body.Bytes()
	}
	// ... otherwise we return the <BODY> place holder
	return BodyPlaceHolder
}

// WriteHeader - writes http status code
func (lrw *ResponseWriter) WriteHeader(code int) {
	lrw.StatusCode = code
	if !lrw.headersLogged {
		lrw.writeHeaders(&lrw.headers, code, lrw.ResponseWriter.Header())
		lrw.headersLogged = true
	}
	lrw.ResponseWriter.WriteHeader(code)
}

// Flush - Calls the underlying Flush.
func (lrw *ResponseWriter) Flush() {
	lrw.ResponseWriter.(http.Flusher).Flush()
}

// Size - reutrns the number of bytes written
func (lrw *ResponseWriter) Size() int {
	return lrw.bytesWritten
}

// AuditTargets is the list of enabled audit loggers
var AuditTargets = []Target{}

// AddAuditTarget adds a new audit logger target to the
// list of enabled loggers
func AddAuditTarget(t Target) {
	AuditTargets = append(AuditTargets, t)
}

// Convert url path into bucket and object name.
func urlPath2BucketObjectName(path string) (bucketName, objectName string) {
	if path == "" || path == "/" {
		return "", ""
	}

	// Trim any preceding slash separator.
	urlPath := strings.TrimPrefix(path, "/")

	// Split urlpath using slash separator into a given number of
	// expected tokens.
	tokens := strings.SplitN(urlPath, "/", 2)
	bucketName = tokens[0]
	if len(tokens) == 2 {
		objectName = tokens[1]
	}

	// Success.
	return bucketName, objectName
}

// AuditLog - logs audit logs to all audit targets.
func AuditLog(w http.ResponseWriter, r *http.Request, api string) {
	var statusCode int
	var timeToResponse time.Duration
	var timeToFirstByte time.Duration
	lrw, ok := w.(*ResponseWriter)
	if ok {
		statusCode = lrw.StatusCode
		timeToResponse = time.Now().UTC().Sub(lrw.StartTime)
		timeToFirstByte = lrw.TimeToFirstByte
	}

	bucket, object := urlPath2BucketObjectName(r.URL.Path)

	// Send audit logs only to http targets.
	for _, t := range AuditTargets {
		entry := audit.ToEntry(w, r, nil, globalDeploymentID)
		entry.API.Name = api
		entry.API.Bucket = bucket
		entry.API.Object = object
		entry.API.Status = http.StatusText(statusCode)
		entry.API.StatusCode = statusCode
		entry.API.TimeToFirstByte = timeToFirstByte.String()
		entry.API.TimeToResponse = timeToResponse.String()
		_ = t.Send(entry, string(All))
	}
}
