package cmd

import (
	"net/http"
	"strings"

	"github.com/minio/minio/pkg/policy"
	"github.com/minio/radio/cmd/logger"
)

// Validate all the ListObjects query arguments, returns an APIErrorCode
// if one of the args do not meet the required conditions.
// Special conditions required by MinIO server are as below
// - delimiter if set should be equal to '/', otherwise the request is rejected.
// - marker if set should have a common prefix with 'prefix' param, otherwise
//   the request is rejected.
func validateListObjectsArgs(marker, delimiter, encodingType string, maxKeys int) APIErrorCode {
	// Max keys cannot be negative.
	if maxKeys < 0 {
		return ErrInvalidMaxKeys
	}

	if encodingType != "" {
		// Only url encoding type is supported
		if strings.ToLower(encodingType) != "url" {
			return ErrInvalidEncodingMethod
		}
	}

	return ErrNone
}

// ListBucketObjectVersions - GET Bucket Object versions
// You can use the versions subresource to list metadata about all
// of the versions of objects in a bucket.
func (api objectAPIHandlers) ListBucketObjectVersionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "ListBucketObjectVersions")

	defer logger.AuditLog(w, r, "ListBucketObjectVersions")

	bucket, _ := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.ListBucketAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	urlValues := r.URL.Query()

	// Extract all the listBucketVersions query params to their native values.
	// versionIDMarker is ignored here.
	prefix, marker, delimiter, maxkeys, encodingType, _, errCode := getListBucketObjectVersionsArgs(urlValues)
	if errCode != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(errCode), r.URL)
		return
	}

	// Validate the query params before beginning to serve the request.
	if s3Error := validateListObjectsArgs(marker, delimiter, encodingType, maxkeys); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	listObjects := objectAPI.ListObjects

	// Inititate a list objects operation based on the input params.
	// On success would return back ListObjectsInfo object to be
	// marshaled into S3 compatible XML header.
	listObjectsInfo, err := listObjects(ctx, bucket, prefix, marker, delimiter, maxkeys)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateListVersionsResponse(bucket, prefix, marker, delimiter, encodingType, maxkeys, listObjectsInfo)

	// Write success response.
	writeSuccessResponseXML(w, encodeResponse(response))
}

// ListObjectsV2MHandler - GET Bucket (List Objects) Version 2 with metadata.
// --------------------------
// This implementation of the GET operation returns some or all (up to 10000)
// of the objects in a bucket. You can use the request parame<ters as selection
// criteria to return a subset of the objects in a bucket.
//
// NOTE: It is recommended that this API to be used for application development.
// MinIO continues to support ListObjectsV1 and V2 for supporting legacy tools.
func (api objectAPIHandlers) ListObjectsV2MHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "ListObjectsV2M")

	defer logger.AuditLog(w, r, "ListObjectsV2M")

	bucket, _ := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.ListBucketAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	urlValues := r.URL.Query()

	// Extract all the listObjectsV2 query params to their native values.
	prefix, token, startAfter, delimiter, fetchOwner, maxKeys, encodingType, errCode := getListObjectsV2Args(urlValues)
	if errCode != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(errCode), r.URL)
		return
	}

	// Validate the query params before beginning to serve the request.
	// fetch-owner is not validated since it is a boolean
	if s3Error := validateListObjectsArgs(token, delimiter, encodingType, maxKeys); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	listObjectsV2 := objectAPI.ListObjectsV2

	// Inititate a list objects operation based on the input params.
	// On success would return back ListObjectsInfo object to be
	// marshaled into S3 compatible XML header.
	listObjectsV2Info, err := listObjectsV2(ctx, bucket, prefix, token, delimiter, maxKeys, fetchOwner, startAfter)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateListObjectsV2Response(bucket, prefix, token,
		listObjectsV2Info.NextContinuationToken, startAfter,
		delimiter, encodingType, fetchOwner, listObjectsV2Info.IsTruncated,
		maxKeys, listObjectsV2Info.Objects, listObjectsV2Info.Prefixes, true)

	// Write success response.
	writeSuccessResponseXML(w, encodeResponse(response))
}

// ListObjectsV2Handler - GET Bucket (List Objects) Version 2.
// --------------------------
// This implementation of the GET operation returns some or all (up to 10000)
// of the objects in a bucket. You can use the request parameters as selection
// criteria to return a subset of the objects in a bucket.
//
// NOTE: It is recommended that this API to be used for application development.
// MinIO continues to support ListObjectsV1 for supporting legacy tools.
func (api objectAPIHandlers) ListObjectsV2Handler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "ListObjectsV2")

	defer logger.AuditLog(w, r, "ListObjectsV2")

	bucket, _ := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.ListBucketAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	urlValues := r.URL.Query()

	// Extract all the listObjectsV2 query params to their native values.
	prefix, token, startAfter, delimiter, fetchOwner, maxKeys, encodingType, errCode := getListObjectsV2Args(urlValues)
	if errCode != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(errCode), r.URL)
		return
	}

	// Validate the query params before beginning to serve the request.
	// fetch-owner is not validated since it is a boolean
	if s3Error := validateListObjectsArgs(token, delimiter, encodingType, maxKeys); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	listObjectsV2 := objectAPI.ListObjectsV2

	// Inititate a list objects operation based on the input params.
	// On success would return back ListObjectsInfo object to be
	// marshaled into S3 compatible XML header.
	listObjectsV2Info, err := listObjectsV2(ctx, bucket, prefix, token, delimiter, maxKeys, fetchOwner, startAfter)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateListObjectsV2Response(bucket, prefix, token,
		listObjectsV2Info.NextContinuationToken, startAfter,
		delimiter, encodingType, fetchOwner, listObjectsV2Info.IsTruncated,
		maxKeys, listObjectsV2Info.Objects, listObjectsV2Info.Prefixes, false)

	// Write success response.
	writeSuccessResponseXML(w, encodeResponse(response))
}

// ListObjectsV1Handler - GET Bucket (List Objects) Version 1.
// --------------------------
// This implementation of the GET operation returns some or all (up to 10000)
// of the objects in a bucket. You can use the request parameters as selection
// criteria to return a subset of the objects in a bucket.
//
func (api objectAPIHandlers) ListObjectsV1Handler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "ListObjectsV1")

	defer logger.AuditLog(w, r, "ListObjectsV1")

	bucket, _ := request2BucketObjectName(r)

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.ListBucketAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Extract all the litsObjectsV1 query params to their native values.
	prefix, marker, delimiter, maxKeys, encodingType, s3Error := getListObjectsV1Args(r.URL.Query())
	if s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Validate all the query params before beginning to serve the request.
	if s3Error := validateListObjectsArgs(marker, delimiter, encodingType, maxKeys); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	listObjects := objectAPI.ListObjects

	// Inititate a list objects operation based on the input params.
	// On success would return back ListObjectsInfo object to be
	// marshaled into S3 compatible XML header.
	listObjectsInfo, err := listObjects(ctx, bucket, prefix, marker, delimiter, maxKeys)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	response := generateListObjectsV1Response(bucket, prefix, marker, delimiter, encodingType, maxKeys, listObjectsInfo)

	// Write success response.
	writeSuccessResponseXML(w, encodeResponse(response))
}
