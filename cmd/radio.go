package cmd

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
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
     {{.Prompt}} {{.HelpName}} -c config.yml
`

// Handler for 'minio radio s3' command line.
func radioMain(ctx *cli.Context) {
	data, err := ioutil.ReadFile(ctx.String("config"))
	if err != nil {
		logger.FatalIf(err, "Invalid command line arguments")
	}

	rconfig := radioConfig{}
	err = yaml.Unmarshal(data, &rconfig)
	if err != nil {
		logger.FatalIf(err, "Invalid command line arguments")
	}

	endpoints, err := createServerEndpoints(ctx.String("address"), rconfig.Distribute.Peers)
	logger.FatalIf(err, "Invalid command line arguments")

	if len(endpoints) > 0 {
		startRadio(ctx, &Radio{endpoints: endpoints, rconfig: rconfig})
	} else {
		startRadio(ctx, &Radio{rconfig: rconfig})
	}
}

// Radio implements active/active radioted radio
type Radio struct {
	endpoints Endpoints
	rconfig   radioConfig
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
	Distribute struct {
		Peers string `yaml:"peers"`
		Token string `yaml:"token"`
		Certs struct {
			CertFile string `yaml:"cert_file"`
			KeyFile  string `yaml:"key_file"`
			CAPath   string `yaml:"ca_path"`
		} `yaml:"certs"`
	} `yaml:"distribute"`
	Cache struct {
		Drives  []string `yaml:"drives"`
		Exclude []string `yaml:"exclude"`
		Quota   int      `yaml:"quota"`
		Expiry  int      `yaml:"expiry"`
	} `yaml:"cache"`
	Mirror []struct {
		Local  bucketConfig   `yaml:"local"`
		Remote []bucketConfig `yaml:"remote"`
	} `yaml:"mirror"`
	Erasure []struct {
		Parity int            `yaml:"parity"`
		Local  bucketConfig   `yaml:"local"`
		Remote []bucketConfig `yaml:"remote"`
	} `yaml:"erasure"`
}

func newBucketClients(bcfgs []bucketConfig) ([]bucketClient, error) {
	var clnts []bucketClient
	for _, bCfg := range bcfgs {
		clnt, err := newS3(bCfg.Bucket, bCfg.Endpoint, bCfg.AccessKey, bCfg.SecretKey, bCfg.SessionToken)
		if err != nil {
			return nil, err
		}
		clnts = append(clnts, bucketClient{
			Core:   clnt,
			Bucket: bCfg.Bucket,
		})
	}
	return clnts, nil
}

// NewRadioLayer returns s3 ObjectLayer.
func (g *Radio) NewRadioLayer() (ObjectLayer, error) {
	var radioLockers []dsync.NetLocker
	for _, endpoint := range g.endpoints {
		radioLockers = append(radioLockers, newLockAPI(endpoint))
	}

	s := radioObjects{
		multipartUploadIDMap: make(map[string][]string),
		endpoints:            g.endpoints,
		radioLockers:         radioLockers,
		nsMutex:              newNSLock(len(radioLockers) > 0),
		mirrorClients:        make(map[string]mirrorConfig),
		erasureClients:       make(map[string]erasureConfig),
	}

	// creds are ignored here, since S3 radio implements chaining all credentials.
	for _, remotes := range g.rconfig.Mirror {
		clnts, err := newBucketClients(remotes.Remote)
		if err != nil {
			return nil, err
		}
		s.mirrorClients[remotes.Local.Bucket] = mirrorConfig{
			clnts: clnts,
		}
	}
	for _, remotes := range g.rconfig.Erasure {
		clnts, err := newBucketClients(remotes.Remote)
		if err != nil {
			return nil, err
		}
		s.erasureClients[remotes.Local.Bucket] = erasureConfig{
			parity: remotes.Parity,
			clnts:  clnts,
		}
	}
	return &s, nil
}

// Production - radio radio is not yet production ready.
func (g *Radio) Production() bool {
	return true
}

type bucketClient struct {
	*miniogo.Core
	Bucket string
}

type mirrorConfig struct {
	clnts []bucketClient
}

type erasureConfig struct {
	parity int
	clnts  []bucketClient
}

// radioObjects implements radio for MinIO and S3 compatible object storage servers.
type radioObjects struct {
	endpoints            Endpoints
	radioLockers         []dsync.NetLocker
	mirrorClients        map[string]mirrorConfig
	erasureClients       map[string]erasureConfig
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
	_, ok := l.mirrorClients[bucket]
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
	for bucket := range l.mirrorClients {
		b = append(b, BucketInfo{
			Name:    bucket,
			Created: time.Now().UTC(),
		})
	}
	return b, nil
}

// ListObjects lists all blobs in S3 bucket filtered by prefix
func (l *radioObjects) ListObjects(ctx context.Context, bucket string, prefix string, marker string, delimiter string, maxKeys int) (loi ListObjectsInfo, e error) {
	rs3, ok := l.mirrorClients[bucket]
	if !ok {
		return loi, BucketNotFound{
			Bucket: bucket,
		}
	}
	result, err := rs3.clnts[0].ListObjects(rs3.clnts[0].Bucket, prefix, marker, delimiter, maxKeys)
	if err != nil {
		return loi, ErrorRespToObjectError(err, bucket)
	}

	return FromMinioClientListBucketResult(bucket, result), nil
}

// ListObjectsV2 lists all blobs in S3 bucket filtered by prefix
func (l *radioObjects) ListObjectsV2(ctx context.Context, bucket, prefix, continuationToken, delimiter string, maxKeys int, fetchOwner bool, startAfter string) (loi ListObjectsV2Info, e error) {
	rs3, ok := l.mirrorClients[bucket]
	if !ok {
		return loi, BucketNotFound{
			Bucket: bucket,
		}
	}
	result, err := rs3.clnts[0].ListObjectsV2(rs3.clnts[0].Bucket, prefix,
		continuationToken, fetchOwner, delimiter, maxKeys, startAfter)
	if err != nil {
		return loi, ErrorRespToObjectError(err, bucket)
	}

	return FromMinioClientListBucketV2Result(bucket, result), nil
}

// GetObjectNInfo - returns object info and locked object ReadCloser
func (l *radioObjects) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, lockType LockType, o ObjectOptions) (gr *GetObjectReader, err error) {
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

	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return nil, BucketNotFound{
			Bucket: bucket,
		}
	}

	info, err := l.getObjectInfo(ctx, bucket, object, o)
	if err != nil {
		return nil, ErrorRespToObjectError(err, bucket, object)
	}

	startOffset, length, err := rs.GetOffsetLength(info.Size)
	if err != nil {
		return nil, ErrorRespToObjectError(err, bucket, object)
	}

	pr, pw := io.Pipe()
	go func() {
		opts := miniogo.GetObjectOptions{}
		opts.ServerSideEncryption = o.ServerSideEncryption

		if startOffset >= 0 && length >= 0 {
			if err := opts.SetRange(startOffset, startOffset+length-1); err != nil {
				pw.CloseWithError(ErrorRespToObjectError(err, bucket, object))
				return
			}
		}

		reader, _, _, err := rs3s.clnts[info.ReplicaIndex].GetObject(rs3s.clnts[info.ReplicaIndex].Bucket, object, opts)
		if err != nil {
			pw.CloseWithError(ErrorRespToObjectError(err, bucket, object))
			return
		}
		defer reader.Close()

		_, err = io.Copy(pw, reader)
		pw.CloseWithError(ErrorRespToObjectError(err, bucket, object))
	}()

	// Setup cleanup function to cause the above go-routine to
	// exit in case of partial read
	pipeCloser := func() { pr.Close() }
	return NewGetObjectReaderFromReader(pr, info, o.CheckCopyPrecondFn, pipeCloser, nsUnlocker)
}

// GetObject reads an object from S3. Supports additional
// parameters like offset and length which are synonymous with
// HTTP Range requests.
//
// startOffset indicates the starting read location of the object.
// length indicates the total length of the object.
func (l *radioObjects) GetObject(ctx context.Context, bucket string, object string, startOffset int64, length int64, writer io.Writer, etag string, o ObjectOptions) error {
	return NotImplemented{}
}

func quorumInfo(infos []miniogo.ObjectInfo) (miniogo.ObjectInfo, int, error) {
	tagCounter := map[string]int{}
	for _, info := range infos {
		uuid := info.Metadata.Get("x-amz-meta-radio-tag")
		_, ok := tagCounter[uuid]
		if !ok {
			tagCounter[uuid] = 1
		} else {
			tagCounter[uuid]++
		}
	}
	var maximalUUID string
	for uuid, count := range tagCounter {
		if count > len(infos)/2+1 {
			maximalUUID = uuid
			break
		}
	}
	if maximalUUID == "" {
		return miniogo.ObjectInfo{}, -1, InsufficientReadQuorum{}
	}

	var info miniogo.ObjectInfo
	var index int
	for index, info = range infos {
		uuid := info.Metadata.Get("x-amz-meta-radio-tag")
		if uuid != maximalUUID {
			continue
		}
		break
	}
	return info, index, nil
}

func (l *radioObjects) getObjectInfo(ctx context.Context, bucket string, object string, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return ObjectInfo{}, BucketNotFound{
			Bucket: bucket,
		}
	}

	oinfos := make([]miniogo.ObjectInfo, len(rs3s.clnts))
	g := errgroup.WithNErrs(len(rs3s.clnts))
	for index := range rs3s.clnts {
		index := index
		g.Go(func() error {
			var perr error
			oinfos[index], perr = rs3s.clnts[index].StatObject(rs3s.clnts[index].Bucket,
				object, miniogo.StatObjectOptions{
					GetObjectOptions: miniogo.GetObjectOptions{
						ServerSideEncryption: opts.ServerSideEncryption,
					},
				})
			return perr
		}, index)
	}

	if maxErr := reduceReadQuorumErrs(ctx, g.Wait(), nil, len(rs3s.clnts)/2+1); maxErr != nil {
		return ObjectInfo{}, maxErr
	}

	info, rindex, err := quorumInfo(oinfos)
	if err != nil {
		return ObjectInfo{}, ErrorRespToObjectError(err, bucket, object)
	}

	return FromMinioClientObjectInfo(bucket, info, rindex), nil
}

// GetObjectInfo reads object info and replies back ObjectInfo
func (l *radioObjects) GetObjectInfo(ctx context.Context, bucket string, object string, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetRLock(globalObjectTimeout); err != nil {
		return ObjectInfo{}, err
	}
	defer objectLock.RUnlock()

	return l.getObjectInfo(ctx, bucket, object, opts)
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

	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return objInfo, BucketNotFound{Bucket: bucket}
	}

	readers, err := streamdup.New(data, len(rs3s.clnts))
	if err != nil {
		return objInfo, ErrorRespToObjectError(err, bucket, object)
	}

	opts.UserDefined["x-amz-meta-radio-tag"] = mustGetUUID()

	oinfos := make([]miniogo.ObjectInfo, len(rs3s.clnts))
	g := errgroup.WithNErrs(len(rs3s.clnts))
	for index := range rs3s.clnts {
		index := index
		g.Go(func() error {
			var perr error
			oinfos[index], perr = rs3s.clnts[index].PutObject(rs3s.clnts[index].Bucket, object,
				readers[index], data.Size(),
				data.MD5Base64String(), data.SHA256HexString(),
				ToMinioClientMetadata(opts.UserDefined), opts.ServerSideEncryption)
			oinfos[index].Key = object
			oinfos[index].Metadata = ToMinioClientObjectInfoMetadata(opts.UserDefined)
			return perr
		}, index)
	}

	errs := g.Wait()
	if maxErr := reduceWriteQuorumErrs(ctx, errs, nil, len(rs3s.clnts)/2+1); maxErr != nil {
		for index, err := range errs {
			if err == nil {
				rs3s.clnts[index].RemoveObject(rs3s.clnts[index].Bucket, object)
			}
		}
		return objInfo, maxErr
	}

	info, rindex, err := quorumInfo(oinfos)
	if err != nil {
		return objInfo, err
	}

	return FromMinioClientObjectInfo(bucket, info, rindex), nil
}

// CopyObject copies an object from source bucket to a destination bucket.
func (l *radioObjects) CopyObject(ctx context.Context, srcBucket string, srcObject string, dstBucket string, dstObject string, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (objInfo ObjectInfo, err error) {
	// Check if this request is only metadata update.
	cpSrcDstSame := isStringEqual(pathJoin(srcBucket, srcObject), pathJoin(dstBucket, dstObject))
	if !cpSrcDstSame {
		objectLock := l.NewNSLock(ctx, dstBucket, dstObject)
		if err = objectLock.GetLock(globalObjectTimeout); err != nil {
			return objInfo, err
		}
		defer objectLock.Unlock()
	}

	if srcOpts.CheckCopyPrecondFn != nil && srcOpts.CheckCopyPrecondFn(srcInfo) {
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

	rs3sSrc := l.mirrorClients[srcBucket]
	rs3sDest := l.mirrorClients[dstBucket]
	if len(rs3sSrc.clnts) != len(rs3sDest.clnts) {
		return objInfo, errors.New("unexpected")
	}

	n := len(rs3sDest.clnts)
	oinfos := make([]miniogo.ObjectInfo, n)

	g := errgroup.WithNErrs(n)
	for index := 0; index < n; index++ {
		index := index
		g.Go(func() error {
			var err error
			oinfos[index], err = rs3sSrc.clnts[index].CopyObject(rs3sSrc.clnts[index].Bucket, srcObject,
				rs3sDest.clnts[index].Bucket, dstObject, srcInfo.UserDefined)
			return err
		}, index)
	}

	errs := g.Wait()
	if maxErr := reduceWriteQuorumErrs(ctx, errs, nil, len(rs3sSrc.clnts)/2+1); maxErr != nil {
		for index, err := range errs {
			if err == nil {
				rs3sDest.clnts[index].RemoveObject(rs3sDest.clnts[index].Bucket, dstObject)
			}
		}
		return objInfo, maxErr
	}

	return l.getObjectInfo(ctx, dstBucket, dstObject, dstOpts)
}

// DeleteObject deletes a blob in bucket
func (l *radioObjects) DeleteObject(ctx context.Context, bucket string, object string) error {
	objectLock := l.NewNSLock(ctx, bucket, object)
	if err := objectLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer objectLock.Unlock()

	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return BucketNotFound{
			Bucket: bucket,
		}
	}

	n := len(rs3s.clnts)
	g := errgroup.WithNErrs(n)
	for index := 0; index < n; index++ {
		index := index
		g.Go(func() error {
			return rs3s.clnts[index].RemoveObject(rs3s.clnts[index].Bucket, object)
		}, index)
	}

	return reduceWriteQuorumErrs(ctx, g.Wait(), nil, len(rs3s.clnts)/2+1)
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
	rs3, ok := l.mirrorClients[bucket]
	if !ok {
		return lmi, BucketNotFound{Bucket: bucket}
	}

	result, err := rs3.clnts[0].ListMultipartUploads(rs3.clnts[0].Bucket, prefix,
		keyMarker, uploadIDMarker, delimiter, maxUploads)
	if err != nil {
		return lmi, err
	}

	return FromMinioClientListMultipartsInfo(result), nil
}

// NewMultipartUpload upload object in multiple parts
func (l *radioObjects) NewMultipartUpload(ctx context.Context, bucket string, object string, o ObjectOptions) (string, error) {

	// Create PutObject options
	opts := miniogo.PutObjectOptions{UserMetadata: o.UserDefined, ServerSideEncryption: o.ServerSideEncryption}
	uploadID := mustGetUUID()

	uploadIDLock := l.NewNSLock(ctx, bucket, pathJoin(object, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return uploadID, err
	}
	defer uploadIDLock.Unlock()

	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return uploadID, BucketNotFound{Bucket: bucket}
	}

	for _, clnt := range rs3s.clnts {
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

	uploadIDLock := l.NewNSLock(ctx, bucket, pathJoin(object, uploadID))
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

	rs3s := l.mirrorClients[bucket]

	readers, err := streamdup.New(data, len(rs3s.clnts))
	if err != nil {
		return pi, err
	}

	pinfos := make([]miniogo.ObjectPart, len(rs3s.clnts))
	g := errgroup.WithNErrs(len(rs3s.clnts))
	for index := range rs3s.clnts {
		index := index
		g.Go(func() error {
			var err error
			pinfos[index], err = rs3s.clnts[index].PutObjectPart(rs3s.clnts[index].Bucket, object,
				uploadIDs[index], partID, readers[index], data.Size(),
				data.MD5Base64String(), data.SHA256HexString(), opts.ServerSideEncryption)
			return err
		}, index)
	}

	if maxErr := reduceWriteQuorumErrs(ctx, g.Wait(), nil, len(rs3s.clnts)/2+1); maxErr != nil {
		return pi, maxErr
	}

	return FromMinioClientObjectPart(pinfos[0]), nil
}

// CopyObjectPart creates a part in a multipart upload by copying
// existing object or a part of it.
func (l *radioObjects) CopyObjectPart(ctx context.Context, srcBucket, srcObject, destBucket, destObject, uploadID string,
	partID int, startOffset, length int64, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (p PartInfo, err error) {

	uploadIDLock := l.NewNSLock(ctx, destBucket, pathJoin(destObject, uploadID))
	if err := uploadIDLock.GetLock(globalOperationTimeout); err != nil {
		return p, err
	}
	defer uploadIDLock.Unlock()

	if srcOpts.CheckCopyPrecondFn != nil && srcOpts.CheckCopyPrecondFn(srcInfo) {
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

	rs3sSrc := l.mirrorClients[srcBucket]
	rs3sDest := l.mirrorClients[destBucket]

	if len(rs3sSrc.clnts) != len(rs3sDest.clnts) {
		return p, errors.New("unexpected")
	}

	n := len(rs3sDest.clnts)
	pinfos := make([]miniogo.CompletePart, n)

	g := errgroup.WithNErrs(n)
	for index := 0; index < n; index++ {
		index := index
		g.Go(func() error {
			var err error
			pinfos[index], err = rs3sSrc.clnts[index].CopyObjectPart(rs3sSrc.clnts[index].Bucket,
				srcObject, rs3sDest.clnts[index].Bucket, destObject,
				uploadIDs[index], partID, startOffset, length, srcInfo.UserDefined)
			return err
		}, index)
	}

	if maxErr := reduceWriteQuorumErrs(ctx, g.Wait(), nil, len(rs3sDest.clnts)/2+1); maxErr != nil {
		return p, maxErr
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
	uploadIDLock := l.NewNSLock(ctx, bucket, pathJoin(object, uploadID))
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

	rs3s := l.mirrorClients[bucket]
	for index, id := range uploadIDs {
		if err := rs3s.clnts[index].AbortMultipartUpload(rs3s.clnts[index].Bucket, object, id); err != nil {
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
	uploadIDLock := l.NewNSLock(ctx, bucket, pathJoin(object, uploadID))
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

	rs3s := l.mirrorClients[bucket]
	var etag string
	for index, id := range uploadIDs {
		etag, err = rs3s.clnts[index].CompleteMultipartUpload(rs3s.clnts[index].Bucket,
			object, id, ToMinioClientCompleteParts(uploadedParts))
		if err != nil {
			return oi, ErrorRespToObjectError(err, bucket, object)
		}
	}
	delete(l.multipartUploadIDMap, uploadID)
	return ObjectInfo{Bucket: bucket, Name: object, ETag: etag}, nil
}
