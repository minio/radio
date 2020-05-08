package cmd

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/minio/minio/pkg/auth"
	"github.com/minio/minio/pkg/bucket/policy"
	"github.com/minio/minio/pkg/hash"
	iampolicy "github.com/minio/minio/pkg/iam/policy"
	xhttp "github.com/minio/radio/cmd/http"
)

const (
	bearerToken = "Bearer"
)

// Verify if request has Bearer.
func isRequestBearerToken(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get(xhttp.Authorization), bearerToken)
}

// Verify if request has AWS Signature Version '4'.
func isRequestSignatureV4(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get(xhttp.Authorization), signV4Algorithm)
}

// Verify if request has AWS PreSign Version '4'.
func isRequestPresignedSignatureV4(r *http.Request) bool {
	_, ok := r.URL.Query()[xhttp.AmzCredential]
	return ok
}

// Verify if request has AWS Post policy Signature Version '4'.
func isRequestPostPolicySignatureV4(r *http.Request) bool {
	return strings.Contains(r.Header.Get(xhttp.ContentType), "multipart/form-data") &&
		r.Method == http.MethodPost
}

// Verify if the request has AWS Streaming Signature Version '4'. This is only valid for 'PUT' operation.
func isRequestSignStreamingV4(r *http.Request) bool {
	return r.Header.Get(xhttp.AmzContentSha256) == streamingContentSHA256 &&
		r.Method == http.MethodPut
}

// Authorization type.
type authType int

// List of all supported auth types.
const (
	authTypeUnknown authType = iota
	authTypePresigned
	authTypePostPolicy
	authTypeStreamingSigned
	authTypeSigned
	authTypeBearer
)

// Get request authentication type.
func getRequestAuthType(r *http.Request) authType {
	if isRequestSignStreamingV4(r) {
		return authTypeStreamingSigned
	} else if isRequestSignatureV4(r) {
		return authTypeSigned
	} else if isRequestPresignedSignatureV4(r) {
		return authTypePresigned
	} else if isRequestBearerToken(r) {
		return authTypeBearer
	} else if isRequestPostPolicySignatureV4(r) {
		return authTypePostPolicy
	}
	return authTypeUnknown
}

// Fetch the security token set by the client.
func getSessionToken(r *http.Request) (token string) {
	token = r.Header.Get(xhttp.AmzSecurityToken)
	if token != "" {
		return token
	}
	return r.URL.Query().Get(xhttp.AmzSecurityToken)
}

// Fetch claims in the security token returned by the client and validate the token.
func checkClaimsFromToken(r *http.Request, cred auth.Credentials) APIErrorCode {
	token := getSessionToken(r)
	if token != "" && cred.AccessKey == "" {
		return ErrNoAccessKey
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(cred.SessionToken)) != 1 {
		return ErrInvalidToken
	}
	return ErrNone
}

// Check request auth type verifies the incoming http request
// - validates the request signature
//   for authenticated requests validates IAM policies.
// returns APIErrorCode if any to be replied to the client.
func checkRequestAuthType(ctx context.Context, r *http.Request, action policy.Action, bucketName, objectName string) (s3Err APIErrorCode) {
	var cred auth.Credentials
	switch getRequestAuthType(r) {
	case authTypeUnknown, authTypeStreamingSigned:
		return ErrAccessDenied
	case authTypeSigned, authTypePresigned:
		region := globalServerRegion
		switch action {
		case policy.GetBucketLocationAction, policy.ListAllMyBucketsAction:
			region = ""
		}
		if s3Err = isReqAuthenticated(ctx, r, region, serviceS3); s3Err != ErrNone {
			return s3Err
		}
		cred, s3Err = getReqAccessKeyV4(r, region, serviceS3)
	}
	if s3Err != ErrNone {
		return s3Err
	}

	return checkClaimsFromToken(r, cred)
}

func reqSignatureV4Verify(r *http.Request, region string, stype serviceType) (s3Error APIErrorCode) {
	sha256sum := getContentSha256Cksum(r)
	switch {
	case isRequestSignatureV4(r):
		return doesSignatureMatch(sha256sum, r, region, stype)
	case isRequestPresignedSignatureV4(r):
		return doesPresignedSignatureMatch(sha256sum, r, region, stype)
	default:
		return ErrAccessDenied
	}
}

// Verify if request has valid AWS Signature Version '4'.
func isReqAuthenticated(ctx context.Context, r *http.Request, region string, stype serviceType) (s3Error APIErrorCode) {
	if errCode := reqSignatureV4Verify(r, region, stype); errCode != ErrNone {
		return errCode
	}

	var (
		err                       error
		contentMD5, contentSHA256 []byte
	)
	// Extract 'Content-Md5' if present.
	if _, ok := r.Header[xhttp.ContentMD5]; ok {
		contentMD5, err = base64.StdEncoding.Strict().DecodeString(r.Header.Get(xhttp.ContentMD5))
		if err != nil || len(contentMD5) == 0 {
			return ErrInvalidDigest
		}
	}

	// Extract either 'X-Amz-Content-Sha256' header or 'X-Amz-Content-Sha256' query parameter (if V4 presigned)
	// Do not verify 'X-Amz-Content-Sha256' if skipSHA256.
	if skipSHA256 := skipContentSha256Cksum(r); !skipSHA256 && isRequestPresignedSignatureV4(r) {
		if sha256Sum, ok := r.URL.Query()[xhttp.AmzContentSha256]; ok && len(sha256Sum) > 0 {
			contentSHA256, err = hex.DecodeString(sha256Sum[0])
			if err != nil {
				return ErrContentSHA256Mismatch
			}
		}
	} else if _, ok := r.Header[xhttp.AmzContentSha256]; !skipSHA256 && ok {
		contentSHA256, err = hex.DecodeString(r.Header.Get(xhttp.AmzContentSha256))
		if err != nil || len(contentSHA256) == 0 {
			return ErrContentSHA256Mismatch
		}
	}

	// Verify 'Content-Md5' and/or 'X-Amz-Content-Sha256' if present.
	// The verification happens implicit during reading.
	reader, err := hash.NewReader(r.Body, -1, hex.EncodeToString(contentMD5),
		hex.EncodeToString(contentSHA256), -1, globalCLIContext.StrictS3Compat)
	if err != nil {
		return toAPIErrorCode(ctx, err)
	}
	r.Body = ioutil.NopCloser(reader)
	return ErrNone
}

// authHandler - handles all the incoming authorization headers and validates them if possible.
type authHandler struct {
	handler http.Handler
}

// setAuthHandler to validate authorization header for the incoming request.
func setAuthHandler(h http.Handler) http.Handler {
	return authHandler{h}
}

// List of all support S3 auth types.
var supportedS3AuthTypes = map[authType]struct{}{
	authTypePresigned:       {},
	authTypeSigned:          {},
	authTypePostPolicy:      {},
	authTypeStreamingSigned: {},
}

// Validate if the authType is valid and supported.
func isSupportedS3AuthType(aType authType) bool {
	_, ok := supportedS3AuthTypes[aType]
	return ok
}

// handler for validating incoming authorization headers.
func (a authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	aType := getRequestAuthType(r)
	if isSupportedS3AuthType(aType) {
		// Let top level caller validate for signed requests.
		a.handler.ServeHTTP(w, r)
		return
	} else if aType == authTypeBearer {
		a.handler.ServeHTTP(w, r)
		return
	}
	writeErrorResponse(context.Background(), w, errorCodes.ToAPIErr(ErrSignatureVersionNotSupported), r.URL)
}

// isPutActionAllowed - check if PUT operation is allowed on the resource, this
// call verifies bucket policies and IAM policies, supports multi user
// checks etc.
func isPutActionAllowed(atype authType, bucketName, objectName string, r *http.Request, action iampolicy.Action) (s3Err APIErrorCode) {
	var cred auth.Credentials
	switch atype {
	case authTypeUnknown:
		return ErrAccessDenied
	case authTypeStreamingSigned, authTypePresigned, authTypeSigned:
		region := globalServerRegion
		cred, s3Err = getReqAccessKeyV4(r, region, serviceS3)
	}
	if s3Err != ErrNone {
		return s3Err
	}

	return checkClaimsFromToken(r, cred)
}
