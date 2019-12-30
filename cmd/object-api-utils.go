package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	xhttp "github.com/minio/radio/cmd/http"
	"github.com/minio/minio/pkg/hash"
	"github.com/minio/radio/cmd/logger"
	"github.com/skyrings/skyring-common/tools/uuid"
)

const (
	// MinIO meta bucket.
	minioMetaBucket = ".minio.sys"
	// Multipart meta prefix.
	mpartMetaPrefix = "multipart"
	// MinIO Multipart meta prefix.
	minioMetaMultipartBucket = minioMetaBucket + SlashSeparator + mpartMetaPrefix
	// MinIO Tmp meta prefix.
	minioMetaTmpBucket = minioMetaBucket + "/tmp"
	// DNS separator (period), used for bucket name validation.
	dnsDelimiter = "."
)

// isMinioBucket returns true if given bucket is a MinIO internal
// bucket and false otherwise.
func isMinioMetaBucketName(bucket string) bool {
	return bucket == minioMetaBucket ||
		bucket == minioMetaMultipartBucket ||
		bucket == minioMetaTmpBucket
}

// IsValidBucketName verifies that a bucket name is in accordance with
// Amazon's requirements (i.e. DNS naming conventions). It must be 3-63
// characters long, and it must be a sequence of one or more labels
// separated by periods. Each label can contain lowercase ascii
// letters, decimal digits and hyphens, but must not begin or end with
// a hyphen. See:
// http://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html
func IsValidBucketName(bucket string) bool {
	// Special case when bucket is equal to one of the meta buckets.
	if isMinioMetaBucketName(bucket) {
		return true
	}
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}

	// Split on dot and check each piece conforms to rules.
	allNumbers := true
	pieces := strings.Split(bucket, dnsDelimiter)
	for _, piece := range pieces {
		if len(piece) == 0 || piece[0] == '-' ||
			piece[len(piece)-1] == '-' {
			// Current piece has 0-length or starts or
			// ends with a hyphen.
			return false
		}
		// Now only need to check if each piece is a valid
		// 'label' in AWS terminology and if the bucket looks
		// like an IP address.
		isNotNumber := false
		for i := 0; i < len(piece); i++ {
			switch {
			case (piece[i] >= 'a' && piece[i] <= 'z' ||
				piece[i] == '-'):
				// Found a non-digit character, so
				// this piece is not a number.
				isNotNumber = true
			case piece[i] >= '0' && piece[i] <= '9':
				// Nothing to do.
			default:
				// Found invalid character.
				return false
			}
		}
		allNumbers = allNumbers && !isNotNumber
	}
	// Does the bucket name look like an IP address?
	return !(len(pieces) == 4 && allNumbers)
}

// IsValidObjectName verifies an object name in accordance with Amazon's
// requirements. It cannot exceed 1024 characters and must be a valid UTF8
// string.
//
// See:
// http://docs.aws.amazon.com/AmazonS3/latest/dev/UsingMetadata.html
//
// You should avoid the following characters in a key name because of
// significant special handling for consistency across all
// applications.
//
// Rejects strings with following characters.
//
// - Backslash ("\")
//
// additionally minio does not support object names with trailing SlashSeparator.
func IsValidObjectName(object string) bool {
	if len(object) == 0 {
		return false
	}
	if HasSuffix(object, SlashSeparator) {
		return false
	}
	return IsValidObjectPrefix(object)
}

// IsValidObjectPrefix verifies whether the prefix is a valid object name.
// Its valid to have a empty prefix.
func IsValidObjectPrefix(object string) bool {
	if hasBadPathComponent(object) {
		return false
	}
	if !utf8.ValidString(object) {
		return false
	}
	// Reject unsupported characters in object name.
	if strings.ContainsAny(object, "\\") {
		return false
	}
	return true
}

// SlashSeparator - slash separator.
const SlashSeparator = "/"

// pathJoin - like path.Join() but retains trailing SlashSeparator of the last element
func pathJoin(elem ...string) string {
	trailingSlash := ""
	if len(elem) > 0 {
		if HasSuffix(elem[len(elem)-1], SlashSeparator) {
			trailingSlash = SlashSeparator
		}
	}
	return path.Join(elem...) + trailingSlash
}

// mustGetUUID - get a random UUID.
func mustGetUUID() string {
	uuid, err := uuid.New()
	if err != nil {
		logger.CriticalIf(context.Background(), err)
	}

	return uuid.String()
}

// Create an s3 compatible MD5sum for complete multipart transaction.
func getCompleteMultipartMD5(parts []CompletePart) string {
	var finalMD5Bytes []byte
	for _, part := range parts {
		md5Bytes, err := hex.DecodeString(canonicalizeETag(part.ETag))
		if err != nil {
			finalMD5Bytes = append(finalMD5Bytes, []byte(part.ETag)...)
		} else {
			finalMD5Bytes = append(finalMD5Bytes, md5Bytes...)
		}
	}
	s3MD5 := fmt.Sprintf("%s-%d", getMD5Hash(finalMD5Bytes), len(parts))
	return s3MD5
}

// Clean unwanted fields from metadata
func cleanMetadata(metadata map[string]string) map[string]string {
	// Remove STANDARD StorageClass
	metadata = removeStandardStorageClass(metadata)
	// Clean meta etag keys 'md5Sum', 'etag', "expires".
	return cleanMetadataKeys(metadata, "md5Sum", "etag", "expires")
}

// Filter X-Amz-Storage-Class field only if it is set to STANDARD.
// This is done since AWS S3 doesn't return STANDARD Storage class as response header.
func removeStandardStorageClass(metadata map[string]string) map[string]string {
	if metadata[xhttp.AmzStorageClass] == "STANDARD" {
		delete(metadata, xhttp.AmzStorageClass)
	}
	return metadata
}

// cleanMetadataKeys takes keyNames to be filtered
// and returns a new map with all the entries with keyNames removed.
func cleanMetadataKeys(metadata map[string]string, keyNames ...string) map[string]string {
	var newMeta = make(map[string]string)
	for k, v := range metadata {
		if contains(keyNames, k) {
			continue
		}
		newMeta[k] = v
	}
	return newMeta
}

// Extracts etag value from the metadata.
func extractETag(metadata map[string]string) string {
	// md5Sum tag is kept for backward compatibility.
	etag, ok := metadata["md5Sum"]
	if !ok {
		etag = metadata["etag"]
	}
	// Success.
	return etag
}

// HasPrefix - Prefix matcher string matches prefix in a platform specific way.
// For example on windows since its case insensitive we are supposed
// to do case insensitive checks.
func HasPrefix(s string, prefix string) bool {
	if runtime.GOOS == globalWindowsOSName {
		return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
	}
	return strings.HasPrefix(s, prefix)
}

// HasSuffix - Suffix matcher string matches suffix in a platform specific way.
// For example on windows since its case insensitive we are supposed
// to do case insensitive checks.
func HasSuffix(s string, suffix string) bool {
	if runtime.GOOS == globalWindowsOSName {
		return strings.HasSuffix(strings.ToLower(s), strings.ToLower(suffix))
	}
	return strings.HasSuffix(s, suffix)
}

// Validates if two strings are equal.
func isStringEqual(s1 string, s2 string) bool {
	if runtime.GOOS == globalWindowsOSName {
		return strings.EqualFold(s1, s2)
	}
	return s1 == s2
}

// GetActualSize - read the decompressed size from the meta json.
func (o ObjectInfo) GetActualSize() int64 {
	metadata := o.UserDefined
	sizeStr, ok := metadata[ReservedMetadataPrefix+"actual-size"]
	if ok {
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err == nil {
			return size
		}
	}
	return -1
}

// GetObjectReader is a type that wraps a reader with a lock to
// provide a ReadCloser interface that unlocks on Close()
type GetObjectReader struct {
	ObjInfo ObjectInfo
	pReader io.Reader

	cleanUpFns []func()
	precondFn  func(ObjectInfo) bool
	once       sync.Once
}

// NewGetObjectReaderFromReader sets up a GetObjectReader with a given
// reader. This ignores any object properties.
func NewGetObjectReaderFromReader(r io.Reader, oi ObjectInfo, pcfn CheckCopyPreconditionFn, cleanupFns ...func()) (*GetObjectReader, error) {
	if pcfn != nil {
		if ok := pcfn(oi); ok {
			// Call the cleanup funcs
			for i := len(cleanupFns) - 1; i >= 0; i-- {
				cleanupFns[i]()
			}
			return nil, PreConditionFailed{}
		}
	}
	return &GetObjectReader{
		ObjInfo:    oi,
		pReader:    r,
		cleanUpFns: cleanupFns,
		precondFn:  pcfn,
	}, nil
}

// ObjReaderFn is a function type that takes a reader and returns
// GetObjectReader and an error. Request headers are passed to provide
// encryption parameters. cleanupFns allow cleanup funcs to be
// registered for calling after usage of the reader.
type ObjReaderFn func(inputReader io.Reader, h http.Header, pcfn CheckCopyPreconditionFn, cleanupFns ...func()) (r *GetObjectReader, err error)

// NewGetObjectReader creates a new GetObjectReader. The cleanUpFns
// are called on Close() in reverse order as passed here. NOTE: It is
// assumed that clean up functions do not panic (otherwise, they may
// not all run!).
func NewGetObjectReader(rs *HTTPRangeSpec, oi ObjectInfo, pcfn CheckCopyPreconditionFn, cleanUpFns ...func()) (
	fn ObjReaderFn, off, length int64, err error) {

	// Call the clean-up functions immediately in case of exit
	// with error
	defer func() {
		if err != nil {
			for i := len(cleanUpFns) - 1; i >= 0; i-- {
				cleanUpFns[i]()
			}
		}
	}()

	off, length, err = rs.GetOffsetLength(oi.Size)
	if err != nil {
		return nil, 0, 0, err
	}
	fn = func(inputReader io.Reader, _ http.Header, pcfn CheckCopyPreconditionFn, cFns ...func()) (r *GetObjectReader, err error) {
		cFns = append(cleanUpFns, cFns...)
		if pcfn != nil {
			if ok := pcfn(oi); ok {
				// Call the cleanup funcs
				for i := len(cFns) - 1; i >= 0; i-- {
					cFns[i]()
				}
				return nil, PreConditionFailed{}
			}
		}
		r = &GetObjectReader{
			ObjInfo:    oi,
			pReader:    inputReader,
			cleanUpFns: cFns,
			precondFn:  pcfn,
		}
		return r, nil
	}
	return fn, off, length, nil
}

// Close - calls the cleanup actions in reverse order
func (g *GetObjectReader) Close() error {
	// sync.Once is used here to ensure that Close() is
	// idempotent.
	g.once.Do(func() {
		for i := len(g.cleanUpFns) - 1; i >= 0; i-- {
			g.cleanUpFns[i]()
		}
	})
	return nil
}

// Read - to implement Reader interface.
func (g *GetObjectReader) Read(p []byte) (n int, err error) {
	n, err = g.pReader.Read(p)
	if err != nil {
		// Calling code may not Close() in case of error, so
		// we ensure it.
		g.Close()
	}
	return
}

//SealMD5CurrFn seals md5sum with object encryption key and returns sealed
// md5sum
type SealMD5CurrFn func([]byte) []byte

// PutObjReader is a type that wraps sio.EncryptReader and
// underlying hash.Reader in a struct
type PutObjReader struct {
	*hash.Reader              // actual data stream
	rawReader    *hash.Reader // original data stream
	sealMD5Fn    SealMD5CurrFn
}

// Size returns the absolute number of bytes the Reader
// will return during reading. It returns -1 for unlimited
// data.
func (p *PutObjReader) Size() int64 {
	return p.Reader.Size()
}

// MD5CurrentHexString returns the current MD5Sum or encrypted MD5Sum
// as a hex encoded string
func (p *PutObjReader) MD5CurrentHexString() string {
	md5sumCurr := p.rawReader.MD5Current()
	var appendHyphen bool
	// md5sumcurr is not empty in two scenarios
	// - server is running in strict compatibility mode
	// - client set Content-Md5 during PUT operation
	if len(md5sumCurr) == 0 {
		// md5sumCurr is only empty when we are running
		// in non-compatibility mode.
		md5sumCurr = make([]byte, 16)
		rand.Read(md5sumCurr)
		appendHyphen = true
	}
	if p.sealMD5Fn != nil {
		md5sumCurr = p.sealMD5Fn(md5sumCurr)
	}
	if appendHyphen {
		// Make sure to return etag string upto 32 length, for SSE
		// requests ETag might be longer and the code decrypting the
		// ETag ignores ETag in multipart ETag form i.e <hex>-N
		return hex.EncodeToString(md5sumCurr)[:32] + "-1"
	}
	return hex.EncodeToString(md5sumCurr)
}

// NewPutObjReader returns a new PutObjReader and holds
// reference to underlying data stream from client and the encrypted
// data reader
func NewPutObjReader(rawReader *hash.Reader, encReader *hash.Reader, encKey []byte) *PutObjReader {
	p := PutObjReader{Reader: rawReader, rawReader: rawReader}
	return &p
}

// CleanMinioInternalMetadataKeys removes X-Amz-Meta- prefix from minio internal
// encryption metadata that was sent by minio radio
func CleanMinioInternalMetadataKeys(metadata map[string]string) map[string]string {
	var newMeta = make(map[string]string, len(metadata))
	for k, v := range metadata {
		if strings.HasPrefix(k, "X-Amz-Meta-X-Minio-Internal-") {
			newMeta[strings.TrimPrefix(k, "X-Amz-Meta-")] = v
		} else {
			newMeta[k] = v
		}
	}
	return newMeta
}

// Returns error if the cancelCh has been closed (indicating that S3 client has disconnected)
type detectDisconnect struct {
	io.ReadCloser
	cancelCh <-chan struct{}
}

func (d *detectDisconnect) Read(p []byte) (int, error) {
	select {
	case <-d.cancelCh:
		return 0, io.ErrUnexpectedEOF
	default:
		return d.ReadCloser.Read(p)
	}
}
