package logger

import (
	"context"
	"sync"

	"time"
)

// Holds a map of recently logged errors.
type logOnceType struct {
	IDMap map[interface{}]error
	sync.Mutex
}

// One log message per error.
func (l *logOnceType) logOnceIf(ctx context.Context, err error, id interface{}, errKind ...interface{}) {
	if err == nil {
		return
	}
	l.Lock()
	shouldLog := false
	prevErr := l.IDMap[id]
	if prevErr == nil {
		l.IDMap[id] = err
		shouldLog = true
	} else {
		if prevErr.Error() != err.Error() {
			l.IDMap[id] = err
			shouldLog = true
		}
	}
	l.Unlock()

	if shouldLog {
		LogIf(ctx, err, errKind...)
	}
}

// Cleanup the map every 30 minutes so that the log message is printed again for the user to notice.
func (l *logOnceType) cleanupRoutine() {
	for {
		l.Lock()
		l.IDMap = make(map[interface{}]error)
		l.Unlock()

		time.Sleep(30 * time.Minute)
	}
}

// Returns logOnceType
func newLogOnceType() *logOnceType {
	l := &logOnceType{IDMap: make(map[interface{}]error)}
	go l.cleanupRoutine()
	return l
}

var logOnce = newLogOnceType()

// LogOnceIf - Logs notification errors - once per error.
// id is a unique identifier for related log messages, refer to cmd/notification.go
// on how it is used.
func LogOnceIf(ctx context.Context, err error, id interface{}, errKind ...interface{}) {
	logOnce.logOnceIf(ctx, err, id, errKind...)
}
