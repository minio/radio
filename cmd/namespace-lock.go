package cmd

import (
	"context"
	"errors"
	pathutil "path"
	"runtime"
	"strings"
	"sync"

	"fmt"
	"time"

	"github.com/minio/lsync"
	"github.com/minio/minio/pkg/dsync"
	"github.com/minio/radio/cmd/logger"
)

// local lock servers
var globalLockServers = make(map[Endpoint]*localLocker)

// RWLocker - locker interface to introduce GetRLock, RUnlock.
type RWLocker interface {
	GetLock(timeout *dynamicTimeout) (timedOutErr error)
	Unlock()
	GetRLock(timeout *dynamicTimeout) (timedOutErr error)
	RUnlock()
}

// newNSLock - return a new name space lock map.
func newNSLock(isDistributed bool) *NSLockMap {
	nsMutex := NSLockMap{
		isDistributed: isDistributed,
	}
	if isDistributed {
		return &nsMutex
	}
	nsMutex.lockMap = make(map[nsParam]*nsLock)
	return &nsMutex
}

// nsParam - carries name space resource.
type nsParam struct {
	volume string
	path   string
}

// nsLock - provides primitives for locking critical namespace regions.
type nsLock struct {
	*lsync.LRWMutex
	ref uint
}

// NSLockMap - namespace lock map, provides primitives to Lock,
// Unlock, RLock and RUnlock.
type NSLockMap struct {
	// Indicates if namespace is part of a distributed setup.
	isDistributed bool
	lockMap       map[nsParam]*nsLock
	lockMapMutex  sync.RWMutex
}

// Lock the namespace resource.
func (n *NSLockMap) lock(ctx context.Context, volume, path string, lockSource, opsID string, readLock bool, timeout time.Duration) (locked bool) {
	var nsLk *nsLock

	n.lockMapMutex.Lock()
	param := nsParam{volume, path}
	nsLk, found := n.lockMap[param]
	if !found {
		n.lockMap[param] = &nsLock{
			LRWMutex: lsync.NewLRWMutex(ctx),
			ref:      1,
		}
		nsLk = n.lockMap[param]
	} else {
		// Update ref count here to avoid multiple races.
		nsLk.ref++
	}
	n.lockMapMutex.Unlock()

	// Locking here will block (until timeout).
	if readLock {
		locked = nsLk.GetRLock(opsID, lockSource, timeout)
	} else {
		locked = nsLk.GetLock(opsID, lockSource, timeout)
	}

	if !locked { // We failed to get the lock

		// Decrement ref count since we failed to get the lock
		n.lockMapMutex.Lock()
		nsLk.ref--
		if nsLk.ref == 0 {
			// Remove from the map if there are no more references.
			delete(n.lockMap, param)
		}
		n.lockMapMutex.Unlock()
	}
	return
}

// Unlock the namespace resource.
func (n *NSLockMap) unlock(volume, path string, readLock bool) {
	param := nsParam{volume, path}
	n.lockMapMutex.RLock()
	nsLk, found := n.lockMap[param]
	n.lockMapMutex.RUnlock()
	if !found {
		return
	}
	if readLock {
		nsLk.RUnlock()
	} else {
		nsLk.Unlock()
	}
	n.lockMapMutex.Lock()
	if nsLk.ref == 0 {
		logger.LogIf(context.Background(), errors.New("Namespace reference count cannot be 0"))
	} else {
		nsLk.ref--
		if nsLk.ref == 0 {
			// Remove from the map if there are no more references.
			delete(n.lockMap, param)
		}
	}
	n.lockMapMutex.Unlock()
}

// Lock - locks the given resource for writes, using a previously
// allocated name space lock or initializing a new one.
func (n *NSLockMap) Lock(volume, path, opsID string, timeout time.Duration) (locked bool) {
	readLock := false // This is a write lock.

	lockSource := getSource() // Useful for debugging
	return n.lock(context.Background(), volume, path, lockSource, opsID, readLock, timeout)
}

// Unlock - unlocks any previously acquired write locks.
func (n *NSLockMap) Unlock(volume, path, opsID string) {
	readLock := false
	n.unlock(volume, path, readLock)
}

// RLock - locks any previously acquired read locks.
func (n *NSLockMap) RLock(volume, path, opsID string, timeout time.Duration) (locked bool) {
	readLock := true

	lockSource := getSource() // Useful for debugging
	return n.lock(context.Background(), volume, path, lockSource, opsID, readLock, timeout)
}

// RUnlock - unlocks any previously acquired read locks.
func (n *NSLockMap) RUnlock(volume, path, opsID string) {
	readLock := true
	n.unlock(volume, path, readLock)
}

// dsync's distributed lock instance.
type distLockInstance struct {
	rwMutex             *dsync.DRWMutex
	volume, path, opsID string
}

// Lock - block until write lock is taken or timeout has occurred.
func (di *distLockInstance) GetLock(timeout *dynamicTimeout) (timedOutErr error) {
	lockSource := getSource()
	start := UTCNow()

	if !di.rwMutex.GetLock(di.opsID, lockSource, timeout.Timeout()) {
		timeout.LogFailure()
		return OperationTimedOut{Path: di.path}
	}
	timeout.LogSuccess(UTCNow().Sub(start))
	return nil
}

// Unlock - block until write lock is released.
func (di *distLockInstance) Unlock() {
	di.rwMutex.Unlock()
}

// RLock - block until read lock is taken or timeout has occurred.
func (di *distLockInstance) GetRLock(timeout *dynamicTimeout) (timedOutErr error) {
	lockSource := getSource()
	start := UTCNow()
	if !di.rwMutex.GetRLock(di.opsID, lockSource, timeout.Timeout()) {
		timeout.LogFailure()
		return OperationTimedOut{Path: di.path}
	}
	timeout.LogSuccess(UTCNow().Sub(start))
	return nil
}

// RUnlock - block until read lock is released.
func (di *distLockInstance) RUnlock() {
	di.rwMutex.RUnlock()
}

// localLockInstance - frontend/top-level interface for namespace locks.
type localLockInstance struct {
	ctx                 context.Context
	ns                  *NSLockMap
	volume, path, opsID string
}

// NewNSLock - returns a lock instance for a given volume and
// path. The returned lockInstance object encapsulates the NSLockMap,
// volume, path and operation ID.
func (n *NSLockMap) NewNSLock(ctx context.Context, lockersFn func() []dsync.NetLocker, volume, path string) RWLocker {
	opsID := mustGetUUID()
	if n.isDistributed {
		return &distLockInstance{dsync.NewDRWMutex(ctx, pathJoin(volume, path), &dsync.Dsync{
			GetLockersFn: lockersFn,
		}), volume, path, opsID}
	}
	return &localLockInstance{ctx, n, volume, path, opsID}
}

// Lock - block until write lock is taken or timeout has occurred.
func (li *localLockInstance) GetLock(timeout *dynamicTimeout) (timedOutErr error) {
	lockSource := getSource()
	start := UTCNow()
	readLock := false
	if !li.ns.lock(li.ctx, li.volume, li.path, lockSource, li.opsID, readLock, timeout.Timeout()) {
		timeout.LogFailure()
		return OperationTimedOut{Path: li.path}
	}
	timeout.LogSuccess(UTCNow().Sub(start))
	return
}

// Unlock - block until write lock is released.
func (li *localLockInstance) Unlock() {
	readLock := false
	li.ns.unlock(li.volume, li.path, readLock)
}

// RLock - block until read lock is taken or timeout has occurred.
func (li *localLockInstance) GetRLock(timeout *dynamicTimeout) (timedOutErr error) {
	lockSource := getSource()
	start := UTCNow()
	readLock := true
	if !li.ns.lock(li.ctx, li.volume, li.path, lockSource, li.opsID, readLock, timeout.Timeout()) {
		timeout.LogFailure()
		return OperationTimedOut{Path: li.path}
	}
	timeout.LogSuccess(UTCNow().Sub(start))
	return
}

// RUnlock - block until read lock is released.
func (li *localLockInstance) RUnlock() {
	readLock := true
	li.ns.unlock(li.volume, li.path, readLock)
}

func getSource() string {
	var funcName string
	pc, filename, lineNum, ok := runtime.Caller(2)
	if ok {
		filename = pathutil.Base(filename)
		funcName = strings.TrimPrefix(runtime.FuncForPC(pc).Name(),
			"github.com/minio/radio/cmd.")
	} else {
		filename = "<unknown>"
		lineNum = 0
	}

	return fmt.Sprintf("[%s:%d:%s()]", filename, lineNum, funcName)
}
