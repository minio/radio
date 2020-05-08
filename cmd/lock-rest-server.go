package cmd

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"path"
	"sort"
	"time"

	"github.com/gorilla/mux"
	"github.com/minio/minio/pkg/dsync"
	xnet "github.com/minio/minio/pkg/net"
	"github.com/minio/radio/cmd/rest"
)

// DefaultSkewTime - skew time is 15 minutes between minio peers.
const DefaultSkewTime = 15 * time.Minute

// Authenticates storage client's requests and validates for skewed time.
func lockRequestValidate(r *http.Request, token string) error {
	authToken := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(authToken), []byte(fmt.Sprintf("Bearer %s", token))) != 1 {
		return errors.New("invalid auth token")
	}
	requestTimeStr := r.Header.Get("X-Radio-Time")
	requestTime, err := time.Parse(time.RFC3339, requestTimeStr)
	if err != nil {
		return err
	}
	utcNow := UTCNow()
	delta := requestTime.Sub(utcNow)
	if delta < 0 {
		delta = delta * -1
	}
	if delta > DefaultSkewTime {
		return fmt.Errorf("client time %v is too apart with server time %v", requestTime, utcNow)
	}
	return nil
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if nerr, ok := err.(*rest.NetworkError); ok {
		return xnet.IsNetworkOrHostDown(nerr.Err)
	}
	return false
}

const (
	// Lock maintenance interval.
	lockMaintenanceInterval = 1 * time.Minute

	// Lock validity check interval.
	lockValidityCheckInterval = 2 * time.Minute
)

// To abstract a node over network.
type lockRESTServer struct {
	ll    *localLocker
	token string
}

func (l *lockRESTServer) writeErrorResponse(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(err.Error()))
}

// IsValid - To authenticate and verify the time difference.
func (l *lockRESTServer) IsValid(w http.ResponseWriter, r *http.Request) bool {
	if err := lockRequestValidate(r, l.token); err != nil {
		l.writeErrorResponse(w, err)
		return false
	}
	return true
}

func getLockArgs(r *http.Request) (args dsync.LockArgs, err error) {
	args = dsync.LockArgs{
		UID:    r.URL.Query().Get(lockRESTUID),
		Source: r.URL.Query().Get(lockRESTSource),
	}

	var resources []string
	bio := bufio.NewScanner(r.Body)
	for bio.Scan() {
		resources = append(resources, bio.Text())
	}

	if err := bio.Err(); err != nil {
		return args, err
	}

	sort.Strings(resources)
	args.Resources = resources
	return args, nil
}

// LockHandler - Acquires a lock.
func (l *lockRESTServer) LockHandler(w http.ResponseWriter, r *http.Request) {
	if !l.IsValid(w, r) {
		l.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}

	args, err := getLockArgs(r)
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}

	success, err := l.ll.Lock(args)
	if err == nil && !success {
		err = errLockConflict
	}
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}
}

// UnlockHandler - releases the acquired lock.
func (l *lockRESTServer) UnlockHandler(w http.ResponseWriter, r *http.Request) {
	if !l.IsValid(w, r) {
		l.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}

	args, err := getLockArgs(r)
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}

	_, err = l.ll.Unlock(args)
	// Ignore the Unlock() "reply" return value because if err == nil, "reply" is always true
	// Consequently, if err != nil, reply is always false
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}
}

// LockHandler - Acquires an RLock.
func (l *lockRESTServer) RLockHandler(w http.ResponseWriter, r *http.Request) {
	if !l.IsValid(w, r) {
		l.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}

	args, err := getLockArgs(r)
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}

	success, err := l.ll.RLock(args)
	if err == nil && !success {
		err = errLockConflict
	}
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}
}

// RUnlockHandler - releases the acquired read lock.
func (l *lockRESTServer) RUnlockHandler(w http.ResponseWriter, r *http.Request) {
	if !l.IsValid(w, r) {
		l.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}

	args, err := getLockArgs(r)
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}

	// Ignore the RUnlock() "reply" return value because if err == nil, "reply" is always true.
	// Consequently, if err != nil, reply is always false
	if _, err = l.ll.RUnlock(args); err != nil {
		l.writeErrorResponse(w, err)
		return
	}
}

// ExpiredHandler - query expired lock status.
func (l *lockRESTServer) ExpiredHandler(w http.ResponseWriter, r *http.Request) {
	if !l.IsValid(w, r) {
		l.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}

	args, err := getLockArgs(r)
	if err != nil {
		l.writeErrorResponse(w, err)
		return
	}

	l.ll.mutex.Lock()
	defer l.ll.mutex.Unlock()

	// Lock found, proceed to verify if belongs to given uid.
	for _, resource := range args.Resources {
		if lri, ok := l.ll.lockMap[resource]; ok {
			// Check whether uid is still active
			for _, entry := range lri {
				if entry.UID == args.UID {
					l.writeErrorResponse(w, errLockNotExpired)
					return
				}
			}
		}
	}
}

// nameLockRequesterInfoPair is a helper type for lock maintenance
type nameLockRequesterInfoPair struct {
	name string
	lri  lockRequesterInfo
}

// getLongLivedLocks returns locks that are older than a certain time and
// have not been 'checked' for validity too soon enough
func getLongLivedLocks(interval time.Duration) map[Endpoint][]nameLockRequesterInfoPair {
	nlripMap := make(map[Endpoint][]nameLockRequesterInfoPair)
	for endpoint, locker := range globalLockServers {
		rslt := []nameLockRequesterInfoPair{}
		locker.mutex.Lock()
		for name, lriArray := range locker.lockMap {
			for idx := range lriArray {
				// Check whether enough time has gone by since last check
				if time.Since(lriArray[idx].TimeLastCheck) >= interval {
					rslt = append(rslt, nameLockRequesterInfoPair{name: name, lri: lriArray[idx]})
					lriArray[idx].TimeLastCheck = UTCNow()
				}
			}
		}
		nlripMap[endpoint] = rslt
		locker.mutex.Unlock()
	}
	return nlripMap
}

// lockMaintenance loops over locks that have been active for some time and checks back
// with the original server whether it is still alive or not
//
// Following logic inside ignores the errors generated for Dsync.Active operation.
// - server at client down
// - some network error (and server is up normally)
//
// We will ignore the error, and we will retry later to get a resolve on this lock
func lockMaintenance(ctx context.Context, interval time.Duration, endpoints Endpoints, token string) error {
	// Validate if long lived locks are indeed clean.
	// Get list of long lived locks to check for staleness.
	for lendpoint, nlrips := range getLongLivedLocks(interval) {
		nlripsMap := make(map[string]int, len(nlrips))
		for _, nlrip := range nlrips {
			// Locks are only held on first zone, make sure that
			// we only look for ownership of locks from endpoints
			// on first zone.
			for _, endpoint := range endpoints {
				c := newLockAPI(endpoint, token)
				if !c.IsOnline() {
					nlripsMap[nlrip.name]++
					continue
				}

				// Call back to original server verify whether the lock is
				// still active (based on name & uid)
				expired, err := c.Expired(dsync.LockArgs{
					UID:       nlrip.lri.UID,
					Resources: []string{nlrip.name},
				})
				if err != nil {
					nlripsMap[nlrip.name]++
					c.Close()
					continue
				}

				if !expired {
					nlripsMap[nlrip.name]++
				}

				// Close the connection regardless of the call response.
				c.Close()
			}

			// Read locks we assume quorum for be N/2 success
			quorum := len(endpoints) / 2
			if nlrip.lri.Writer {
				// For write locks we need N/2+1 success
				quorum = len(endpoints)/2 + 1
			}

			// less than the quorum, we have locks expired.
			if nlripsMap[nlrip.name] < quorum {
				// The lock is no longer active at server that originated
				// the lock, attempt to remove the lock.
				globalLockServers[lendpoint].mutex.Lock()
				// Purge the stale entry if it exists.
				globalLockServers[lendpoint].removeEntryIfExists(nlrip)
				globalLockServers[lendpoint].mutex.Unlock()
			}

		}
	}

	return nil
}

// Start lock maintenance from all lock servers.
func startLockMaintenance(endpoints Endpoints, token string) {
	// Start with random sleep time, so as to avoid "synchronous checks" between servers
	time.Sleep(time.Duration(rand.Float64() * float64(lockMaintenanceInterval)))

	// Initialize a new ticker with a minute between each ticks.
	ticker := time.NewTicker(lockMaintenanceInterval)
	// Stop the timer upon service closure and cleanup the go-routine.
	defer ticker.Stop()

	for {
		// Verifies every minute for locks held more than 2 minutes.
		select {
		case <-GlobalContext.Done():
			return
		case <-ticker.C:
			lockMaintenance(GlobalContext, lockValidityCheckInterval, endpoints, token)
		}
	}
}

// registerLockRESTHandlers - register lock rest router.
func registerLockRESTHandlers(router *mux.Router, endpoints Endpoints, token string) {
	queries := restQueries(lockRESTUID, lockRESTSource)
	for _, endpoint := range endpoints {
		if !endpoint.IsLocal {
			continue
		}

		lockServer := &lockRESTServer{
			ll:    newLocker(endpoint),
			token: token,
		}

		subrouter := router.PathPrefix(path.Join(lockRESTPrefix, lockRESTVersionPrefix)).Subrouter()
		subrouter.Methods(http.MethodPost).Path(lockRESTMethodLock).HandlerFunc(httpTraceHdrs(lockServer.LockHandler)).Queries(queries...)
		subrouter.Methods(http.MethodPost).Path(lockRESTMethodRLock).HandlerFunc(httpTraceHdrs(lockServer.RLockHandler)).Queries(queries...)
		subrouter.Methods(http.MethodPost).Path(lockRESTMethodUnlock).HandlerFunc(httpTraceHdrs(lockServer.UnlockHandler)).Queries(queries...)
		subrouter.Methods(http.MethodPost).Path(lockRESTMethodRUnlock).HandlerFunc(httpTraceHdrs(lockServer.RUnlockHandler)).Queries(queries...)
		subrouter.Methods(http.MethodPost).Path(lockRESTMethodExpired).HandlerFunc(httpTraceAll(lockServer.ExpiredHandler)).Queries(queries...)

		globalLockServers[endpoint] = lockServer.ll
	}

	go startLockMaintenance(endpoints, token)
}
