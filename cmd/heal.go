package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v6"
	miniogo "github.com/minio/minio-go/v6"
	"github.com/minio/minio-go/v6/pkg/encrypt"
	"github.com/minio/minio/pkg/sync/errgroup"
	"github.com/minio/radio/cmd/logger"
)

const radioErrLogName = "log.json"

type operation string

var (
	opPutObject         operation = "PutObject"
	opDeleteObject      operation = "DeleteObject"
	opCopyObject        operation = "CopyObject"
	opCompleteMultipart operation = "CompleteMultipart"
)

// journalEntry struct for logging radio errors to local journal
type journalEntry struct {
	Timestamp            time.Time          `json:"timestamp"`
	ReplicaBucket        string             `json:"replicabucket"`
	Bucket               string             `json:"bucket"`
	Object               string             `json:"object"`
	ErrClientID          string             `json:"err_client_id"`
	SrcClientID          string             `json:"src_client_id"`
	Op                   operation          `json:"operation"`
	ETag                 string             `json:"etag"`
	DstBucket            string             `json:"dst_bucket"`
	DstObject            string             `json:"dst_object"`
	RadioTagID           string             `json:"radio_tag_id"`
	UserMeta             map[string]string  `json:"user_meta"`
	ServerSideEncryption encrypt.ServerSide `json:"sse"`
}

// HealSys struct represents the config for healing system
type HealSys struct {
	ctx         context.Context
	Dir         string
	errorCh     chan journalEntry
	healCh      chan journalEntry
	nsMutex     *NSLockMap
	NewNSLockFn func(ctx context.Context, logPath string) RWLocker
}

// newHealSys initializes healing
func newHealSys(ctx context.Context, dir string) *HealSys {
	sys := &HealSys{ctx: ctx,
		Dir:     dir,
		errorCh: make(chan journalEntry, 10000),
		healCh:  make(chan journalEntry, 10000),
		nsMutex: newNSLock(false),
	}
	sys.NewNSLockFn = func(ctx context.Context, logPath string) RWLocker {
		return sys.nsMutex.NewNSLock(ctx, nil, logPath, "")
	}

	go sys.heal(ctx)
	return sys
}

func (sys *HealSys) init() {
	if sys == nil {
		return
	}
	if err := os.MkdirAll(sys.Dir, 0777); err != nil {
		logger.FatalIf(err, fmt.Sprintf("Unable to initialize '%s' dir", sys.Dir))
	}
	select {
	case <-time.After(100 * time.Millisecond):
		err := filepath.Walk(sys.Dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() || strings.HasSuffix(sys.Dir, info.Name()) {
				return nil
			}
			return sys.process(sys.ctx, info.Name())
		})
		if err != nil {
			logger.LogIf(sys.ctx, err)
		}
		go sys.startJournal(sys.ctx)
	case <-sys.ctx.Done():
	}
}

// process sends error logs to the heal channel for an attempt to heal after
// server bootup.
func (sys *HealSys) process(ctx context.Context, jdir string) error {
	if sys == nil {
		return errServerNotInitialized
	}
	fpath := pathJoin(sys.Dir, jdir)
	entry, err := sys.readJournalEntry(ctx, fpath)
	if err != nil {
		return err
	}
	sys.healCh <- entry
	return nil
}

// send error log to error channel
func (sys *HealSys) send(ctx context.Context, entry journalEntry) {
	if sys == nil {
		return
	}
	select {
	case sys.errorCh <- entry:
		globalNotificationSys.PutJournalRec(ctx, entry)
		return
	case <-ctx.Done():
		return
	}
}

func (sys *HealSys) getJournalDir(ReplicaBucket, bucket, object string) string {
	if sys == nil {
		return ""
	}
	return pathJoin(sys.Dir, getSHA256Hash([]byte(pathJoin(ReplicaBucket, bucket, object))))
}
func (sys *HealSys) saveJournalEntry(ctx context.Context, jlog journalEntry) error {
	if sys == nil {
		return errServerNotInitialized
	}
	jdir := sys.getJournalDir(jlog.ReplicaBucket, jlog.Bucket, jlog.Object)
	lock := sys.NewNSLockFn(sys.ctx, jdir)
	if err := lock.GetLock(globalObjectTimeout); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	defer lock.Unlock()

	bytes, err := json.Marshal(jlog)
	if err != nil {
		logger.LogIf(ctx, err)
		return err

	}
	if err := os.MkdirAll(jdir, 0777); err != nil {
		logger.LogIf(ctx, err)
		return err

	}
	fptr, err := os.OpenFile(pathJoin(jdir, radioErrLogName), os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		logger.LogIf(ctx, err)
		return err
	}

	if _, err = fptr.Write(bytes); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	fptr.WriteString("\n")
	return fptr.Close()
}

func (sys *HealSys) removeJournalEntry(ctx context.Context, journalDir string) error {
	if sys == nil {
		return errServerNotInitialized
	}
	lock := sys.NewNSLockFn(sys.ctx, journalDir)
	if err := lock.GetLock(globalObjectTimeout); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	defer lock.Unlock()

	return removeAll(pathJoin(sys.Dir, journalDir))
}

func (sys *HealSys) readJournalEntry(ctx context.Context, jdir string) (jlog journalEntry, err error) {
	if sys == nil {
		return jlog, errServerNotInitialized
	}
	lock := sys.NewNSLockFn(sys.ctx, jdir)
	if jerr := lock.GetRLock(globalObjectTimeout); jerr != nil {
		logger.LogIf(ctx, jerr)
		return
	}
	defer lock.RUnlock()

	var file *os.File
	file, err = os.Open(pathJoin(jdir, radioErrLogName))
	if err != nil {
		if os.IsNotExist(err) {
			return jlog, errFileNotFound
		}
		return
	}
	defer file.Close()
	bio := bufio.NewScanner(file)
	for bio.Scan() {
		var errLog journalEntry
		if err = json.Unmarshal(bio.Bytes(), &errLog); err != nil {
			continue
		}
		return errLog, nil
	}
	if err = bio.Err(); err != nil {
		return
	}
	return
}

// startJournal logs radio API call errors to a temporary file for further processing
// by healing routine.
func (sys *HealSys) startJournal(ctx context.Context) {
	for {
		select {
		case log, ok := <-sys.errorCh:
			if !ok {
				continue
			}
			if err := sys.saveJournalEntry(ctx, log); err != nil {
				logger.LogIf(ctx, err)
				continue
			}
			sys.healCh <- log
		case <-ctx.Done():
			return
		}
	}
}

// heal() retries failed mutable API operations and attempts to
// get the object in sync between mirrored clients.
func (sys *HealSys) heal(ctx context.Context) {
	for {
		select {
		case log, ok := <-sys.healCh:
			if !ok {
				continue
			}
			journalDir := sys.getJournalDir(log.ReplicaBucket, log.Bucket, log.Object)
			lock := sys.NewNSLockFn(ctx, journalDir)
			if err := lock.GetLock(globalObjectTimeout); err != nil {
				logger.LogIf(ctx, err)
				continue
			}
			var err error
			switch log.Op {
			case opPutObject:
				err = globalObjectAPI.HealPutObject(ctx, log)
			case opCompleteMultipart:
				err = globalObjectAPI.HealPutObject(ctx, log)
			case opDeleteObject:
				err = globalObjectAPI.HealDeleteObject(ctx, log)
			case opCopyObject:
				err = globalObjectAPI.HealCopyObject(ctx, log)
			}
			if err != nil {
				if err == errRadioSiteOffline || IsNetworkOrHostDown(err) {
					go func(l journalEntry) {
						time.Sleep(30 * time.Second)
						sys.healCh <- l
					}(log)
					lock.Unlock()
					continue
				}
			}
			removeAll(journalDir)
			globalNotificationSys.RemoveJournalRec(ctx, journalDir)

			lock.Unlock()
			continue
		case <-ctx.Done():
			return
		}
	}
}

var (
	errRadioConfigNotFound = errors.New("invalid radio config")
	errRadioSiteNotFound   = errors.New("remote not found")
	errRadioSiteOffline    = errors.New("remote offline")
	errRadioTagMismatch    = errors.New("radio tag IDs do not match")
)

func (l *radioObjects) getReplicaObjectInfo(ctx context.Context, bucket string, object string, opts ObjectOptions, rIndex int) (objInfo ObjectInfo, err error) {
	rs3s, ok := l.mirrorClients[bucket]
	if !ok {
		return ObjectInfo{}, BucketNotFound{
			Bucket: bucket,
		}
	}
	if rIndex < 0 || rIndex >= len(rs3s.clnts) {
		return ObjectInfo{}, fmt.Errorf("invalid replica index")
	}

	oinfo, err := rs3s.clnts[rIndex].StatObjectWithContext(
		ctx,
		rs3s.clnts[rIndex].Bucket, object,
		miniogo.StatObjectOptions{
			GetObjectOptions: miniogo.GetObjectOptions{
				ServerSideEncryption: opts.ServerSideEncryption,
			},
		})
	if err != nil {
		return ObjectInfo{}, ErrorRespToObjectError(err, bucket, object)
	}
	return FromMinioClientObjectInfo(bucket, oinfo, rIndex), nil
}

// HealPutObject tries to sync client that failed PutObject operation with
// other mirror clients that completed successfully. If client is still down,
// resend error log back to error channel for reprocessing.
func (l *radioObjects) HealPutObject(ctx context.Context, log journalEntry) error {
	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, log.Bucket, log.Object)
	if err := objectLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer objectLock.Unlock()

	rs3s, ok := l.mirrorClients[log.Bucket]
	if !ok {
		return errRadioConfigNotFound
	}
	errIdx := -1
	srcIdx := -1
	for i, clnt := range rs3s.clnts {
		if clnt.ID == log.ErrClientID {
			errIdx = i
		}
		if clnt.ID == log.SrcClientID {
			srcIdx = i
		}
	}
	if errIdx == -1 || srcIdx == -1 {
		return errRadioSiteNotFound
	}
	// if remote client to heal is still down or source down, resend the log to error channel.
	if rs3s.clnts[errIdx].isOffline() || rs3s.clnts[srcIdx].isOffline() {
		return errRadioSiteOffline
	}
	// check if object already healed by peer
	oi, err := l.getReplicaObjectInfo(ctx, log.Bucket, log.Object, ObjectOptions{}, errIdx)
	if err == nil {
		radioTagID, ok := oi.UserDefined["X-Amz-Meta-Radio-Tag"]
		if ok && radioTagID == log.RadioTagID {
			return nil
		}
	}
	oi, err = l.getReplicaObjectInfo(ctx, log.Bucket, log.Object, ObjectOptions{}, srcIdx)
	if err != nil {
		return err
	}
	// Ensure Radio-Tag is identical on replica source
	radioTagID, ok := oi.UserDefined["X-Amz-Meta-Radio-Tag"]
	if !ok || radioTagID != log.RadioTagID {
		return errRadioTagMismatch
	}
	g := errgroup.WithNErrs(1)
	g.Go(func() error {
		reader, objInfo, _, err := rs3s.clnts[srcIdx].GetObjectWithContext(
			ctx,
			rs3s.clnts[srcIdx].Bucket,
			log.Object, minio.GetObjectOptions{})
		if err == nil {
			_, err = rs3s.clnts[errIdx].PutObjectWithContext(ctx, rs3s.clnts[errIdx].Bucket, log.Object, reader, objInfo.Size, "", "", log.UserMeta, log.ServerSideEncryption)
			if err != nil {
				logger.LogIf(ctx, err, " could not heal ", log, " err ")
			}
		}
		return err
	}, 0)
	errs := g.Wait()
	return errs[0]
}

// HealDeleteObject tries to sync client that failed DeleteObject operation with
// other mirror clients that completed successfully. If client is still down,
// resend error log back to error channel for reprocessing.
func (l *radioObjects) HealDeleteObject(ctx context.Context, log journalEntry) error {
	// Lock the object before reading.
	objectLock := l.NewNSLock(ctx, log.Bucket, log.Object)
	if err := objectLock.GetLock(globalObjectTimeout); err != nil {
		return err
	}
	defer objectLock.Unlock()

	rs3s, ok := l.mirrorClients[log.Bucket]
	if !ok {
		return errRadioConfigNotFound
	}
	errIdx := -1

	for i, clnt := range rs3s.clnts {
		if clnt.ID == log.ErrClientID {
			errIdx = i
		}
	}
	if errIdx == -1 {
		return errRadioSiteNotFound
	}
	// if remote client is still down, resend the log to error channel.
	if rs3s.clnts[errIdx].isOffline() {
		return errRadioSiteOffline
	}
	// ensure object has been removed from other remote clients before deleting from errIdx
	if _, err := l.getObjectInfo(ctx, log.Bucket, log.Object, ObjectOptions{}); err != nil {
		return rs3s.clnts[errIdx].RemoveObject(log.Bucket, log.Object)
	}
	return nil
}

// HealCopyObject tries to sync client that failed CopyObject operation with
// other mirror clients that completed successfully. If client is still down,
// resend error log back to error channel for reprocessing.
func (l *radioObjects) HealCopyObject(ctx context.Context, log journalEntry) error {
	// Lock the object before reading.
	cpSrcDstSame := isStringEqual(pathJoin(log.Bucket, log.Object), pathJoin(log.DstBucket, log.DstObject))
	if !cpSrcDstSame {
		objectLock := l.NewNSLock(ctx, log.DstBucket, log.DstObject)
		if err := objectLock.GetLock(globalObjectTimeout); err != nil {
			return err
		}
		defer objectLock.Unlock()
	}

	rs3sSrc := l.mirrorClients[log.Bucket]
	rs3sDest := l.mirrorClients[log.DstBucket]
	if len(rs3sSrc.clnts) != len(rs3sDest.clnts) {
		return errRadioConfigNotFound
	}

	srcIdx := -1
	dstIdx := -1

	for i, clnt := range rs3sSrc.clnts {
		if clnt.ID == log.ErrClientID {
			srcIdx = i
		}
	}

	for i, clnt := range rs3sDest.clnts {
		if clnt.ID == log.ErrClientID {
			dstIdx = i
		}
	}
	if srcIdx == -1 || dstIdx == -1 {
		return errRadioSiteNotFound
	}
	// if src or dst remote clients is down, resend the log to error channel
	if rs3sSrc.clnts[srcIdx].isOffline() || rs3sDest.clnts[dstIdx].isOffline() {
		return errRadioSiteOffline
	}
	// sanity check that dest object exists and has identical radio-tag
	oi, err := l.getObjectInfo(ctx, log.DstBucket, log.DstObject, ObjectOptions{})
	if err != nil {
		return err
	}
	radioTagID, ok := oi.UserDefined["X-Amz-Meta-Radio-Tag"]
	if !ok || radioTagID != log.RadioTagID {
		return errRadioTagMismatch
	}

	if _, err = rs3sSrc.clnts[srcIdx].CopyObjectWithContext(
		ctx,
		rs3sSrc.clnts[srcIdx].Bucket, log.Object,
		rs3sDest.clnts[dstIdx].Bucket, log.DstObject, oi.UserDefined); err != nil {
		return err
	}
	return nil
}
