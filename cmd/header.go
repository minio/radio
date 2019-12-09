package cmd

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// SSEHeader is the general AWS SSE HTTP header key.
const SSEHeader = "X-Amz-Server-Side-Encryption"

const (
	// SSEKmsID is the HTTP header key referencing the SSE-KMS
	// key ID.
	SSEKmsID = SSEHeader + "-Aws-Kms-Key-Id"

	// SSEKmsContext is the HTTP header key referencing the
	// SSE-KMS encryption context.
	SSEKmsContext = SSEHeader + "-Context"
)

const (
	// SSECAlgorithm is the HTTP header key referencing
	// the SSE-C algorithm.
	SSECAlgorithm = SSEHeader + "-Customer-Algorithm"

	// SSECKey is the HTTP header key referencing the
	// SSE-C client-provided key..
	SSECKey = SSEHeader + "-Customer-Key"

	// SSECKeyMD5 is the HTTP header key referencing
	// the MD5 sum of the client-provided key.
	SSECKeyMD5 = SSEHeader + "-Customer-Key-Md5"
)

const (
	// SSECopyAlgorithm is the HTTP header key referencing
	// the SSE-C algorithm for SSE-C copy requests.
	SSECopyAlgorithm = "X-Amz-Copy-Source-Server-Side-Encryption-Customer-Algorithm"

	// SSECopyKey is the HTTP header key referencing the SSE-C
	// client-provided key for SSE-C copy requests.
	SSECopyKey = "X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key"

	// SSECopyKeyMD5 is the HTTP header key referencing the
	// MD5 sum of the client key for SSE-C copy requests.
	SSECopyKeyMD5 = "X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key-Md5"
)

const (
	// SSEAlgorithmAES256 is the only supported value for the SSE-S3 or SSE-C algorithm header.
	// For SSE-S3 see: https://docs.aws.amazon.com/AmazonS3/latest/dev/SSEUsingRESTAPI.html
	// For SSE-C  see: https://docs.aws.amazon.com/AmazonS3/latest/dev/ServerSideEncryptionCustomerKeys.html
	SSEAlgorithmAES256 = "AES256"

	// SSEAlgorithmKMS is the value of 'X-Amz-Server-Side-Encryption' for SSE-KMS.
	// See: https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingKMSEncryption.html
	SSEAlgorithmKMS = "aws:kms"
)

// RemoveSensitiveHeaders removes confidential encryption
// information - e.g. the SSE-C key - from the HTTP headers.
// It has the same semantics as RemoveSensitiveEntires.
func RemoveSensitiveHeaders(h http.Header) {
	h.Del(SSECKey)
	h.Del(SSECopyKey)
}

// IsRequested returns true if the HTTP headers indicates
// that any form server-side encryption (SSE-C, SSE-S3 or SSE-KMS)
// is requested.
func IsRequested(h http.Header) bool {
	return S3.IsRequested(h) || SSEC.IsRequested(h) || SSECopy.IsRequested(h) || S3KMS.IsRequested(h)
}

// S3 represents AWS SSE-S3. It provides functionality to handle
// SSE-S3 requests.
var S3 = s3{}

type s3 struct{}

// IsRequested returns true if the HTTP headers indicates that
// the S3 client requests SSE-S3.
func (s3) IsRequested(h http.Header) bool {
	_, ok := h[SSEHeader]
	return ok && strings.ToLower(h.Get(SSEHeader)) != SSEAlgorithmKMS // Return only true if the SSE header is specified and does not contain the SSE-KMS value
}

// ParseHTTP parses the SSE-S3 related HTTP headers and checks
// whether they contain valid values.
func (s3) ParseHTTP(h http.Header) (err error) {
	if h.Get(SSEHeader) != SSEAlgorithmAES256 {
		err = errors.New("The encryption method is not supported")
	}
	return
}

// S3KMS represents AWS SSE-KMS. It provides functionality to
// handle SSE-KMS requests.
var S3KMS = s3KMS{}

type s3KMS struct{}

// IsRequested returns true if the HTTP headers indicates that
// the S3 client requests SSE-KMS.
func (s3KMS) IsRequested(h http.Header) bool {
	if _, ok := h[SSEKmsID]; ok {
		return true
	}
	if _, ok := h[SSEKmsContext]; ok {
		return true
	}
	if _, ok := h[SSEHeader]; ok {
		return strings.ToUpper(h.Get(SSEHeader)) != SSEAlgorithmAES256 // Return only true if the SSE header is specified and does not contain the SSE-S3 value
	}
	return false
}

// ParseHTTP parses the SSE-KMS headers and returns the SSE-KMS key ID
// and context, if present, on success.
func (s3KMS) ParseHTTP(h http.Header) (string, interface{}, error) {
	algorithm := h.Get(SSEHeader)
	if algorithm != SSEAlgorithmKMS {
		return "", nil, errors.New("The encryption method is not supported")
	}

	contextStr, ok := h[SSEKmsContext]
	if ok {
		var context map[string]interface{}
		if err := json.Unmarshal([]byte(contextStr[0]), &context); err != nil {
			return "", nil, err
		}
		return h.Get(SSEKmsID), context, nil
	}
	return h.Get(SSEKmsID), nil, nil
}

var (
	// SSEC represents AWS SSE-C. It provides functionality to handle
	// SSE-C requests.
	SSEC = ssec{}

	// SSECopy represents AWS SSE-C for copy requests. It provides
	// functionality to handle SSE-C copy requests.
	SSECopy = ssecCopy{}
)

type ssec struct{}
type ssecCopy struct{}

// IsRequested returns true if the HTTP headers contains
// at least one SSE-C header. SSE-C copy headers are ignored.
func (ssec) IsRequested(h http.Header) bool {
	if _, ok := h[SSECAlgorithm]; ok {
		return true
	}
	if _, ok := h[SSECKey]; ok {
		return true
	}
	if _, ok := h[SSECKeyMD5]; ok {
		return true
	}
	return false
}

// IsRequested returns true if the HTTP headers contains
// at least one SSE-C copy header. Regular SSE-C headers
// are ignored.
func (ssecCopy) IsRequested(h http.Header) bool {
	if _, ok := h[SSECopyAlgorithm]; ok {
		return true
	}
	if _, ok := h[SSECopyKey]; ok {
		return true
	}
	if _, ok := h[SSECopyKeyMD5]; ok {
		return true
	}
	return false
}

// ParseHTTP parses the SSE-C headers and returns the SSE-C client key
// on success. SSE-C copy headers are ignored.
func (ssec) ParseHTTP(h http.Header) (key [32]byte, err error) {
	if h.Get(SSECAlgorithm) != SSEAlgorithmAES256 {
		return key, ErrInvalidCustomerAlgorithm
	}
	if h.Get(SSECKey) == "" {
		return key, ErrMissingCustomerKey
	}
	if h.Get(SSECKeyMD5) == "" {
		return key, ErrMissingCustomerKeyMD5
	}

	clientKey, err := base64.StdEncoding.DecodeString(h.Get(SSECKey))
	if err != nil || len(clientKey) != 32 { // The client key must be 256 bits long
		return key, ErrInvalidCustomerKey
	}
	keyMD5, err := base64.StdEncoding.DecodeString(h.Get(SSECKeyMD5))
	if md5Sum := md5.Sum(clientKey); err != nil || !bytes.Equal(md5Sum[:], keyMD5) {
		return key, ErrCustomerKeyMD5Mismatch
	}
	copy(key[:], clientKey)
	return key, nil
}

var (
	// ErrInvalidCustomerAlgorithm indicates that the specified SSE-C algorithm
	// is not supported.
	ErrInvalidCustomerAlgorithm = errors.New("The SSE-C algorithm is not supported")

	// ErrMissingCustomerKey indicates that the HTTP headers contains no SSE-C client key.
	ErrMissingCustomerKey = errors.New("The SSE-C request is missing the customer key")

	// ErrMissingCustomerKeyMD5 indicates that the HTTP headers contains no SSE-C client key
	// MD5 checksum.
	ErrMissingCustomerKeyMD5 = errors.New("The SSE-C request is missing the customer key MD5")

	// ErrInvalidCustomerKey indicates that the SSE-C client key is not valid - e.g. not a
	// base64-encoded string or not 256 bits long.
	ErrInvalidCustomerKey = errors.New("The SSE-C client key is invalid")

	// ErrSecretKeyMismatch indicates that the provided secret key (SSE-C client key / SSE-S3 KMS key)
	// does not match the secret key used during encrypting the object.
	ErrSecretKeyMismatch = errors.New("The secret key does not match the secret key used during upload")

	// ErrCustomerKeyMD5Mismatch indicates that the SSE-C key MD5 does not match the
	// computed MD5 sum. This means that the client provided either the wrong key for
	// a certain MD5 checksum or the wrong MD5 for a certain key.
	ErrCustomerKeyMD5Mismatch = errors.New("The provided SSE-C key MD5 does not match the computed MD5 of the SSE-C key")
)

// ParseHTTP parses the SSE-C copy headers and returns the SSE-C client key
// on success. Regular SSE-C headers are ignored.
func (ssecCopy) ParseHTTP(h http.Header) (key [32]byte, err error) {
	if h.Get(SSECopyAlgorithm) != SSEAlgorithmAES256 {
		return key, ErrInvalidCustomerAlgorithm
	}
	if h.Get(SSECopyKey) == "" {
		return key, ErrMissingCustomerKey
	}
	if h.Get(SSECopyKeyMD5) == "" {
		return key, ErrMissingCustomerKeyMD5
	}

	clientKey, err := base64.StdEncoding.DecodeString(h.Get(SSECopyKey))
	if err != nil || len(clientKey) != 32 { // The client key must be 256 bits long
		return key, ErrInvalidCustomerKey
	}
	keyMD5, err := base64.StdEncoding.DecodeString(h.Get(SSECopyKeyMD5))
	if md5Sum := md5.Sum(clientKey); err != nil || !bytes.Equal(md5Sum[:], keyMD5) {
		return key, ErrCustomerKeyMD5Mismatch
	}
	copy(key[:], clientKey)
	return key, nil
}
