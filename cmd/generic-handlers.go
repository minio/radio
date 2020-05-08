package cmd

import (
	"context"
	"net/http"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	xhttp "github.com/minio/radio/cmd/http"
	"github.com/minio/radio/cmd/logger"
	"github.com/rs/cors"
)

// HandlerFunc - useful to chain different middleware http.Handler
type HandlerFunc func(http.Handler) http.Handler

func registerHandlers(h http.Handler, handlerFns ...HandlerFunc) http.Handler {
	for _, hFn := range handlerFns {
		h = hFn(h)
	}
	return h
}

// Adds limiting body size middleware

// Maximum allowed form data field values. 64MiB is a guessed practical value
// which is more than enough to accommodate any form data fields and headers.
const requestFormDataSize = 64 * humanize.MiByte

// For any HTTP request, request body should be not more than 16GiB + requestFormDataSize
// where, 16GiB is the maximum allowed object size for object upload.
const requestMaxBodySize = globalMaxObjectSize + requestFormDataSize

type requestSizeLimitHandler struct {
	handler     http.Handler
	maxBodySize int64
}

func setRequestSizeLimitHandler(h http.Handler) http.Handler {
	return requestSizeLimitHandler{handler: h, maxBodySize: requestMaxBodySize}
}

func (h requestSizeLimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Restricting read data to a given maximum length
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodySize)
	h.handler.ServeHTTP(w, r)
}

const (
	// Maximum size for http headers - See: https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingMetadata.html
	maxHeaderSize = 8 * 1024
	// Maximum size for user-defined metadata - See: https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingMetadata.html
	maxUserDataSize = 2 * 1024
)

type requestHeaderSizeLimitHandler struct {
	http.Handler
}

func setRequestHeaderSizeLimitHandler(h http.Handler) http.Handler {
	return requestHeaderSizeLimitHandler{h}
}

// ServeHTTP restricts the size of the http header to 8 KB and the size
// of the user-defined metadata to 2 KB.
func (h requestHeaderSizeLimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isHTTPHeaderSizeTooLarge(r.Header) {
		writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrMetadataTooLarge), r.URL)
		return
	}
	h.Handler.ServeHTTP(w, r)
}

// isHTTPHeaderSizeTooLarge returns true if the provided
// header is larger than 8 KB or the user-defined metadata
// is larger than 2 KB.
func isHTTPHeaderSizeTooLarge(header http.Header) bool {
	var size, usersize int
	for key := range header {
		length := len(key) + len(header.Get(key))
		size += length
		for _, prefix := range userMetadataKeyPrefixes {
			if HasPrefix(key, prefix) {
				usersize += length
				break
			}
		}
		if usersize > maxUserDataSize || size > maxHeaderSize {
			return true
		}
	}
	return false
}

// ReservedMetadataPrefix is the prefix of a metadata key which
// is reserved and for internal use only.
const ReservedMetadataPrefix = "X-Minio-Internal-"

type reservedMetadataHandler struct {
	http.Handler
}

func filterReservedMetadata(h http.Handler) http.Handler {
	return reservedMetadataHandler{h}
}

// ServeHTTP fails if the request contains at least one reserved header which
// would be treated as metadata.
func (h reservedMetadataHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if containsReservedMetadata(r.Header) {
		writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrUnsupportedMetadata), r.URL)
		return
	}
	h.Handler.ServeHTTP(w, r)
}

// containsReservedMetadata returns true if the http.Header contains
// keys which are treated as metadata but are reserved for internal use
// and must not set by clients
func containsReservedMetadata(header http.Header) bool {
	for key := range header {
		if HasPrefix(key, ReservedMetadataPrefix) {
			return true
		}
	}
	return false
}

// Reserved bucket.
const (
	radioReservedBucket     = ".radio"
	radioReservedBucketPath = SlashSeparator + radioReservedBucket
)

type timeValidityHandler struct {
	handler http.Handler
}

// setTimeValidityHandler to validate parsable time over http header
func setTimeValidityHandler(h http.Handler) http.Handler {
	return timeValidityHandler{h}
}

// Supported Amz date formats.
var amzDateFormats = []string{
	time.RFC1123,
	time.RFC1123Z,
	iso8601Format,
	// Add new AMZ date formats here.
}

// Supported Amz date headers.
var amzDateHeaders = []string{
	"x-amz-date",
	"date",
}

// parseAmzDate - parses date string into supported amz date formats.
func parseAmzDate(amzDateStr string) (amzDate time.Time, apiErr APIErrorCode) {
	for _, dateFormat := range amzDateFormats {
		amzDate, err := time.Parse(dateFormat, amzDateStr)
		if err == nil {
			return amzDate, ErrNone
		}
	}
	return time.Time{}, ErrMalformedDate
}

// parseAmzDateHeader - parses supported amz date headers, in
// supported amz date formats.
func parseAmzDateHeader(req *http.Request) (time.Time, APIErrorCode) {
	for _, amzDateHeader := range amzDateHeaders {
		amzDateStr := req.Header.Get(http.CanonicalHeaderKey(amzDateHeader))
		if amzDateStr != "" {
			return parseAmzDate(amzDateStr)
		}
	}
	// Date header missing.
	return time.Time{}, ErrMissingDateHeader
}

func (h timeValidityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	aType := getRequestAuthType(r)
	if aType == authTypeSigned || aType == authTypeStreamingSigned {
		// Verify if date headers are set, if not reject the request
		amzDate, errCode := parseAmzDateHeader(r)
		if errCode != ErrNone {
			// All our internal APIs are sensitive towards Date
			// header, for all requests where Date header is not
			// present we will reject such clients.
			writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(errCode), r.URL)
			return
		}
		// Verify if the request date header is shifted by less than globalMaxSkewTime parameter in the past
		// or in the future, reject request otherwise.
		curTime := UTCNow()
		if curTime.Sub(amzDate) > globalMaxSkewTime || amzDate.Sub(curTime) > globalMaxSkewTime {
			writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrRequestTimeTooSkewed), r.URL)
			return
		}
	}
	h.handler.ServeHTTP(w, r)
}

// setCorsHandler handler for CORS (Cross Origin Resource Sharing)
func setCorsHandler(h http.Handler) http.Handler {
	commonS3Headers := []string{
		xhttp.Date,
		xhttp.ETag,
		xhttp.ServerInfo,
		xhttp.Connection,
		xhttp.AcceptRanges,
		xhttp.ContentRange,
		xhttp.ContentEncoding,
		xhttp.ContentLength,
		xhttp.ContentType,
		"X-Amz*",
		"x-amz*",
		"*",
	}

	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPut,
			http.MethodHead,
			http.MethodPost,
			http.MethodDelete,
			http.MethodOptions,
			http.MethodPatch,
		},
		AllowedHeaders:   commonS3Headers,
		ExposedHeaders:   commonS3Headers,
		AllowCredentials: true,
	})

	return c.Handler(h)
}

// httpStatsHandler definition: gather HTTP statistics
type httpStatsHandler struct {
	handler http.Handler
}

// setHttpStatsHandler sets a http Stats Handler
func setHTTPStatsHandler(h http.Handler) http.Handler {
	return httpStatsHandler{handler: h}
}

func (h httpStatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isS3Request := !strings.HasPrefix(r.URL.Path, radioReservedBucketPath)
	// record s3 connection stats.
	recordRequest := &recordTrafficRequest{ReadCloser: r.Body, isS3Request: isS3Request}
	r.Body = recordRequest
	recordResponse := &recordTrafficResponse{w, isS3Request}
	// Execute the request
	h.handler.ServeHTTP(recordResponse, r)
}

// requestValidityHandler validates all the incoming paths for
// any malicious requests.
type requestValidityHandler struct {
	handler http.Handler
}

func setRequestValidityHandler(h http.Handler) http.Handler {
	return requestValidityHandler{handler: h}
}

// Bad path components to be rejected by the path validity handler.
const (
	dotdotComponent = ".."
	dotComponent    = "."
)

// Check if the incoming path has bad path components,
// such as ".." and "."
func hasBadPathComponent(path string) bool {
	path = strings.TrimSpace(path)
	for _, p := range strings.Split(path, SlashSeparator) {
		switch strings.TrimSpace(p) {
		case dotdotComponent:
			return true
		case dotComponent:
			return true
		}
	}
	return false
}

// Check if client is sending a malicious request.
func hasMultipleAuth(r *http.Request) bool {
	authTypeCount := 0
	for _, hasValidAuth := range []func(*http.Request) bool{
		isRequestSignatureV4,
		isRequestPresignedSignatureV4,
		isRequestPostPolicySignatureV4,
		isRequestBearerToken,
	} {
		if hasValidAuth(r) {
			authTypeCount++
		}
	}
	return authTypeCount > 1
}

func (h requestValidityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check for bad components in URL path.
	if hasBadPathComponent(r.URL.Path) {
		writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrInvalidResourceName), r.URL)
		return
	}
	// Check for bad components in URL query values.
	for _, vv := range r.URL.Query() {
		for _, v := range vv {
			if hasBadPathComponent(v) {
				writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrInvalidResourceName), r.URL)
				return
			}
		}
	}
	if hasMultipleAuth(r) {
		writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrInvalidRequest), r.URL)
		return
	}
	h.handler.ServeHTTP(w, r)
}

// customHeaderHandler sets x-amz-request-id header.
// Previously, this value was set right before a response was sent to
// the client. So, logger and Error response XML were not using this
// value. This is set here so that this header can be logged as
// part of the log entry, Error response XML and auditing.
type customHeaderHandler struct {
	handler http.Handler
}

func addCustomHeaders(h http.Handler) http.Handler {
	return customHeaderHandler{handler: h}
}

func (s customHeaderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set custom headers such as x-amz-request-id for each request.
	w.Header().Set(xhttp.AmzRequestID, mustGetRequestID(UTCNow()))
	s.handler.ServeHTTP(logger.NewResponseWriter(w), r)
}

type securityHeaderHandler struct {
	handler http.Handler
}

func addSecurityHeaders(h http.Handler) http.Handler {
	return securityHeaderHandler{handler: h}
}

func (s securityHeaderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("X-XSS-Protection", "1; mode=block")                  // Prevents against XSS attacks
	header.Set("Content-Security-Policy", "block-all-mixed-content") // prevent mixed (HTTP / HTTPS content)
	s.handler.ServeHTTP(w, r)
}

// criticalErrorHandler handles critical server failures caused by
// `panic(logger.ErrCritical)` as done by `logger.CriticalIf`.
//
// It should be always the first / highest HTTP handler.
type criticalErrorHandler struct{ handler http.Handler }

func (h criticalErrorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err == logger.ErrCritical { // handle
			writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrInternalError), r.URL)
		} else if err != nil {
			panic(err) // forward other panic calls
		}
	}()
	h.handler.ServeHTTP(w, r)
}

func setSSETLSHandler(h http.Handler) http.Handler { return sseTLSHandler{h} }

// sseTLSHandler enforces certain rules for SSE requests which are made / must be made over TLS.
type sseTLSHandler struct{ handler http.Handler }

func (h sseTLSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Deny SSE-C requests if not made over TLS
	if !globalIsSSL && (SSEC.IsRequested(r.Header) || SSECopy.IsRequested(r.Header)) {
		if r.Method == http.MethodHead {
			writeErrorResponseHeadersOnly(w, errorCodes.ToAPIErr(ErrInsecureSSECustomerRequest))
		} else {
			writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrInsecureSSECustomerRequest), r.URL)
		}
		return
	}
	h.handler.ServeHTTP(w, r)
}
