package cmd

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	xhttp "github.com/minio/radio/cmd/http"
)

func newHTTPServerFn() *xhttp.Server {
	globalObjLayerMutex.Lock()
	defer globalObjLayerMutex.Unlock()
	return globalHTTPServer
}

func newObjectLayerFn() ObjectLayer {
	globalObjLayerMutex.Lock()
	defer globalObjLayerMutex.Unlock()
	return globalObjectAPI
}

func newCachedObjectLayerFn() CacheObjectLayer {
	globalObjLayerMutex.Lock()
	defer globalObjLayerMutex.Unlock()
	return globalCacheObjectAPI
}

// objectAPIHandler implements and provides http handlers for S3 API.
type objectAPIHandlers struct {
	ObjectAPI func() ObjectLayer
	CacheAPI  func() CacheObjectLayer
}

// registerAPIRouter - registers S3 compatible APIs.
func registerAPIRouter(router *mux.Router, bucketName string) {
	// Initialize API.
	api := objectAPIHandlers{
		ObjectAPI: newObjectLayerFn,
		CacheAPI:  newCachedObjectLayerFn,
	}

	var routers []*mux.Router
	// API Router
	apiRouter := router.PathPrefix(SlashSeparator).Subrouter()
	for _, domainName := range globalDomainNames {
		domainBucketName := fmt.Sprintf("%s.%s", bucketName, domainName)
		domainBucketNamePort := fmt.Sprintf("%s.%s:{port:.*}", bucketName, domainName)
		routers = append(routers, apiRouter.Host(domainBucketName).Subrouter())
		routers = append(routers, apiRouter.Host(domainBucketNamePort).Subrouter())
	}
	routers = append(routers, apiRouter.PathPrefix(fmt.Sprintf("/%s", bucketName)).Subrouter())

	for _, bucket := range routers {
		// Object operations
		// HeadObject
		bucket.Methods(http.MethodHead).Path("/{object:.+}").HandlerFunc(collectAPIStats("headobject", httpTraceAll(api.HeadObjectHandler)))
		// CopyObjectPart
		bucket.Methods(http.MethodPut).Path("/{object:.+}").HeadersRegexp(xhttp.AmzCopySource, ".*?(\\/|%2F).*?").HandlerFunc(collectAPIStats("copyobjectpart", httpTraceAll(api.CopyObjectPartHandler))).Queries("partNumber", "{partNumber:[0-9]+}", "uploadId", "{uploadId:.*}")
		// PutObjectPart
		bucket.Methods(http.MethodPut).Path("/{object:.+}").HandlerFunc(collectAPIStats("putobjectpart", httpTraceHdrs(api.PutObjectPartHandler))).Queries("partNumber", "{partNumber:[0-9]+}", "uploadId", "{uploadId:.*}")
		// ListObjectParts
		bucket.Methods(http.MethodGet).Path("/{object:.+}").HandlerFunc(collectAPIStats("listobjectparts", httpTraceAll(api.ListObjectPartsHandler))).Queries("uploadId", "{uploadId:.*}")
		// CompleteMultipartUpload
		bucket.Methods(http.MethodPost).Path("/{object:.+}").HandlerFunc(collectAPIStats("completemutipartupload", httpTraceAll(api.CompleteMultipartUploadHandler))).Queries("uploadId", "{uploadId:.*}")
		// NewMultipartUpload
		bucket.Methods(http.MethodPost).Path("/{object:.+}").HandlerFunc(collectAPIStats("newmultipartupload", httpTraceAll(api.NewMultipartUploadHandler))).Queries("uploads", "")
		// AbortMultipartUpload
		bucket.Methods(http.MethodDelete).Path("/{object:.+}").HandlerFunc(collectAPIStats("abortmultipartupload", httpTraceAll(api.AbortMultipartUploadHandler))).Queries("uploadId", "{uploadId:.*}")
		// SelectObjectContent
		bucket.Methods(http.MethodPost).Path("/{object:.+}").HandlerFunc(collectAPIStats("selectobjectcontent", httpTraceHdrs(api.SelectObjectContentHandler))).Queries("select", "").Queries("select-type", "2")
		// GetObject
		bucket.Methods(http.MethodGet).Path("/{object:.+}").HandlerFunc(collectAPIStats("getobject", httpTraceHdrs(api.GetObjectHandler)))
		// CopyObject
		bucket.Methods(http.MethodPut).Path("/{object:.+}").HeadersRegexp(xhttp.AmzCopySource, ".*?(\\/|%2F).*?").HandlerFunc(collectAPIStats("copyobject", httpTraceAll(api.CopyObjectHandler)))
		// PutObject
		bucket.Methods(http.MethodPut).Path("/{object:.+}").HandlerFunc(collectAPIStats("putobject", httpTraceHdrs(api.PutObjectHandler)))
		// DeleteObject
		bucket.Methods(http.MethodDelete).Path("/{object:.+}").HandlerFunc(collectAPIStats("deleteobject", httpTraceAll(api.DeleteObjectHandler)))

		/// Bucket operations
		// GetBucketLocation
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("getbucketlocation", httpTraceAll(api.GetBucketLocationHandler))).Queries("location", "")

		// ListMultipartUploads
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("listmultipartuploads", httpTraceAll(api.ListMultipartUploadsHandler))).Queries("uploads", "")
		// ListObjectsV2M
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("listobjectsv2M", httpTraceAll(api.ListObjectsV2MHandler))).Queries("list-type", "2", "metadata", "true")
		// ListObjectsV2
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("listobjectsv2", httpTraceAll(api.ListObjectsV2Handler))).Queries("list-type", "2")
		// ListBucketVersions
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("listbucketversions", httpTraceAll(api.ListBucketObjectVersionsHandler))).Queries("versions", "")
		// ListObjectsV1 (Legacy)
		bucket.Methods(http.MethodGet).HandlerFunc(collectAPIStats("listobjectsv1", httpTraceAll(api.ListObjectsV1Handler)))

		// HeadBucket
		bucket.Methods(http.MethodHead).HandlerFunc(collectAPIStats("headbucket", httpTraceAll(api.HeadBucketHandler)))
		// PostPolicy
		bucket.Methods(http.MethodPost).HeadersRegexp(xhttp.ContentType, "multipart/form-data*").HandlerFunc(collectAPIStats("postpolicybucket", httpTraceHdrs(api.PostPolicyBucketHandler)))
		// DeleteMultipleObjects
		bucket.Methods(http.MethodPost).HandlerFunc(collectAPIStats("deletemultipleobjects", httpTraceAll(api.DeleteMultipleObjectsHandler))).Queries("delete", "")
	}

	/// Root operation

	// ListBuckets
	apiRouter.Methods(http.MethodGet).Path(SlashSeparator).HandlerFunc(collectAPIStats("listbuckets", httpTraceAll(api.ListBucketsHandler)))

	// If none of the routes match add default error handler routes
	apiRouter.NotFoundHandler = collectAPIStats("notfound", httpTraceAll(errorResponseHandler))
	apiRouter.MethodNotAllowedHandler = collectAPIStats("methodnotallowed", httpTraceAll(errorResponseHandler))
}
