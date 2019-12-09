package cmd

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/cli"
	miniogo "github.com/minio/minio-go/v6"
	"github.com/minio/minio-go/v6/pkg/credentials"
	"github.com/minio/minio/pkg/dsync"
	"gopkg.in/yaml.v2"

	"github.com/minio/minio-go/v6/pkg/encrypt"
	"github.com/minio/minio-go/v6/pkg/s3utils"
	"github.com/minio/minio/pkg/sync/errgroup"
	"github.com/minio/radio/cmd/logger"
	"github.com/minio/radio/pkg/streamdup"
)

const radioTemplate = `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS]{{end}} [ENDPOINT...]
{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}

EXAMPLES:
  1. Start radio server
     {{.Prompt}} {{.HelpName}} -r config.yml
`

// Handler for 'minio radio s3' command line.
func radioMain(ctx *cli.Context) {
	data, err := ioutil.ReadFile(ctx.String("config"))
	logger.FatalIf(err, "Invalid command line arguments")

	rconfig := radioConfig{}
	logger.FatalIf(yaml.Unmarshal(data, &rconfig), "Invalid command line arguments")

	if ctx.Args().Present() {
		endpointZones, err := CreateServerEndpoints(ctx.GlobalString("address"), ctx.Args()...)
		logger.FatalIf(err, "Invalid command line arguments")

		if len(endpointZones) > 1 {
			logger.FatalIf(errors.New("too many arguments"), "Invalid command line arguments")
		}

		// Start the radio..
		StartRadio(ctx, &Radio{endpoints: endpointZones[0].Endpoints, rconfig: rconfig})
	} else {
		StartRadio(ctx, &Radio{rconfig: rconfig})
	}
}

// Radio implements active/active radioted radio
type Radio struct {
	endpoints Endpoints
	rconfig   radioConfig
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz01234569"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// randString generates random names and prepends them with a known prefix.
func randString(n int, src rand.Source, prefix string) string {
	b := make([]byte, n)
	// A rand.Int63() generates 63 random bits, enough for letterIdxMax letters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return prefix + string(b[0:30-len(prefix)])
}

// newS3 - Initializes a new client by auto probing S3 server signature.
func newS3(bucket, urlStr, accessKey, secretKey, sessionToken string) (*miniogo.Core, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	options := miniogo.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, sessionToken),
		Secure:       u.Scheme == "https",
		Region:       s3utils.GetRegionFromURL(*u),
		BucketLookup: miniogo.BucketLookupAuto,
	}

	clnt, err := miniogo.NewWithOptions(u.Host, &options)
	if err != nil {
		return nil, err
	}

	// Set custom transport
	clnt.SetCustomTransport(NewCustomHTTPTransport())

	// Check if the provided keys are valid.
	if _, err = clnt.BucketExists(bucket); err != nil {
		return nil, err
	}

	return &miniogo.Core{Client: clnt}, nil
}

type bucketConfig struct {
	Bucket       string `yaml:"bucket"`
	Endpoint     string `yaml:"endpoint"`
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
	SessionToken string `yaml:"sessionToken"`
}

// radioConfig radio configuration
type radioConfig struct {
	Mirror []struct {
		Local  bucketConfig   `yaml:"local"`
		Remote []bucketConfig `yaml:"remote"`
	} `yaml:"mirror"`
	Erasure []struct {
		Parity int            `yaml:"parity"`
		Local  bucketConfig   `yaml:"local"`
		Remote []bucketConfig `yaml:"remote"`
	} `yaml:"erasure"`
	// Future erasure
}

// NewRadioLayer returns s3 ObjectLayer.
func (g *Radio) NewRadioLayer() (ObjectLayer, error) {
	var radioLockers = make([]dsync.NetLocker, len(g.endpoints))
	for i, endpoint := range g.endpoints {
		radioLockers[i] = NewLockAPI(endpoint)
	}

	s := radioObjects{
		multipartUploadIDMap: make(map[string][]string),
		endpoints:            g.endpoints,
		radioLockers:         radioLockers,
		nsMutex:              NewNSLock(len(radioLockers) > 0),
		bucketClients:        make(map[string][]remoteS3),
	}

	// creds are ignored here, since S3 radio implements chaining all credentials.
	for _, remotes := range g.rconfig.Mirror {
		for _, bCfg := range remotes.Remote {
			clnt, err := newS3(bCfg.Bucket, bCfg.Endpoint, bCfg.AccessKey, bCfg.SecretKey, bCfg.SessionToken)
			if err != nil {
				return nil, err
			}
			s.bucketClients[remotes.Local.Bucket] = append(s.bucketClients[remotes.Local.Bucket], remoteS3{
				Core:   clnt,
				Bucket: bCfg.Bucket,
			})
		}
	}
	return &s, nil
}

// Production - radio radio is not yet production ready.
func (g *Radio) Production() bool {
	return true
}

type remoteS3 struct {
	*miniogo.Core
	Bucket string
}

// radioObjects implements radio for MinIO and S3 compatible object storage servers.
type radioObjects struct {
	endpoints            Endpoints
	radioLockers         []dsync.NetLocker
	bucketClients        map[string][]remoteS3
	multipartUploadIDMap map[string][]string
	nsMutex              *NSLockMap
}

func (l *radioObjects) NewNSLock(ctx context.Context, bucket string, object string) RWLocker {
	return l.nsMutex.NewNSLock(ctx, func() []dsync.NetLocker {
		return l.radioLockers
	}, bucket, object)
}

// GetBucketInfo gets bucket metadata..
func (l *radioObjects) GetBucketInfo(ctx context.Context, bucket string) (bi BucketInfo, e error) {
	_, ok := l.bucketClients[bucket]
	if !ok {
		return bi, BucketNotFound{Bucket: bucket}
	}
	return BucketInfo{
		Name:    bucket,
		Created: time.Now().UTC(),
	}, nil
}

// ListBuckets lists all S3 buckets
func (l *radioObjects) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	var b []BucketInfo
	for bucket := range l.bucketClients {
		b = append(b, BucketInfo{
			Name:    bucket,
			Created: time.Now().UTC(),
		})
	}
	return b, nil
}

// ListObjects lists all blobs in S3 bucket filtered by prefix
func (l *radioObjects) ListObjects(ctx context.Context, bucket string, prefix string, marker string, delimiter string, maxKeys int) (loi ListObjectsInfo, e error) {
	rs3 := l.bucketClients[bucket][0]
	result, err := rs3.ListObjects(rs3.Bucket, prefix, marker, delimiter, maxKeys)
	if err != nil {
		return loi, ErrorRespToObjectError(err, bucket)
	}

	return FromMinioClientListBucketResult(bucket, result), nil
}

// ListObjectsV2 lists all blobs in S3 bucket filtered by prefix
func (l *radioObjects) ListObjectsV2(ctx context.Context, bucket, prefix, continuationToken, delimiter string, maxKeys int, fetchOwner bool, startAfter string) (loi ListObjectsV2Info, e error) {
	rs3 := l.bucketClients[bucket][0]
	result, err := rs3.ListObjectsV2(rs3.Bucket, prefix, continuationToken, fetchOwner, delimiter, maxKeys, startAfter)
	if err != nil {
		return loi, ErrorRespToObjectError(err, bucket)
	}

	return FromMinioClientListBucketV2Result(bucket, result), nil
}

// GetObjectNInfo - returns object info and locked object ReadCloser
func (l *radioObjects) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, lockType LockType, opts ObjectOptions) (gr *GetObjectReader, err error) {
	var nsUnlocker = func() {}

	// Acquire lock
	if lockType != NoLock {
		lock := l.NewNSLock(ctx, bucket, object)
		switch lockType {
		case WriteLock:
			if err = lock.GetLock(globalObjectTimeout); err != nil {
				return nil, err
			}
			nsUnlocker = lock.Unlock
		case ReadLock:
			if err = lock.GetRLock(globalObjectTimeout); err != nil {
				return nil, err
			}
			nsUnlocker = lock.RUnlock
		}
	}

	var objInfo ObjectInfo
	objInfo, err = l.GetObjectInfo(ctx, bucket, object, opts)
	if err != nil {
		return nil, ErrorRespToObjectError(err, bucket, object)
	}

	var startOffset, length int64
	startOffset, length, err = rs.GetOffsetLength(objInfo.Size)
	if err != nil {
		return nil, ErrorRespToObjectError(err, bucket, object)
	}

	pr, pw := io.Pipe()
	go func() {
		err := l.GetObject(ctx, bucket, object, startOffset, length, pw, objInfo.ETag, opts)
		pw.CloseWithError(err)
	}()

	// Setup cleanup function to cause the above go-routine to
	// exit in case of partial read
	pipeCloser := func() { pr.Close() }
	return NewGetObjectReaderFromReader(pr, objInfo, opts.CheckCopyPrecondFn, pipeCloser, nsUnlocker)
}

// GetObject reads an object from S3. Supports additional
// parameters like offset and length which are synonymous with
// HTTP Range requests.
//
// startOffset indicates the starting read location of the object.
// length indicates the total length of the object.
func (l *radioObjects) GetObject(ctx context.Context, bucket string, object string, startOffset int64, length int64, writer io.Writer, etag string, o ObjectOptions) error {
	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetRLock(globalObjectTimeout); err != nil {
		return err
	}
	defer objectLock.RUnlock()

	if length < 0 && length != -1 {
		return ErrorRespToObjectError(InvalidRange{}, bucket, object)
	}

	opts := miniogo.GetObjectOptions{}
	opts.ServerSideEncryption = o.ServerSideEncryption

	if startOffset >= 0 && length >= 0 {
		if err := opts.SetRange(startOffset, startOffset+length-1); err != nil {
			return ErrorRespToObjectError(err, bucket, object)
		}
	}
	rs3 := l.bucketClients[bucket][0]
	reader, _, _, err := rs3.GetObject(rs3.Bucket, object, opts)
	if err != nil {
		return ErrorRespToObjectError(err, bucket, object)
	}
	defer reader.Close()

	if _, err = io.Copy(writer, reader); err != nil {
		return ErrorRespToObjectError(err, bucket, object)
	}

	return nil
}

// GetObjectInfo reads object info and replies back ObjectInfo
func (l *radioObjects) GetObjectInfo(ctx context.Context, bucket string, object string, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetRLock(globalObjectTimeout); err != nil {
		return ObjectInfo{}, err
	}
	defer objectLock.RUnlock()

	rs3 := l.bucketClients[bucket][0]
	oi, err := rs3.StatObject(rs3.Bucket, object, miniogo.StatObjectOptions{
		GetObjectOptions: miniogo.GetObjectOptions{
			ServerSideEncryption: opts.ServerSideEncryption,
		},
	})
	if err != nil {
		return ObjectInfo{}, ErrorRespToObjectError(err, bucket, object)
	}

	return FromMinioClientObjectInfo(bucket, oi), nil
}

// PutObject creates a new object with the incoming data,
func (l *radioObjects) PutObject(ctx context.Context, bucket string, object string, r *PutObjReader, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	data := r.Reader

	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetLock(globalObjectTimeout); err != nil {
		return ObjectInfo{}, err
	}
	defer objectLock.Unlock()

	rs3s := l.bucketClients[bucket]

	readers, err := streamdup.New(data, len(rs3s))
	if err != nil {
		return objInfo, ErrorRespToObjectError(err, bucket, object)
	}

	oinfos := make([]miniogo.ObjectInfo, len(rs3s))
	g := errgroup.WithNErrs(len(rs3s))
	for index := range rs3s {
		index := index
		g.Go(func() error {
			var perr error
			oinfos[index], perr = rs3s[index].PutObject(rs3s[index].Bucket, object, readers[index], data.Size(),
				data.MD5Base64String(), data.SHA256HexString(),
				ToMinioClientMetadata(opts.UserDefined), opts.ServerSideEncryption)
			return perr
		}, index)
	}

	for _, err = range g.Wait() {
		if err != nil {
			return objInfo, ErrorRespToObjectError(err, bucket, object)
		}
	}

	oi := oinfos[0]
	// On success, populate the key & metadata so they are present in the notification
	oi.Key = object
	oi.Metadata = ToMinioClientObjectInfoMetadata(opts.UserDefined)

	return FromMinioClientObjectInfo(bucket, oi), nil
}

// CopyObject copies an object from source bucket to a destination bucket.
func (l *radioObjects) CopyObject(ctx context.Context, srcBucket string, srcObject string, dstBucket string, dstObject string, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (objInfo ObjectInfo, err error) {
	// Check if this request is only metadata update.
	cpSrcDstSame := IsStringEqual(PathJoin(srcBucket, srcObject), PathJoin(dstBucket, dstObject))
	if !cpSrcDstSame {
		objectLock := l.NewNSLock(ctx, dstBucket, dstObject)
		if err = objectLock.GetLock(globalObjectTimeout); err != nil {
			return objInfo, err
		}
		defer objectLock.Unlock()
	}

	if srcOpts.CheckCopyPrecondFn != nil && srcOpts.CheckCopyPrecondFn(srcInfo, "") {
		return ObjectInfo{}, PreConditionFailed{}
	}
	// Set this header such that following CopyObject() always sets the right metadata on the destination.
	// metadata input is already a trickled down value from interpreting x-amz-metadata-directive at
	// handler layer. So what we have right now is supposed to be applied on the destination object anyways.
	// So preserve it by adding "REPLACE" directive to save all the metadata set by CopyObject API.
	srcInfo.UserDefined["x-amz-metadata-directive"] = "REPLACE"
	srcInfo.UserDefined["x-amz-copy-source-if-match"] = srcInfo.ETag
	header := make(http.Header)
	if srcOpts.ServerSideEncryption != nil {
		encrypt.SSECopy(srcOpts.ServerSideEncryption).Marshal(header)
	}

	if dstOpts.ServerSideEncryption != nil {
		dstOpts.ServerSideEncryption.Marshal(header)
	}
	for k, v := range header {
		srcInfo.UserDefined[k] = v[0]
	}

	rs3sSrc := l.bucketClients[srcBucket]
	rs3sDest := l.bucketClients[dstBucket]
	if len(rs3sSrc) != len(rs3sDest) {
		return objInfo, errors.New("unexpected")
	}

	n := len(rs3sDest)
	oinfos := make([]miniogo.ObjectInfo, n)

	g := errgroup.WithNErrs(n)
	for index := 0; index < n; index++ {
		index := index
		g.Go(func() error {
			var err error
			oinfos[index], err = rs3sSrc[index].CopyObject(rs3sSrc[index].Bucket, srcObject,
				rs3sDest[index].Bucket, dstObject, srcInfo.UserDefined)
			return err
		}, index)
	}

	for _, err := range g.Wait() {
		if err != nil {
			return objInfo, ErrorRespToObjectError(err, srcBucket, srcObject)
		}
	}

	return l.GetObjectInfo(ctx, dstBucket, dstObject, dstOpts)
}

// DeleteObject deletes a blob in bucket
func (l *radioObjects) DeleteObject(ctx context.Context, bucket string, object string) error {
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer objectLock.Unlock()

	rs3s := l.bucketClients[bucket]
	for _, clnt := range rs3s {
		if err := clnt.RemoveObject(clnt.Bucket, object); err != nil {
			return ErrorRespToObjectError(err, bucket, object)
		}
	}

	return nil
}

func (l *radioObjects) DeleteObjects(ctx context.Context, bucket string, objects []string) ([]error, error) {
	errs := make([]error, len(objects))
	for idx, object := range objects {
		errs[idx] = l.DeleteObject(ctx, bucket, object)
	}
	return errs, nil
}

// ListMultipartUploads lists all multipart uploads.
func (l *radioObjects) ListMultipartUploads(ctx context.Context, bucket string, prefix string, keyMarker string, uploadIDMarker string, delimiter string, maxUploads int) (lmi ListMultipartsInfo, e error) {
	rs3 := l.bucketClients[bucket][0]
	result, err := rs3.ListMultipartUploads(rs3.Bucket, prefix, keyMarker, uploadIDMarker, delimiter, maxUploads)
	if err != nil {
		return lmi, err
	}

	return FromMinioClientListMultipartsInfo(result), nil
}

// NewMultipartUpload upload object in multiple parts
func (l *radioObjects) NewMultipartUpload(ctx context.Context, bucket string, object string, o ObjectOptions) (string, error) {

	// Create PutObject options
	opts := miniogo.PutObjectOptions{UserMetadata: o.UserDefined, ServerSideEncryption: o.ServerSideEncryption}
	uploadID := MustGetUUID()

	uploadIDLock := l.NewNSLock(ctx, bucket, PathJoin(object, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return uploadID, err
	}
	defer uploadIDLock.Unlock()

	rs3s := l.bucketClients[bucket]
	for _, clnt := range rs3s {
		id, err := clnt.NewMultipartUpload(clnt.Bucket, object, opts)
		if err != nil {
			// Abort any failed uploads to one of the radios
			clnt.AbortMultipartUpload(clnt.Bucket, object, uploadID)
			return uploadID, ErrorRespToObjectError(err, bucket, object)
		}
		l.multipartUploadIDMap[uploadID] = append(l.multipartUploadIDMap[uploadID], id)

	}
	return uploadID, nil
}

// PutObjectPart puts a part of object in bucket
func (l *radioObjects) PutObjectPart(ctx context.Context, bucket string, object string, uploadID string, partID int, r *PutObjReader, opts ObjectOptions) (pi PartInfo, e error) {
	data := r.Reader

	uploadIDLock := l.NewNSLock(ctx, bucket, PathJoin(object, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return pi, err
	}
	defer uploadIDLock.Unlock()

	uploadIDs, ok := l.multipartUploadIDMap[uploadID]
	if !ok {
		return pi, InvalidUploadID{
			Bucket:   bucket,
			Object:   object,
			UploadID: uploadID,
		}
	}

	rs3s := l.bucketClients[bucket]

	readers, err := streamdup.New(data, len(rs3s))
	if err != nil {
		return pi, err
	}

	pinfos := make([]miniogo.ObjectPart, len(rs3s))
	g := errgroup.WithNErrs(len(rs3s))
	for index := range rs3s {
		index := index
		g.Go(func() error {
			var err error
			pinfos[index], err = rs3s[index].PutObjectPart(rs3s[index].Bucket, object, uploadIDs[index], partID, readers[index], data.Size(), data.MD5Base64String(), data.SHA256HexString(), opts.ServerSideEncryption)
			return err
		}, index)
	}

	for _, err := range g.Wait() {
		if err != nil {
			return pi, ErrorRespToObjectError(err, bucket, object)
		}
	}

	return FromMinioClientObjectPart(pinfos[0]), nil
}

// CopyObjectPart creates a part in a multipart upload by copying
// existing object or a part of it.
func (l *radioObjects) CopyObjectPart(ctx context.Context, srcBucket, srcObject, destBucket, destObject, uploadID string,
	partID int, startOffset, length int64, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (p PartInfo, err error) {

	uploadIDLock := l.NewNSLock(ctx, destBucket, PathJoin(destObject, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return p, err
	}
	defer uploadIDLock.Unlock()

	if srcOpts.CheckCopyPrecondFn != nil && srcOpts.CheckCopyPrecondFn(srcInfo, "") {
		return PartInfo{}, PreConditionFailed{}
	}
	srcInfo.UserDefined = map[string]string{
		"x-amz-copy-source-if-match": srcInfo.ETag,
	}
	header := make(http.Header)
	if srcOpts.ServerSideEncryption != nil {
		encrypt.SSECopy(srcOpts.ServerSideEncryption).Marshal(header)
	}

	if dstOpts.ServerSideEncryption != nil {
		dstOpts.ServerSideEncryption.Marshal(header)
	}
	for k, v := range header {
		srcInfo.UserDefined[k] = v[0]
	}

	uploadIDs, ok := l.multipartUploadIDMap[uploadID]
	if !ok {
		return p, InvalidUploadID{
			Bucket:   srcBucket,
			Object:   srcObject,
			UploadID: uploadID,
		}
	}

	rs3sSrc := l.bucketClients[srcBucket]
	rs3sDest := l.bucketClients[destBucket]

	if len(rs3sSrc) != len(rs3sDest) {
		return p, errors.New("unexpected")
	}

	n := len(rs3sDest)
	pinfos := make([]miniogo.CompletePart, n)

	g := errgroup.WithNErrs(n)
	for index := 0; index < n; index++ {
		index := index
		g.Go(func() error {
			var err error
			pinfos[index], err = rs3sSrc[index].CopyObjectPart(rs3sSrc[index].Bucket, srcObject, rs3sDest[index].Bucket, destObject,
				uploadIDs[index], partID, startOffset, length, srcInfo.UserDefined)
			return err
		}, index)
	}

	for _, err := range g.Wait() {
		if err != nil {
			return p, ErrorRespToObjectError(err, srcBucket, srcObject)
		}
	}
	p.PartNumber = pinfos[0].PartNumber
	p.ETag = pinfos[0].ETag
	return p, nil
}

// ListObjectParts returns all object parts for specified object in specified bucket
func (l *radioObjects) ListObjectParts(ctx context.Context, bucket string, object string, uploadID string, partNumberMarker int, maxParts int, opts ObjectOptions) (lpi ListPartsInfo, e error) {
	return lpi, nil
}

// AbortMultipartUpload aborts a ongoing multipart upload
func (l *radioObjects) AbortMultipartUpload(ctx context.Context, bucket string, object string, uploadID string) error {
	uploadIDLock := l.NewNSLock(ctx, bucket, PathJoin(object, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return err
	}
	defer uploadIDLock.Unlock()

	uploadIDs, ok := l.multipartUploadIDMap[uploadID]
	if !ok {
		return InvalidUploadID{
			Bucket:   bucket,
			Object:   object,
			UploadID: uploadID,
		}
	}

	rs3s := l.bucketClients[bucket]
	for index, id := range uploadIDs {
		if err := rs3s[index].AbortMultipartUpload(rs3s[index].Bucket, object, id); err != nil {
			return ErrorRespToObjectError(err, bucket, object)
		}
	}
	delete(l.multipartUploadIDMap, uploadID)
	return nil
}

// CompleteMultipartUpload completes ongoing multipart upload and finalizes object
func (l *radioObjects) CompleteMultipartUpload(ctx context.Context, bucket string, object string, uploadID string, uploadedParts []CompletePart, opts ObjectOptions) (oi ObjectInfo, err error) {

	// Hold read-locks to verify uploaded parts, also disallows
	// parallel part uploads as well.
	uploadIDLock := l.NewNSLock(ctx, bucket, PathJoin(object, uploadID))
	if err = uploadIDLock.GetRLock(globalOperationTimeout); err != nil {
		return oi, err
	}
	defer uploadIDLock.RUnlock()

	// Hold namespace to complete the transaction, only hold
	// if uploadID can be held exclusively.
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err = objectLock.GetLock(globalOperationTimeout); err != nil {
		return oi, err
	}
	defer objectLock.Unlock()

	uploadIDs, ok := l.multipartUploadIDMap[uploadID]
	if !ok {
		return oi, InvalidUploadID{
			Bucket:   bucket,
			Object:   object,
			UploadID: uploadID,
		}
	}

	rs3s := l.bucketClients[bucket]
	var etag string
	for index, id := range uploadIDs {
		etag, err = rs3s[index].CompleteMultipartUpload(rs3s[index].Bucket, object, id,
			ToMinioClientCompleteParts(uploadedParts))
		if err != nil {
			return oi, ErrorRespToObjectError(err, bucket, object)
		}
	}
	delete(l.multipartUploadIDMap, uploadID)
	return ObjectInfo{Bucket: bucket, Name: object, ETag: etag}, nil
}
