package cmd

import (
	"context"
	"encoding/hex"
	"encoding/xml"
	"io"
	goioutil "io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"time"

	"github.com/minio/minio-go/v6/pkg/encrypt"
	xhttp "github.com/minio/minio/cmd/http"
	"github.com/minio/minio/pkg/hash"
	iampolicy "github.com/minio/minio/pkg/iam/policy"
	"github.com/minio/minio/pkg/ioutil"
	"github.com/minio/minio/pkg/policy"
	"github.com/minio/minio/pkg/s3select"
	"github.com/minio/radio/cmd/logger"
)

// supportedHeadGetReqParams - supported request parameters for GET and HEAD presigned request.
var supportedHeadGetReqParams = map[string]string{
	"response-expires":             xhttp.Expires,
	"response-content-type":        xhttp.ContentType,
	"response-cache-control":       xhttp.CacheControl,
	"response-content-encoding":    xhttp.ContentEncoding,
	"response-content-language":    xhttp.ContentLanguage,
	"response-content-disposition": xhttp.ContentDisposition,
}

// setHeadGetRespHeaders - set any requested parameters as response headers.
func setHeadGetRespHeaders(w http.ResponseWriter, reqParams url.Values) {
	for k, v := range reqParams {
		if header, ok := supportedHeadGetReqParams[k]; ok {
			w.Header()[header] = v
		}
	}
}

// SelectObjectContentHandler - GET Object?select
// ----------
// This implementation of the GET operation retrieves object content based
// on an SQL expression. In the request, along with the sql expression, you must
// also specify a data serialization format (JSON, CSV) of the object.
func (api objectAPIHandlers) SelectObjectContentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "SelectObject")

	defer logger.AuditLog(w, r, "SelectObject")

	// Fetch object stat info.
	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	bucket, object := request2BucketObjectName(r)

	// Check for auth type to return S3 compatible error.
	// type to return the correct error (NoSuchKey vs AccessDenied)
	if s3Error := checkRequestAuthType(ctx, r, policy.GetObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Get request range.
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrUnsupportedRangeHeader), r.URL)
		return
	}

	if r.ContentLength <= 0 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEmptyRequestBody), r.URL)
		return
	}

	s3Select, err := s3select.NewS3Select(r.Body)
	if err != nil {
		if serr, ok := err.(s3select.SelectError); ok {
			encodedErrorResponse := encodeResponse(APIErrorResponse{
				Code:       serr.ErrorCode(),
				Message:    serr.ErrorMessage(),
				BucketName: bucket,
				Key:        object,
				Resource:   r.URL.Path,
				RequestID:  w.Header().Get(xhttp.AmzRequestID),
				HostID:     globalDeploymentID,
			})
			writeResponse(w, serr.HTTPStatusCode(), encodedErrorResponse, mimeXML)
		} else {
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		}
		return
	}

	getObjectNInfo := objectAPI.GetObjectNInfo
	if api.CacheAPI() != nil {
		getObjectNInfo = api.CacheAPI().GetObjectNInfo
	}
	getObject := func(offset, length int64) (rc io.ReadCloser, err error) {
		isSuffixLength := false
		if offset < 0 {
			isSuffixLength = true
		}
		rs := &HTTPRangeSpec{
			IsSuffixLength: isSuffixLength,
			Start:          offset,
			End:            offset + length,
		}

		return getObjectNInfo(ctx, bucket, object, rs, r.Header, ReadLock, ObjectOptions{})
	}

	if err = s3Select.Open(getObject); err != nil {
		if serr, ok := err.(s3select.SelectError); ok {
			encodedErrorResponse := encodeResponse(APIErrorResponse{
				Code:       serr.ErrorCode(),
				Message:    serr.ErrorMessage(),
				BucketName: bucket,
				Key:        object,
				Resource:   r.URL.Path,
				RequestID:  w.Header().Get(xhttp.AmzRequestID),
				HostID:     globalDeploymentID,
			})
			writeResponse(w, serr.HTTPStatusCode(), encodedErrorResponse, mimeXML)
		} else {
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		}
		return
	}

	s3Select.Evaluate(w)
	s3Select.Close()
}

// GetObjectHandler - GET Object
// ----------
// This implementation of the GET operation retrieves object. To use GET,
// you must have READ access to the object.
func (api objectAPIHandlers) GetObjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "GetObject")

	defer logger.AuditLog(w, r, "GetObject")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	bucket, object := request2BucketObjectName(r)

	if vid := r.URL.Query().Get("versionId"); vid != "" && vid != "null" {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
		return
	}

	// Check for auth type to return S3 compatible error.
	// type to return the correct error (NoSuchKey vs AccessDenied)
	if s3Error := checkRequestAuthType(ctx, r, policy.GetObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	getObjectNInfo := objectAPI.GetObjectNInfo
	if api.CacheAPI() != nil {
		getObjectNInfo = api.CacheAPI().GetObjectNInfo
	}

	// Get request range.
	var rs *HTTPRangeSpec
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var err error
		if rs, err = parseRequestRangeSpec(rangeHeader); err != nil {
			// Handle only errInvalidRange. Ignore other
			// parse error and treat it as regular Get
			// request like Amazon S3.
			if err == errInvalidRange {
				writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidRange), r.URL)
				return
			}

			logger.LogIf(ctx, err, logger.Application)
		}
	}

	gr, err := getObjectNInfo(ctx, bucket, object, rs, r.Header, ReadLock, ObjectOptions{})
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	defer gr.Close()
	objInfo := gr.ObjInfo

	// Validate pre-conditions if any.
	if checkPreconditions(ctx, w, r, objInfo) {
		return
	}

	if err = setObjectHeaders(w, objInfo, rs); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	setHeadGetRespHeaders(w, r.URL.Query())

	statusCodeWritten := false
	httpWriter := ioutil.WriteOnClose(w)
	if rs != nil {
		statusCodeWritten = true
		w.WriteHeader(http.StatusPartialContent)
	}
	// Write object content to response body
	if _, err = io.Copy(httpWriter, gr); err != nil {
		if !httpWriter.HasWritten() && !statusCodeWritten { // write error response only if no data or headers has been written to client yet
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		}
		return
	}

	if err = httpWriter.Close(); err != nil {
		if !httpWriter.HasWritten() && !statusCodeWritten { // write error response only if no data or headers has been written to client yet
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
			return
		}
	}
}

// HeadObjectHandler - HEAD Object
// -----------
// The HEAD operation retrieves metadata from an object without returning the object itself.
func (api objectAPIHandlers) HeadObjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "HeadObject")

	defer logger.AuditLog(w, r, "HeadObject")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponseHeadersOnly(w, errorCodes.ToAPIErr(ErrServerNotInitialized))
		return
	}

	bucket, object := request2BucketObjectName(r)

	if vid := r.URL.Query().Get("versionId"); vid != "" && vid != "null" {
		writeErrorResponseHeadersOnly(w, errorCodes.ToAPIErr(ErrNoSuchVersion))
		return
	}

	getObjectInfo := objectAPI.GetObjectInfo
	if api.CacheAPI() != nil {
		getObjectInfo = api.CacheAPI().GetObjectInfo
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.GetObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponseHeadersOnly(w, errorCodes.ToAPIErr(s3Error))
		return
	}

	// Get request range.
	var rs *HTTPRangeSpec
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var err error
		if rs, err = parseRequestRangeSpec(rangeHeader); err != nil {
			// Handle only errInvalidRange. Ignore other
			// parse error and treat it as regular Get
			// request like Amazon S3.
			if err == errInvalidRange {
				writeErrorResponseHeadersOnly(w, errorCodes.ToAPIErr(ErrInvalidRange))
				return
			}

			logger.LogIf(ctx, err)
		}
	}

	objInfo, err := getObjectInfo(ctx, bucket, object, ObjectOptions{})
	if err != nil {
		writeErrorResponseHeadersOnly(w, toAPIError(ctx, err))
		return
	}

	// Validate pre-conditions if any.
	if checkPreconditions(ctx, w, r, objInfo) {
		return
	}

	// Set standard object headers.
	if err = setObjectHeaders(w, objInfo, rs); err != nil {
		writeErrorResponseHeadersOnly(w, toAPIError(ctx, err))
		return
	}

	// Set any additional requested response headers.
	setHeadGetRespHeaders(w, r.URL.Query())

	// Successful response.
	if rs != nil {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}

// Extract metadata relevant for an CopyObject operation based on conditional
// header values specified in X-Amz-Metadata-Directive.
func getCpObjMetadataFromHeader(ctx context.Context, r *http.Request, userMeta map[string]string) (map[string]string, error) {
	// Make a copy of the supplied metadata to avoid
	// to change the original one.
	defaultMeta := make(map[string]string, len(userMeta))
	for k, v := range userMeta {
		defaultMeta[k] = v
	}

	// if x-amz-metadata-directive says REPLACE then
	// we extract metadata from the input headers.
	if isMetadataReplace(r.Header) {
		return extractMetadata(ctx, r)
	}

	// if x-amz-metadata-directive says COPY then we
	// return the default metadata.
	if isMetadataCopy(r.Header) {
		return defaultMeta, nil
	}

	// Copy is default behavior if not x-amz-metadata-directive is set.
	return defaultMeta, nil
}

// CopyObjectHandler - Copy Object
// ----------
// This implementation of the PUT operation adds an object to a bucket
// while reading the object from another source.
// Notice: The S3 client can send secret keys in headers for encryption related jobs,
// the handler should ensure to remove these keys before sending them to the object layer.
// Currently these keys are:
//   - X-Amz-Server-Side-Encryption-Customer-Key
//   - X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key
func (api objectAPIHandlers) CopyObjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "CopyObject")

	defer logger.AuditLog(w, r, "CopyObject")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	dstBucket, dstObject := request2BucketObjectName(r)

	if s3Error := checkRequestAuthType(ctx, r, policy.PutObjectAction, dstBucket, dstObject); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// TODO: Reject requests where body/payload is present, for now we don't even read it.

	// Read escaped copy source path to check for parameters.
	cpSrcPath := r.Header.Get(xhttp.AmzCopySource)

	// Check https://docs.aws.amazon.com/AmazonS3/latest/dev/ObjectVersioning.html
	// Regardless of whether you have enabled versioning, each object in your bucket
	// has a version ID. If you have not enabled versioning, Amazon S3 sets the value
	// of the version ID to null. If you have enabled versioning, Amazon S3 assigns a
	// unique version ID value for the object.
	if u, err := url.Parse(cpSrcPath); err == nil {
		// Check if versionId query param was added, if yes then check if
		// its non "null" value, we should error out since we do not support
		// any versions other than "null".
		if vid := u.Query().Get("versionId"); vid != "" && vid != "null" {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
			return
		}
		// Note that url.Parse does the unescaping
		cpSrcPath = u.Path
	}
	if vid := r.Header.Get(xhttp.AmzCopySourceVersionID); vid != "" {
		// Check if versionId header was added, if yes then check if
		// its non "null" value, we should error out since we do not support
		// any versions other than "null".
		if vid != "null" {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
			return
		}
	}

	srcBucket, srcObject := path2BucketAndObject(cpSrcPath)
	// If source object is empty or bucket is empty, reply back invalid copy source.
	if srcObject == "" || srcBucket == "" {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopySource), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.GetObjectAction, srcBucket, srcObject); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Check if metadata directive is valid.
	if !isMetadataDirectiveValid(r.Header) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidMetadataDirective), r.URL)
		return
	}

	cpSrcDstSame := isStringEqual(pathJoin(srcBucket, srcObject), pathJoin(dstBucket, dstObject))

	getObjectNInfo := objectAPI.GetObjectNInfo
	if api.CacheAPI() != nil {
		getObjectNInfo = api.CacheAPI().GetObjectNInfo
	}

	var lock = NoLock
	if !cpSrcDstSame {
		lock = ReadLock
	}
	var rs *HTTPRangeSpec
	gr, err := getObjectNInfo(ctx, srcBucket, srcObject, rs, r.Header, lock, ObjectOptions{})
	if err != nil {
		if isErrPreconditionFailed(err) {
			return
		}
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	defer gr.Close()
	srcInfo := gr.ObjInfo

	/// maximum Upload size for object in a single CopyObject operation.
	if isMaxObjectSize(srcInfo.Size) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEntityTooLarge), r.URL)
		return
	}

	// We have to copy metadata only if source and destination are same.
	// this changes for encryption which can be observed below.
	if cpSrcDstSame {
		srcInfo.metadataOnly = true
	}

	var reader io.Reader
	var length = srcInfo.Size

	// Set the actual size to the decrypted size if encrypted.
	actualSize := srcInfo.Size
	srcInfo.Reader, err = hash.NewReader(reader, length, "", "", actualSize, globalCLIContext.StrictS3Compat)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	pReader := NewPutObjReader(srcInfo.Reader, nil, nil)
	srcInfo.PutObjReader = pReader

	srcInfo.UserDefined, err = getCpObjMetadataFromHeader(ctx, r, srcInfo.UserDefined)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	// Check if x-amz-metadata-directive was not set to REPLACE and source,
	// desination are same objects. Apply this restriction also when
	// metadataOnly is true indicating that we are not overwriting the object.
	// if encryption is enabled we do not need explicit "REPLACE" metadata to
	// be enabled as well - this is to allow for key-rotation.
	if !isMetadataReplace(r.Header) && srcInfo.metadataOnly {
		// If x-amz-metadata-directive is not set to REPLACE then we need
		// to error out if source and destination are same.
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopyDest), r.URL)
		return
	}

	// Copy source object to destination, if source and destination
	// object is same then only metadata is updated.
	objInfo, err := objectAPI.CopyObject(ctx, srcBucket, srcObject, dstBucket, dstObject, srcInfo, ObjectOptions{}, ObjectOptions{})
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateCopyObjectResponse(objInfo.ETag, objInfo.ModTime)
	encodedSuccessResponse := encodeResponse(response)

	// Write success response.
	writeSuccessResponseXML(w, encodedSuccessResponse)
}

// PutObjectHandler - PUT Object
// ----------
// This implementation of the PUT operation adds an object to a bucket.
// Notice: The S3 client can send secret keys in headers for encryption related jobs,
// the handler should ensure to remove these keys before sending them to the object layer.
// Currently these keys are:
//   - X-Amz-Server-Side-Encryption-Customer-Key
//   - X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key
func (api objectAPIHandlers) PutObjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "PutObject")
	defer logger.AuditLog(w, r, "PutObject")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	bucket, object := request2BucketObjectName(r)

	// To detect if the client has disconnected.
	r.Body = &detectDisconnect{r.Body, r.Context().Done()}

	// X-Amz-Copy-Source shouldn't be set for this call.
	if _, ok := r.Header[xhttp.AmzCopySource]; ok {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopySource), r.URL)
		return
	}

	// Get Content-Md5 sent by client and verify if valid
	md5Bytes, err := checkValidMD5(r.Header)
	if err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidDigest), r.URL)
		return
	}
	/// if Content-Length is unknown/missing, deny the request
	size := r.ContentLength
	rAuthType := getRequestAuthType(r)
	if rAuthType == authTypeStreamingSigned {
		if sizeStr, ok := r.Header[xhttp.AmzDecodedContentLength]; ok {
			if sizeStr[0] == "" {
				writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentLength), r.URL)
				return
			}
			size, err = strconv.ParseInt(sizeStr[0], 10, 64)
			if err != nil {
				writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
				return
			}
		}
	}
	if size == -1 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentLength), r.URL)
		return
	}

	/// maximum Upload size for objects in a single operation
	if isMaxObjectSize(size) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEntityTooLarge), r.URL)
		return
	}

	metadata, err := extractMetadata(ctx, r)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	if rAuthType == authTypeStreamingSigned {
		if contentEncoding, ok := metadata["content-encoding"]; ok {
			contentEncoding = trimAwsChunkedContentEncoding(contentEncoding)
			if contentEncoding != "" {
				// Make sure to trim and save the content-encoding
				// parameter for a streaming signature which is set
				// to a custom value for example: "aws-chunked,gzip".
				metadata["content-encoding"] = contentEncoding
			} else {
				// Trimmed content encoding is empty when the header
				// value is set to "aws-chunked" only.

				// Make sure to delete the content-encoding parameter
				// for a streaming signature which is set to value
				// for example: "aws-chunked"
				delete(metadata, "content-encoding")
			}
		}
	}

	var (
		md5hex    = hex.EncodeToString(md5Bytes)
		sha256hex = ""
		reader    io.Reader
		s3Err     APIErrorCode
		putObject = objectAPI.PutObject
	)
	reader = r.Body

	// Check if put is allowed
	if s3Err = isPutActionAllowed(rAuthType, bucket, object, r, iampolicy.PutObjectAction); s3Err != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Err), r.URL)
		return
	}

	switch rAuthType {
	case authTypeStreamingSigned:
		// Initialize stream signature verifier.
		reader, s3Err = newSignV4ChunkedReader(r)
		if s3Err != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Err), r.URL)
			return
		}
	case authTypeSignedV2, authTypePresignedV2:
		s3Err = isReqAuthenticatedV2(r)
		if s3Err != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Err), r.URL)
			return
		}

	case authTypePresigned, authTypeSigned:
		if s3Err = reqSignatureV4Verify(r, globalServerRegion, serviceS3); s3Err != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Err), r.URL)
			return
		}
		if !skipContentSha256Cksum(r) {
			sha256hex = getContentSha256Cksum(r, serviceS3)
		}
	}

	actualSize := size

	hashReader, err := hash.NewReader(reader, size, md5hex, sha256hex, actualSize, globalCLIContext.StrictS3Compat)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	rawReader := hashReader
	pReader := NewPutObjReader(rawReader, nil, nil)

	if api.CacheAPI() != nil {
		putObject = api.CacheAPI().PutObject
	}

	// Create the object..
	objInfo, err := putObject(ctx, bucket, object, pReader, ObjectOptions{UserDefined: metadata})
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	etag := objInfo.ETag
	w.Header()[xhttp.ETag] = []string{"\"" + etag + "\""}

	writeSuccessResponseHeadersOnly(w)
}

/// Multipart objectAPIHandlers

// NewMultipartUploadHandler - New multipart upload.
// Notice: The S3 client can send secret keys in headers for encryption related jobs,
// the handler should ensure to remove these keys before sending them to the object layer.
// Currently these keys are:
//   - X-Amz-Server-Side-Encryption-Customer-Key
//   - X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key
func (api objectAPIHandlers) NewMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "NewMultipartUpload")

	defer logger.AuditLog(w, r, "NewMultipartUpload")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	bucket, object := request2BucketObjectName(r)

	if s3Error := checkRequestAuthType(ctx, r, policy.PutObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Extract metadata that needs to be saved.
	metadata, err := extractMetadata(ctx, r)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	newMultipartUpload := objectAPI.NewMultipartUpload

	uploadID, err := newMultipartUpload(ctx, bucket, object, ObjectOptions{UserDefined: metadata})
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateInitiateMultipartUploadResponse(bucket, object, uploadID)
	encodedSuccessResponse := encodeResponse(response)

	// Write success response.
	writeSuccessResponseXML(w, encodedSuccessResponse)
}

// CopyObjectPartHandler - uploads a part by copying data from an existing object as data source.
func (api objectAPIHandlers) CopyObjectPartHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "CopyObjectPart")

	defer logger.AuditLog(w, r, "CopyObjectPart")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	dstBucket, dstObject := request2BucketObjectName(r)

	if s3Error := checkRequestAuthType(ctx, r, policy.PutObjectAction, dstBucket, dstObject); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Read escaped copy source path to check for parameters.
	cpSrcPath := r.Header.Get(xhttp.AmzCopySource)

	// Check https://docs.aws.amazon.com/AmazonS3/latest/dev/ObjectVersioning.html
	// Regardless of whether you have enabled versioning, each object in your bucket
	// has a version ID. If you have not enabled versioning, Amazon S3 sets the value
	// of the version ID to null. If you have enabled versioning, Amazon S3 assigns a
	// unique version ID value for the object.
	if u, err := url.Parse(cpSrcPath); err == nil {
		// Check if versionId query param was added, if yes then check if
		// its non "null" value, we should error out since we do not support
		// any versions other than "null".
		if vid := u.Query().Get("versionId"); vid != "" && vid != "null" {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
			return
		}
		// Note that url.Parse does the unescaping
		cpSrcPath = u.Path
	}
	if vid := r.Header.Get(xhttp.AmzCopySourceVersionID); vid != "" {
		// Check if X-Amz-Copy-Source-Version-Id header was added, if yes then check if
		// its non "null" value, we should error out since we do not support
		// any versions other than "null".
		if vid != "null" {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
			return
		}
	}

	srcBucket, srcObject := path2BucketAndObject(cpSrcPath)
	// If source object is empty or bucket is empty, reply back invalid copy source.
	if srcObject == "" || srcBucket == "" {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopySource), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.GetObjectAction, srcBucket, srcObject); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	uploadID := r.URL.Query().Get("uploadId")
	partIDString := r.URL.Query().Get("partNumber")

	partID, err := strconv.Atoi(partIDString)
	if err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidPart), r.URL)
		return
	}

	// check partID with maximum part ID for multipart objects
	if isMaxPartID(partID) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidMaxParts), r.URL)
		return
	}

	var srcOpts, dstOpts ObjectOptions
	var getOpts = ObjectOptions{}
	if srcOpts.ServerSideEncryption != nil {
		getOpts.ServerSideEncryption = encrypt.SSE(srcOpts.ServerSideEncryption)
	}

	getObjectNInfo := objectAPI.GetObjectNInfo
	if api.CacheAPI() != nil {
		getObjectNInfo = api.CacheAPI().GetObjectNInfo
	}

	// Get request range.
	var rs *HTTPRangeSpec
	rangeHeader := r.Header.Get(xhttp.AmzCopySourceRange)
	if rangeHeader != "" {
		var parseRangeErr error
		if rs, parseRangeErr = parseCopyPartRangeSpec(rangeHeader); parseRangeErr != nil {
			// Handle only errInvalidRange
			// Ignore other parse error and treat it as regular Get request like Amazon S3.
			logger.GetReqInfo(ctx).AppendTags("rangeHeader", rangeHeader)
			logger.LogIf(ctx, parseRangeErr)
			writeCopyPartErr(ctx, w, parseRangeErr, r.URL)
			return

		}
	}
	checkCopyPartPrecondFn := func(o ObjectInfo) bool {
		return checkCopyObjectPartPreconditions(ctx, w, r, o)
	}
	getOpts.CheckCopyPrecondFn = checkCopyPartPrecondFn
	srcOpts.CheckCopyPrecondFn = checkCopyPartPrecondFn

	gr, err := getObjectNInfo(ctx, srcBucket, srcObject, rs, r.Header, ReadLock, getOpts)
	if err != nil {
		if isErrPreconditionFailed(err) {
			return
		}
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	defer gr.Close()
	srcInfo := gr.ObjInfo

	actualPartSize := srcInfo.Size
	// Special care for CopyObjectPart
	if partRangeErr := checkCopyPartRangeWithSize(rs, actualPartSize); partRangeErr != nil {
		writeCopyPartErr(ctx, w, partRangeErr, r.URL)
		return
	}

	// Get the object offset & length
	startOffset, length, err := rs.GetOffsetLength(actualPartSize)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	/// maximum copy size for multipart objects in a single operation
	if isMaxAllowedPartSize(length) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEntityTooLarge), r.URL)
		return
	}

	actualPartSize = length
	reader := gr
	srcInfo.Reader, err = hash.NewReader(reader, length, "", "",
		actualPartSize, globalCLIContext.StrictS3Compat)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	rawReader := srcInfo.Reader
	pReader := NewPutObjReader(rawReader, nil, nil)

	srcInfo.PutObjReader = pReader
	// Copy source object to destination, if source and destination
	// object is same then only metadata is updated.
	partInfo, err := objectAPI.CopyObjectPart(ctx, srcBucket, srcObject, dstBucket, dstObject, uploadID, partID,
		startOffset, length, srcInfo, srcOpts, dstOpts)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateCopyObjectPartResponse(partInfo.ETag, partInfo.LastModified)
	encodedSuccessResponse := encodeResponse(response)

	// Write success response.
	writeSuccessResponseXML(w, encodedSuccessResponse)
}

// PutObjectPartHandler - uploads an incoming part for an ongoing multipart operation.
func (api objectAPIHandlers) PutObjectPartHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "PutObjectPart")

	defer logger.AuditLog(w, r, "PutObjectPart")

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	bucket, object := request2BucketObjectName(r)

	// X-Amz-Copy-Source shouldn't be set for this call.
	if _, ok := r.Header[xhttp.AmzCopySource]; ok {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidCopySource), r.URL)
		return
	}

	// get Content-Md5 sent by client and verify if valid
	md5Bytes, err := checkValidMD5(r.Header)
	if err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidDigest), r.URL)
		return
	}

	/// if Content-Length is unknown/missing, throw away
	size := r.ContentLength

	rAuthType := getRequestAuthType(r)
	// For auth type streaming signature, we need to gather a different content length.
	if rAuthType == authTypeStreamingSigned {
		if sizeStr, ok := r.Header[xhttp.AmzDecodedContentLength]; ok {
			if sizeStr[0] == "" {
				writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentLength), r.URL)
				return
			}
			size, err = strconv.ParseInt(sizeStr[0], 10, 64)
			if err != nil {
				writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
				return
			}
		}
	}
	if size == -1 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentLength), r.URL)
		return
	}

	/// maximum Upload size for multipart objects in a single operation
	if isMaxAllowedPartSize(size) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEntityTooLarge), r.URL)
		return
	}

	uploadID := r.URL.Query().Get("uploadId")
	partIDString := r.URL.Query().Get("partNumber")

	partID, err := strconv.Atoi(partIDString)
	if err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidPart), r.URL)
		return
	}

	// check partID with maximum part ID for multipart objects
	if isMaxPartID(partID) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidMaxParts), r.URL)
		return
	}

	var (
		md5hex    = hex.EncodeToString(md5Bytes)
		sha256hex = ""
		reader    io.Reader
		s3Error   APIErrorCode
	)
	reader = r.Body
	if s3Error = isPutActionAllowed(rAuthType, bucket, object, r, iampolicy.PutObjectAction); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	switch rAuthType {
	case authTypeStreamingSigned:
		// Initialize stream signature verifier.
		reader, s3Error = newSignV4ChunkedReader(r)
		if s3Error != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
			return
		}
	case authTypeSignedV2, authTypePresignedV2:
		if s3Error = isReqAuthenticatedV2(r); s3Error != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
			return
		}
	case authTypePresigned, authTypeSigned:
		if s3Error = reqSignatureV4Verify(r, globalServerRegion, serviceS3); s3Error != ErrNone {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
			return
		}

		if !skipContentSha256Cksum(r) {
			sha256hex = getContentSha256Cksum(r, serviceS3)
		}
	}

	actualSize := size
	hashReader, err := hash.NewReader(reader, size, md5hex, sha256hex, actualSize, globalCLIContext.StrictS3Compat)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	rawReader := hashReader
	pReader := NewPutObjReader(rawReader, nil, nil)

	putObjectPart := objectAPI.PutObjectPart

	partInfo, err := putObjectPart(ctx, bucket, object, uploadID, partID, pReader, ObjectOptions{})
	if err != nil {
		// Verify if the underlying error is signature mismatch.
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	etag := partInfo.ETag
	w.Header()[xhttp.ETag] = []string{"\"" + etag + "\""}

	writeSuccessResponseHeadersOnly(w)
}

// AbortMultipartUploadHandler - Abort multipart upload
func (api objectAPIHandlers) AbortMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "AbortMultipartUpload")

	defer logger.AuditLog(w, r, "AbortMultipartUpload")

	bucket, object := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}
	abortMultipartUpload := objectAPI.AbortMultipartUpload

	if s3Error := checkRequestAuthType(ctx, r, policy.AbortMultipartUploadAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	uploadID, _, _, _, s3Error := getObjectResources(r.URL.Query())
	if s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}
	if err := abortMultipartUpload(ctx, bucket, object, uploadID); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	writeSuccessNoContent(w)
}

// ListObjectPartsHandler - List object parts
func (api objectAPIHandlers) ListObjectPartsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "ListObjectParts")

	defer logger.AuditLog(w, r, "ListObjectParts")

	bucket, object := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.ListMultipartUploadPartsAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	uploadID, partNumberMarker, maxParts, encodingType, s3Error := getObjectResources(r.URL.Query())
	if s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}
	if partNumberMarker < 0 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidPartNumberMarker), r.URL)
		return
	}
	if maxParts < 0 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidMaxParts), r.URL)
		return
	}
	var opts ObjectOptions
	listPartsInfo, err := objectAPI.ListObjectParts(ctx, bucket, object, uploadID, partNumberMarker, maxParts, opts)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateListPartsResponse(listPartsInfo, encodingType)
	encodedSuccessResponse := encodeResponse(response)

	// Write success response.
	writeSuccessResponseXML(w, encodedSuccessResponse)
}

type whiteSpaceWriter struct {
	http.ResponseWriter
	http.Flusher
	written bool
}

func (w *whiteSpaceWriter) Write(b []byte) (n int, err error) {
	n, err = w.ResponseWriter.Write(b)
	w.written = true
	return
}

func (w *whiteSpaceWriter) WriteHeader(statusCode int) {
	if !w.written {
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

// Send empty whitespaces every 10 seconds to the client till completeMultiPartUpload() is
// done so that the client does not time out. Downside is we might send 200 OK and
// then send error XML. But accoording to S3 spec the client is supposed to check
// for error XML even if it received 200 OK. But for erasure this is not a problem
// as completeMultiPartUpload() is quick. Even For FS, it would not be an issue as
// we do background append as and when the parts arrive and completeMultiPartUpload
// is quick. Only in a rare case where parts would be out of order will
// FS:completeMultiPartUpload() take a longer time.
func sendWhiteSpace(w http.ResponseWriter) <-chan bool {
	doneCh := make(chan bool)
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		headerWritten := false
		for {
			select {
			case <-ticker.C:
				// Write header if not written yet.
				if !headerWritten {
					w.Write([]byte(xml.Header))
					headerWritten = true
				}

				// Once header is written keep writing empty spaces
				// which are ignored by client SDK XML parsers.
				// This occurs when server takes long time to completeMultiPartUpload()
				w.Write([]byte(" "))
				w.(http.Flusher).Flush()
			case doneCh <- headerWritten:
				ticker.Stop()
				return
			}
		}

	}()
	return doneCh
}

// CompleteMultipartUploadHandler - Complete multipart upload.
func (api objectAPIHandlers) CompleteMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "CompleteMultipartUpload")

	defer logger.AuditLog(w, r, "CompleteMultipartUpload")

	bucket, object := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.PutObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Get upload id.
	uploadID, _, _, _, s3Error := getObjectResources(r.URL.Query())
	if s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	completeMultipartBytes, err := goioutil.ReadAll(r.Body)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}
	complMultipartUpload := &CompleteMultipartUpload{}
	if err = xml.Unmarshal(completeMultipartBytes, complMultipartUpload); err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMalformedXML), r.URL)
		return
	}
	if len(complMultipartUpload.Parts) == 0 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMalformedXML), r.URL)
		return
	}
	if !sort.IsSorted(CompletedParts(complMultipartUpload.Parts)) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrInvalidPartOrder), r.URL)
		return
	}

	// Complete parts.
	var completeParts []CompletePart
	for _, part := range complMultipartUpload.Parts {
		part.ETag = canonicalizeETag(part.ETag)
		completeParts = append(completeParts, part)
	}

	completeMultiPartUpload := objectAPI.CompleteMultipartUpload

	// This code is specifically to handle the requirements for slow
	// complete multipart upload operations on FS mode.
	writeErrorResponseWithoutXMLHeader := func(ctx context.Context, w http.ResponseWriter, err APIError, reqURL *url.URL) {
		switch err.Code {
		case "SlowDown", "XMinioServerNotInitialized", "XMinioReadQuorum", "XMinioWriteQuorum":
			// Set retry-after header to indicate user-agents to retry request after 120secs.
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After
			w.Header().Set(xhttp.RetryAfter, "120")
		}

		// Generate error response.
		errorResponse := getAPIErrorResponse(ctx, err, reqURL.Path,
			w.Header().Get(xhttp.AmzRequestID), globalDeploymentID)
		encodedErrorResponse, _ := xml.Marshal(errorResponse)
		setCommonHeaders(w)
		w.Header().Set(xhttp.ContentType, string(mimeXML))
		w.Write(encodedErrorResponse)
		w.(http.Flusher).Flush()
	}

	w.Header().Set(xhttp.ContentType, "text/event-stream")
	w = &whiteSpaceWriter{ResponseWriter: w, Flusher: w.(http.Flusher)}
	completeDoneCh := sendWhiteSpace(w)

	objInfo, err := completeMultiPartUpload(ctx, bucket, object, uploadID, completeParts, ObjectOptions{})
	// Stop writing white spaces to the client. Note that close(doneCh) style is not used as it
	// can cause white space to be written after we send XML response in a race condition.
	headerWritten := <-completeDoneCh
	if err != nil {
		if headerWritten {
			writeErrorResponseWithoutXMLHeader(ctx, w, toAPIError(ctx, err), r.URL)
		} else {
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		}
		return
	}

	// Get object location.
	location := getObjectLocation(r, globalDomainNames, bucket, object)
	// Generate complete multipart response.
	response := generateCompleteMultpartUploadResponse(bucket, object, location, objInfo.ETag)
	var encodedSuccessResponse []byte
	if !headerWritten {
		encodedSuccessResponse = encodeResponse(response)
	} else {
		encodedSuccessResponse, err = xml.Marshal(response)
		if err != nil {
			writeErrorResponseWithoutXMLHeader(ctx, w, toAPIError(ctx, err), r.URL)
			return
		}
	}

	// Set etag.
	w.Header()[xhttp.ETag] = []string{"\"" + objInfo.ETag + "\""}

	// Write success response.
	writeSuccessResponseXML(w, encodedSuccessResponse)
}

/// Delete objectAPIHandlers

// DeleteObjectHandler - delete an object
func (api objectAPIHandlers) DeleteObjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "DeleteObject")

	defer logger.AuditLog(w, r, "DeleteObject")

	bucket, object := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.DeleteObjectAction, bucket, object); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	if vid := r.URL.Query().Get("versionId"); vid != "" && vid != "null" {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchVersion), r.URL)
		return
	}

	// http://docs.aws.amazon.com/AmazonS3/latest/API/RESTObjectDELETE.html
	if err := deleteObject(ctx, objectAPI, api.CacheAPI(), bucket, object, r); err != nil {
		switch err.(type) {
		case BucketNotFound:
			// When bucket doesn't exist specially handle it.
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
			return
		}
		// Ignore delete object errors while replying to client, since we are suppposed to reply only 204.
	}
	writeSuccessNoContent(w)
}
