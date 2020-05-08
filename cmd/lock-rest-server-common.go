package cmd

import (
	"errors"
)

const (
	lockRESTVersion       = "v1"
	lockRESTVersionPrefix = SlashSeparator + "v1"
	lockRESTPrefix        = radioReservedBucketPath + "/lock"
)

const (
	lockRESTMethodLock    = "/lock"
	lockRESTMethodRLock   = "/rlock"
	lockRESTMethodUnlock  = "/unlock"
	lockRESTMethodRUnlock = "/runlock"
	lockRESTMethodExpired = "/expired"

	// Unique ID of lock/unlock request.
	lockRESTUID = "uid"
	// Source contains the line number, function and file name of the code
	// on the client node that requested the lock.
	lockRESTSource = "source"
)

var (
	errLockConflict   = errors.New("lock conflict")
	errLockNotExpired = errors.New("lock not expired")
)
