package cmd

import (
	"errors"
)

const (
	lockRESTVersion       = "v2"
	lockRESTVersionPrefix = SlashSeparator + "v2"
	lockRESTPrefix        = minioReservedBucketPath + "/lock"
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
	// Resource contains a entity to be locked/unlocked.
	lockRESTResource = "resource"
)

var (
	errLockConflict   = errors.New("lock conflict")
	errLockNotExpired = errors.New("lock not expired")
)
