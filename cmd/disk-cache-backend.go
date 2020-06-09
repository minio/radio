package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/djherbis/atime"
	"github.com/minio/highwayhash"
	"github.com/minio/minio/pkg/disk"
	xhttp "github.com/minio/radio/cmd/http"
	"github.com/minio/radio/cmd/logger"
	"github.com/minio/sha256-simd"
	"go.uber.org/atomic"
	"golang.org/x/crypto/blake2b"
)

const (
	// cache.json object metadata for cached objects.
	cacheMetaJSONFile = "cache.json"
	cacheDataFile     = "part.1"
	cacheMetaVersion  = "1.0.0"
	cacheExpiryDays   = time.Duration(90 * time.Hour * 24) // defaults to 90 days
)

// CacheChecksumInfoV1 - carries checksums of individual blocks on disk.
type CacheChecksumInfoV1 struct {
	Algorithm string `json:"algorithm"`
	Blocksize int64  `json:"blocksize"`
}

// statInfo - carries stat information of the object.
type statInfo struct {
	Size    int64     `json:"size"`    // Size of the object `xl.json`.
	ModTime time.Time `json:"modTime"` // ModTime of the object `xl.json`.
}

// Represents the cache metadata struct
type cacheMeta struct {
	Version string   `json:"version"`
	Stat    statInfo `json:"stat"` // Stat of the current object `cache.json`.

	// checksums of blocks on disk.
	Checksum CacheChecksumInfoV1 `json:"checksum,omitempty"`
	// Metadata map for current object.
	Meta map[string]string `json:"meta,omitempty"`
	// Ranges maps cached range to associated filename.
	Ranges map[string]string `json:"ranges,omitempty"`
	// Hits is a counter on the number of times this object has been accessed so far.
	Hits int `json:"hits,omitempty"`
}

// RangeInfo has the range, file and range length information for a cached range.
type RangeInfo struct {
	Range string
	File  string
	Size  int64
}

// Empty returns true if this is an empty struct
func (r *RangeInfo) Empty() bool {
	return r.Range == "" && r.File == "" && r.Size == 0
}

func (m *cacheMeta) ToObjectInfo(bucket, object string) (o ObjectInfo) {
	if len(m.Meta) == 0 {
		m.Meta = make(map[string]string)
		m.Stat.ModTime = timeSentinel
	}

	o = ObjectInfo{
		Bucket:            bucket,
		Name:              object,
		CacheStatus:       CacheHit,
		CacheLookupStatus: CacheHit,
	}

	// We set file info only if its valid.
	o.ModTime = m.Stat.ModTime
	o.Size = m.Stat.Size
	o.ETag = extractETag(m.Meta)
	o.ContentType = m.Meta["content-type"]
	o.ContentEncoding = m.Meta["content-encoding"]
	if storageClass, ok := m.Meta[xhttp.AmzStorageClass]; ok {
		o.StorageClass = storageClass
	} else {
		o.StorageClass = globalRadioDefaultStorageClass
	}
	var (
		t time.Time
		e error
	)
	if exp, ok := m.Meta["expires"]; ok {
		if t, e = time.Parse(http.TimeFormat, exp); e == nil {
			o.Expires = t.UTC()
		}
	}
	// etag/md5Sum has already been extracted. We need to
	// remove to avoid it from appearing as part of user-defined metadata
	o.UserDefined = cleanMetadata(m.Meta)
	return o
}

// represents disk cache struct
type diskCache struct {
	dir      string // caching directory
	quotaPct int    // max usage in %
	// mark false if drive is offline
	online bool
	// mutex to protect updates to online variable
	onlineMutex   *sync.RWMutex
	pool          sync.Pool
	after         int // minimum accesses before an object is cached.
	lowWatermark  int
	highWatermark int
	gcCounter     atomic.Uint64
	// nsMutex namespace lock
	nsMutex *NSLockMap
	// Object functions pointing to the corresponding functions of backend implementation.
	NewNSLockFn func(ctx context.Context, cachePath string) RWLocker
}

// Inits the disk cache dir if it is not initialized already.
func newDiskCache(dir string, quotaPct, after, lowWatermark, highWatermark int) (*diskCache, error) {
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, fmt.Errorf("Unable to initialize '%s' dir, %w", dir, err)
	}
	cache := diskCache{
		dir:           dir,
		quotaPct:      quotaPct,
		after:         after,
		lowWatermark:  lowWatermark,
		highWatermark: highWatermark,
		online:        true,
		onlineMutex:   &sync.RWMutex{},
		pool: sync.Pool{
			New: func() interface{} {
				b := disk.AlignedBlock(int(cacheBlkSize))
				return &b
			},
		},
		nsMutex: newNSLock(false),
	}
	cache.NewNSLockFn = func(ctx context.Context, cachePath string) RWLocker {
		return cache.nsMutex.NewNSLock(ctx, nil, cachePath, "")
	}
	return &cache, nil
}

// diskUsageLow() returns true if disk usage falls below the low watermark w.r.t configured cache quota.
// Ex. for a 100GB disk, if quota is configured as 70%  and watermark_low = 80% and
// watermark_high = 90% then garbage collection starts when 63% of disk is used and
// stops when disk usage drops to 56%
func (c *diskCache) diskUsageLow() bool {
	gcStopPct := c.quotaPct * c.lowWatermark / 100
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return false
	}
	usedPercent := (di.Total - di.Free) * 100 / di.Total
	return int(usedPercent) < gcStopPct
}

// Returns if the disk usage reaches high water mark w.r.t the configured cache quota.
// gc starts if high water mark reached.
func (c *diskCache) diskUsageHigh() bool {
	gcTriggerPct := c.quotaPct * c.highWatermark / 100
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return false
	}
	usedPercent := (di.Total - di.Free) * 100 / di.Total
	return int(usedPercent) >= gcTriggerPct
}

// Returns if size space can be allocated without exceeding
// max disk usable for caching
func (c *diskCache) diskAvailable(size int64) bool {
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return false
	}
	usedPercent := (di.Total - (di.Free - uint64(size))) * 100 / di.Total
	return int(usedPercent) < c.quotaPct
}

// toClear returns how many bytes should be cleared to reach the low watermark quota.
// returns 0 if below quota.
func (c *diskCache) toClear() uint64 {
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return 0
	}
	return bytesToClear(int64(di.Total), int64(di.Free), uint64(c.quotaPct), uint64(c.lowWatermark))
}

// Purge cache entries that were not accessed.
func (c *diskCache) purge(ctx context.Context) {
	if c.diskUsageLow() {
		return
	}
	toFree := c.toClear()
	if toFree == 0 {
		return
	}
	// expiry for cleaning up old cache.json files that
	// need to be cleaned up.
	expiry := UTCNow().Add(-cacheExpiryDays)
	// defaulting max hits count to 100
	scorer, err := newFileScorer(int64(toFree), time.Now().Unix(), 100)
	if err != nil {
		logger.LogIf(ctx, err)
		return
	}

	// this function returns FileInfo for cached range files and cache data file.
	fiStatFn := func(ranges map[string]string, dataFile, pathPrefix string) map[string]os.FileInfo {
		fm := make(map[string]os.FileInfo)
		fname := pathJoin(pathPrefix, dataFile)
		if fi, err := os.Stat(fname); err == nil {
			fm[fname] = fi
		}

		for _, rngFile := range ranges {
			fname = pathJoin(pathPrefix, rngFile)
			if fi, err := os.Stat(fname); err == nil {
				fm[fname] = fi
			}
		}
		return fm
	}
	objDirs, err := ioutil.ReadDir(c.dir)
	if err != nil {
		log.Fatal(err)
	}

	for _, obj := range objDirs {
		if obj.Name() == minioMetaBucket {
			continue
		}

		cacheDir := pathJoin(c.dir, obj.Name())
		meta, _, numHits, err := c.statCachedMeta(ctx, cacheDir)
		if err != nil {
			// delete any partially filled cache entry left behind.
			removeAll(cacheDir)
			continue
		}
		// stat all cached file ranges and cacheDataFile.
		cachedFiles := fiStatFn(meta.Ranges, cacheDataFile, pathJoin(c.dir, obj.Name()))
		objInfo := meta.ToObjectInfo("", "")
		cc := cacheControlOpts(objInfo)
		for fname, fi := range cachedFiles {
			if cc != nil {
				if cc.isStale(objInfo.ModTime) {
					if err = removeAll(fname); err != nil {
						logger.LogIf(ctx, err)
					}
					scorer.adjustSaveBytes(-fi.Size())
					// break early if sufficient disk space reclaimed.
					if c.diskUsageLow() {
						return
					}
				}
				continue
			}
			scorer.addFile(fname, atime.Get(fi), fi.Size(), numHits)
		}
		// clean up stale cache.json files for objects that never got cached but access count was maintained in cache.json
		fi, err := os.Stat(pathJoin(cacheDir, cacheMetaJSONFile))
		if err != nil || (fi.ModTime().Before(expiry) && len(cachedFiles) == 0) {
			removeAll(cacheDir)
			scorer.adjustSaveBytes(-fi.Size())
			continue
		}
		if c.diskUsageLow() {
			return
		}
	}
	for _, path := range scorer.fileNames() {
		removeAll(path)
		slashIdx := strings.LastIndex(path, SlashSeparator)
		pathPrefix := path[0:slashIdx]
		fname := path[slashIdx+1:]
		if fname == cacheDataFile {
			removeAll(pathPrefix)
		}
	}
}

func (c *diskCache) incGCCounter() {
	c.gcCounter.Add(uint64(1))
}
func (c *diskCache) resetGCCounter() {
	c.gcCounter.Store(uint64(0))
}
func (c *diskCache) gcCount() uint64 {
	return c.gcCounter.Load()
}

// sets cache drive status
func (c *diskCache) setOnline(status bool) {
	c.onlineMutex.Lock()
	c.online = status
	c.onlineMutex.Unlock()
}

// returns true if cache drive is online
func (c *diskCache) IsOnline() bool {
	c.onlineMutex.RLock()
	defer c.onlineMutex.RUnlock()
	return c.online
}

// Stat returns ObjectInfo from disk cache
func (c *diskCache) Stat(ctx context.Context, bucket, object string) (oi ObjectInfo, numHits int, err error) {
	var partial bool
	var meta *cacheMeta

	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	// Stat the file to get file size.
	meta, partial, numHits, err = c.statCachedMeta(ctx, cacheObjPath)
	if err != nil {
		return
	}
	if partial {
		return oi, numHits, errFileNotFound
	}
	oi = meta.ToObjectInfo("", "")
	oi.Bucket = bucket
	oi.Name = object
	return
}

// statCachedMeta returns metadata from cache - including ranges cached, partial to indicate
// if partial object is cached.
func (c *diskCache) statCachedMeta(ctx context.Context, cacheObjPath string) (meta *cacheMeta, partial bool, numHits int, err error) {

	cLock := c.NewNSLockFn(ctx, cacheObjPath)
	if err = cLock.GetRLock(globalObjectTimeout); err != nil {
		return
	}

	defer cLock.RUnlock()
	return c.statCache(ctx, cacheObjPath)
}

// statRange returns ObjectInfo and RangeInfo from disk cache
func (c *diskCache) statRange(ctx context.Context, bucket, object string, rs *HTTPRangeSpec) (oi ObjectInfo, rngInfo RangeInfo, numHits int, err error) {
	// Stat the file to get file size.
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	var meta *cacheMeta
	var partial bool

	meta, partial, numHits, err = c.statCachedMeta(ctx, cacheObjPath)
	if err != nil {
		return
	}

	oi = meta.ToObjectInfo("", "")
	oi.Bucket = bucket
	oi.Name = object
	if !partial {
		return
	}

	actualSize := uint64(meta.Stat.Size)
	var length int64
	_, length, err = rs.GetOffsetLength(int64(actualSize))
	if err != nil {
		return
	}

	actualRngSize := uint64(length)

	rng := rs.String(int64(actualSize))
	rngFile, ok := meta.Ranges[rng]
	if !ok {
		return oi, rngInfo, numHits, ObjectNotFound{Bucket: bucket, Object: object}
	}
	if _, err = os.Stat(pathJoin(cacheObjPath, rngFile)); err != nil {
		return oi, rngInfo, numHits, ObjectNotFound{Bucket: bucket, Object: object}
	}
	rngInfo = RangeInfo{Range: rng, File: rngFile, Size: int64(actualRngSize)}

	return
}

// statCache is a convenience function for purge() to get ObjectInfo for cached object
func (c *diskCache) statCache(ctx context.Context, cacheObjPath string) (meta *cacheMeta, partial bool, numHits int, err error) {
	// Stat the file to get file size.
	metaPath := pathJoin(cacheObjPath, cacheMetaJSONFile)
	f, err := os.Open(metaPath)
	if err != nil {
		return meta, partial, 0, err
	}
	defer f.Close()
	meta = &cacheMeta{Version: cacheMetaVersion}
	if err := jsonLoad(f, meta); err != nil {
		return meta, partial, 0, err
	}
	// get metadata of part.1 if full file has been cached.
	partial = true
	fi, err := os.Stat(pathJoin(cacheObjPath, cacheDataFile))
	if err == nil {
		meta.Stat.ModTime = atime.Get(fi)
		partial = false
	}
	return meta, partial, meta.Hits, nil
}

// saves object metadata to disk cache
// incHitsOnly is true if metadata update is incrementing only the hit counter
func (c *diskCache) SaveMetadata(ctx context.Context, bucket, object string, meta map[string]string, actualSize int64, rs *HTTPRangeSpec, rsFileName string, incHitsOnly bool) error {
	cachedPath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(ctx, cachedPath)
	if err := cLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer cLock.Unlock()
	return c.saveMetadata(ctx, bucket, object, meta, actualSize, rs, rsFileName, incHitsOnly)
}

// magic HH-256 key as HH-256 hash of the first 100 decimals of π as utf-8 string with a zero key.
var magicHighwayHash256Key = []byte("\x4b\xe7\x34\xfa\x8e\x23\x8a\xcd\x26\x3e\x83\xe6\xbb\x96\x85\x52\x04\x0f\x93\x5d\xa3\x9f\x44\x14\x97\xe0\x9d\x13\x22\xde\x36\xa0")

// BitrotAlgorithm specifies a algorithm used for bitrot protection.
type BitrotAlgorithm uint

const (
	// SHA256 represents the SHA-256 hash function
	SHA256 BitrotAlgorithm = 1 + iota
	// HighwayHash256 represents the HighwayHash-256 hash function
	HighwayHash256
	// HighwayHash256S represents the Streaming HighwayHash-256 hash function
	HighwayHash256S
	// BLAKE2b512 represents the BLAKE2b-512 hash function
	BLAKE2b512
)

// DefaultBitrotAlgorithm is the default algorithm used for bitrot protection.
const (
	DefaultBitrotAlgorithm = HighwayHash256S
)

var bitrotAlgorithms = map[BitrotAlgorithm]string{
	SHA256:          "sha256",
	BLAKE2b512:      "blake2b",
	HighwayHash256:  "highwayhash256",
	HighwayHash256S: "highwayhash256S",
}

// New returns a new hash.Hash calculating the given bitrot algorithm.
func (a BitrotAlgorithm) New() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New()
	case BLAKE2b512:
		b2, _ := blake2b.New512(nil) // New512 never returns an error if the key is nil
		return b2
	case HighwayHash256:
		hh, _ := highwayhash.New(magicHighwayHash256Key) // New will never return error since key is 256 bit
		return hh
	case HighwayHash256S:
		hh, _ := highwayhash.New(magicHighwayHash256Key) // New will never return error since key is 256 bit
		return hh
	default:
		logger.CriticalIf(GlobalContext, errors.New("Unsupported bitrot algorithm"))
		return nil
	}
}

// Available reports whether the given algorithm is available.
func (a BitrotAlgorithm) Available() bool {
	_, ok := bitrotAlgorithms[a]
	return ok
}

// String returns the string identifier for a given bitrot algorithm.
// If the algorithm is not supported String panics.
func (a BitrotAlgorithm) String() string {
	name, ok := bitrotAlgorithms[a]
	if !ok {
		logger.CriticalIf(GlobalContext, errors.New("Unsupported bitrot algorithm"))
	}
	return name
}

// saves object metadata to disk cache
// incHitsOnly is true if metadata update is incrementing only the hit counter
func (c *diskCache) saveMetadata(ctx context.Context, bucket, object string, meta map[string]string, actualSize int64, rs *HTTPRangeSpec, rsFileName string, incHitsOnly bool) error {
	cachedPath := getCacheSHADir(c.dir, bucket, object)
	metaPath := pathJoin(cachedPath, cacheMetaJSONFile)
	// Create cache directory if needed
	if err := os.MkdirAll(cachedPath, 0777); err != nil {
		return err
	}
	f, err := os.OpenFile(metaPath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	m := &cacheMeta{Version: cacheMetaVersion}
	if err := jsonLoad(f, m); err != nil && err != io.EOF {
		return err
	}
	// increment hits
	if rs != nil {
		if m.Ranges == nil {
			m.Ranges = make(map[string]string)
		}
		m.Ranges[rs.String(actualSize)] = rsFileName
	} else {
		// this is necessary cleanup of range files if entire object is cached.
		for _, f := range m.Ranges {
			removeAll(pathJoin(cachedPath, f))
		}
		m.Ranges = nil
	}
	m.Stat.Size = actualSize
	m.Stat.ModTime = UTCNow()
	if !incHitsOnly {
		// reset meta
		m.Meta = meta
	} else {
		if m.Meta == nil {
			m.Meta = make(map[string]string)
		}
		if etag, ok := meta["etag"]; ok {
			m.Meta["etag"] = etag
		}
	}

	m.Hits++

	m.Checksum = CacheChecksumInfoV1{Algorithm: HighwayHash256S.String(), Blocksize: cacheBlkSize}
	return jsonSave(f, m)
}

func getCacheSHADir(dir, bucket, object string) string {
	return pathJoin(dir, getSHA256Hash([]byte(pathJoin(bucket, object))))
}

// Cache data to disk with bitrot checksum added for each block of 1MB
func (c *diskCache) bitrotWriteToCache(cachePath, fileName string, reader io.Reader, size uint64) (int64, error) {
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return 0, err
	}
	filePath := pathJoin(cachePath, fileName)

	if filePath == "" || reader == nil {
		return 0, errInvalidArgument
	}

	if err := checkPathLength(filePath); err != nil {
		return 0, err
	}
	f, err := os.Create(filePath)
	if err != nil {
		return 0, osErrToFSFileErr(err)
	}
	defer f.Close()

	var bytesWritten int64

	h := HighwayHash256S.New()

	bufp := c.pool.Get().(*[]byte)
	defer c.pool.Put(bufp)

	var n, n2 int
	for {
		n, err = io.ReadFull(reader, *bufp)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, err
		}
		eof := err == io.EOF || err == io.ErrUnexpectedEOF
		if n == 0 && size != 0 {
			// Reached EOF, nothing more to be done.
			break
		}
		h.Reset()
		if _, err = h.Write((*bufp)[:n]); err != nil {
			return 0, err
		}
		hashBytes := h.Sum(nil)
		if _, err = f.Write(hashBytes); err != nil {
			return 0, err
		}
		if n2, err = f.Write((*bufp)[:n]); err != nil {
			return 0, err
		}
		bytesWritten += int64(n2)
		if eof {
			break
		}
	}
	return bytesWritten, nil
}

// Caches the object to disk
func (c *diskCache) Put(ctx context.Context, bucket, object string, data io.Reader, size int64, rs *HTTPRangeSpec, opts ObjectOptions, incHitsOnly bool) error {
	if c.diskUsageHigh() {
		c.incGCCounter()
		io.Copy(ioutil.Discard, data)
		return errDiskFull
	}
	cachePath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(ctx, cachePath)
	if err := cLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer cLock.Unlock()

	meta, _, numHits, err := c.statCache(ctx, cachePath)
	// Case where object not yet cached
	if os.IsNotExist(err) && c.after >= 1 {
		return c.saveMetadata(ctx, bucket, object, opts.UserDefined, size, nil, "", false)
	}
	// Case where object already has a cache metadata entry but not yet cached
	if err == nil && numHits < c.after {
		cETag := extractETag(meta.Meta)
		bETag := extractETag(opts.UserDefined)
		if cETag == bETag {
			return c.saveMetadata(ctx, bucket, object, opts.UserDefined, size, nil, "", false)
		}
		incHitsOnly = true
	}

	if rs != nil {
		return c.putRange(ctx, bucket, object, data, size, rs, opts)
	}
	if !c.diskAvailable(size) {
		return errDiskFull
	}
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return err
	}
	var metadata = make(map[string]string)
	for k, v := range opts.UserDefined {
		metadata[k] = v
	}
	var reader = data
	var actualSize = uint64(size)
	n, err := c.bitrotWriteToCache(cachePath, cacheDataFile, reader, actualSize)
	if IsErr(err, baseErrs...) {
		c.setOnline(false)
	}
	if err != nil {
		removeAll(cachePath)
		return err
	}

	if actualSize != uint64(n) {
		removeAll(cachePath)
		return IncompleteBody{}
	}
	return c.saveMetadata(ctx, bucket, object, metadata, n, nil, "", incHitsOnly)
}

// Caches the range to disk
func (c *diskCache) putRange(ctx context.Context, bucket, object string, data io.Reader, size int64, rs *HTTPRangeSpec, opts ObjectOptions) error {
	rlen, err := rs.GetLength(size)
	if err != nil {
		return err
	}
	if !c.diskAvailable(rlen) {
		return errDiskFull
	}
	cachePath := getCacheSHADir(c.dir, bucket, object)
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return err
	}
	var metadata = make(map[string]string)
	for k, v := range opts.UserDefined {
		metadata[k] = v
	}
	var reader = data
	var actualSize = uint64(rlen)
	// objSize is the actual size of object (with encryption overhead if any)
	var objSize = uint64(size)
	cacheFile := mustGetUUID()
	n, err := c.bitrotWriteToCache(cachePath, cacheFile, reader, actualSize)
	if IsErr(err, baseErrs...) {
		c.setOnline(false)
	}
	if err != nil {
		removeAll(cachePath)
		return err
	}
	if actualSize != uint64(n) {
		removeAll(cachePath)
		return IncompleteBody{}
	}
	return c.saveMetadata(ctx, bucket, object, metadata, int64(objSize), rs, cacheFile, false)
}

// checks streaming bitrot checksum of cached object before returning data
func (c *diskCache) bitrotReadFromCache(ctx context.Context, filePath string, offset, length int64, writer io.Writer) error {
	h := HighwayHash256S.New()

	checksumHash := make([]byte, h.Size())

	startBlock := offset / cacheBlkSize
	endBlock := (offset + length) / cacheBlkSize

	// get block start offset
	var blockStartOffset int64
	if startBlock > 0 {
		blockStartOffset = (cacheBlkSize + int64(h.Size())) * startBlock
	}

	tillLength := (cacheBlkSize + int64(h.Size())) * (endBlock - startBlock + 1)

	// Start offset cannot be negative.
	if offset < 0 {
		logger.LogIf(ctx, errUnexpected)
		return errUnexpected
	}

	// Writer cannot be nil.
	if writer == nil {
		logger.LogIf(ctx, errUnexpected)
		return errUnexpected
	}
	var blockOffset, blockLength int64
	rc, err := readCacheFileStream(filePath, blockStartOffset, tillLength)
	if err != nil {
		return err
	}
	bufp := c.pool.Get().(*[]byte)
	defer c.pool.Put(bufp)

	for block := startBlock; block <= endBlock; block++ {
		switch {
		case startBlock == endBlock:
			blockOffset = offset % cacheBlkSize
			blockLength = length
		case block == startBlock:
			blockOffset = offset % cacheBlkSize
			blockLength = cacheBlkSize - blockOffset
		case block == endBlock:
			blockOffset = 0
			blockLength = (offset + length) % cacheBlkSize
		default:
			blockOffset = 0
			blockLength = cacheBlkSize
		}
		if blockLength == 0 {
			break
		}
		if _, err := io.ReadFull(rc, checksumHash); err != nil {
			return err
		}
		h.Reset()
		n, err := io.ReadFull(rc, *bufp)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			logger.LogIf(ctx, err)
			return err
		}
		eof := err == io.EOF || err == io.ErrUnexpectedEOF
		if n == 0 && length != 0 {
			// Reached EOF, nothing more to be done.
			break
		}

		if _, e := h.Write((*bufp)[:n]); e != nil {
			return e
		}
		hashBytes := h.Sum(nil)

		if !bytes.Equal(hashBytes, checksumHash) {
			err = fmt.Errorf("hashes do not match expected %s, got %s",
				hex.EncodeToString(checksumHash), hex.EncodeToString(hashBytes))
			logger.LogIf(GlobalContext, err)
			return err
		}

		if _, err := io.Copy(writer, bytes.NewReader((*bufp)[blockOffset:blockOffset+blockLength])); err != nil {
			if err != io.ErrClosedPipe {
				logger.LogIf(ctx, err)
				return err
			}
			eof = true
		}
		if eof {
			break
		}
	}

	return nil
}

// Get returns ObjectInfo and reader for object from disk cache
func (c *diskCache) Get(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, opts ObjectOptions) (gr *GetObjectReader, numHits int, err error) {
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(ctx, cacheObjPath)
	if err := cLock.GetRLock(globalObjectTimeout); err != nil {
		return nil, numHits, err
	}

	defer cLock.RUnlock()
	var objInfo ObjectInfo
	var rngInfo RangeInfo
	if objInfo, rngInfo, numHits, err = c.statRange(ctx, bucket, object, rs); err != nil {
		return nil, numHits, toObjectErr(err, bucket, object)
	}
	cacheFile := cacheDataFile
	objSize := objInfo.Size
	if !rngInfo.Empty() {
		// for cached ranges, need to pass actual range file size to GetObjectReader
		// and clear out range spec
		cacheFile = rngInfo.File
		objInfo.Size = rngInfo.Size
		rs = nil
	}
	var nsUnlocker = func() {}
	// For a directory, we need to send an reader that returns no bytes.
	if HasSuffix(object, SlashSeparator) {
		// The lock taken above is released when
		// objReader.Close() is called by the caller.
		gr, gerr := NewGetObjectReaderFromReader(bytes.NewBuffer(nil), objInfo, opts, nsUnlocker)
		return gr, numHits, gerr
	}

	fn, off, length, nErr := NewGetObjectReader(rs, objInfo, opts, nsUnlocker)
	if nErr != nil {
		return nil, numHits, nErr
	}
	filePath := pathJoin(cacheObjPath, cacheFile)
	pr, pw := io.Pipe()
	go func() {
		err := c.bitrotReadFromCache(ctx, filePath, off, length, pw)
		if err != nil {
			removeAll(cacheObjPath)
		}
		pw.CloseWithError(err)
	}()
	// Cleanup function to cause the go routine above to exit, in
	// case of incomplete read.
	pipeCloser := func() { pr.Close() }

	gr, gerr := fn(pr, h, opts.CheckCopyPrecondFn, pipeCloser)
	if gerr != nil {
		return gr, numHits, gerr
	}
	if !rngInfo.Empty() {
		// overlay Size with actual object size and not the range size
		gr.ObjInfo.Size = objSize
	}
	return gr, numHits, nil

}

// Deletes the cached object
func (c *diskCache) delete(ctx context.Context, cacheObjPath string) (err error) {
	cLock := c.NewNSLockFn(ctx, cacheObjPath)
	if err := cLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer cLock.Unlock()
	return removeAll(cacheObjPath)
}

// Deletes the cached object
func (c *diskCache) Delete(ctx context.Context, bucket, object string) (err error) {
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	return c.delete(ctx, cacheObjPath)
}

// convenience function to check if object is cached on this diskCache
func (c *diskCache) Exists(ctx context.Context, bucket, object string) bool {
	if _, err := os.Stat(getCacheSHADir(c.dir, bucket, object)); err != nil {
		return false
	}
	return true
}
