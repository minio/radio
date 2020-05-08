package cmd

import (
	ring "container/ring"
	"context"
	"strings"
	"sync"

	"github.com/minio/minio/pkg/pubsub"
	"github.com/minio/radio/cmd/logger"
	"github.com/minio/radio/cmd/logger/message/log"
	"github.com/minio/radio/cmd/logger/target/console"
)

// number of log messages to buffer
const defaultLogBufferCount = 10000

//HTTPConsoleLoggerSys holds global console logger state
type HTTPConsoleLoggerSys struct {
	pubsub   *pubsub.PubSub
	console  *console.Target
	nodeName string
	// To protect ring buffer.
	logBufLk sync.RWMutex
	logBuf   *ring.Ring
}

// NewConsoleLogger - creates new HTTPConsoleLoggerSys with all nodes subscribed to
// the console logging pub sub system
func NewConsoleLogger(ctx context.Context) *HTTPConsoleLoggerSys {
	ps := pubsub.New()
	return &HTTPConsoleLoggerSys{
		ps, nil, "", sync.RWMutex{}, ring.New(defaultLogBufferCount),
	}
}

// HasLogListeners returns true if console log listeners are registered
// for this node or peers
func (sys *HTTPConsoleLoggerSys) HasLogListeners() bool {
	return sys != nil && sys.pubsub.HasSubscribers()
}

// LogInfo holds console log messages
type LogInfo struct {
	log.Entry
	ConsoleMsg string
	NodeName   string `json:"node"`
	Err        error  `json:"-"`
}

// SendLog returns true if log pertains to node specified in args.
func (l LogInfo) SendLog(node, logKind string) bool {
	nodeFltr := (node == "" || strings.EqualFold(node, l.NodeName))
	typeFltr := strings.EqualFold(logKind, "all") || strings.EqualFold(l.LogKind, logKind)
	return nodeFltr && typeFltr
}

// Subscribe starts console logging for this node.
func (sys *HTTPConsoleLoggerSys) Subscribe(subCh chan interface{}, doneCh chan struct{}, node string, last int, logKind string, filter func(entry interface{}) bool) {
	// Enable console logging for remote client even if local console logging is disabled in the config.
	if !sys.pubsub.HasSubscribers() {
		logger.AddTarget(globalConsoleSys.Console())
	}

	cnt := 0
	// by default send all console logs in the ring buffer unless node or limit query parameters
	// are set.
	var lastN []LogInfo
	if last > defaultLogBufferCount || last <= 0 {
		last = defaultLogBufferCount
	}

	lastN = make([]LogInfo, last)
	sys.logBufLk.RLock()
	sys.logBuf.Do(func(p interface{}) {
		if p != nil && (p.(LogInfo)).SendLog(node, logKind) {
			lastN[cnt%last] = p.(LogInfo)
			cnt++
		}
	})
	sys.logBufLk.RUnlock()
	// send last n console log messages in order filtered by node
	if cnt > 0 {
		for i := 0; i < last; i++ {
			entry := lastN[(cnt+i)%last]
			if (entry == LogInfo{}) {
				continue
			}
			select {
			case subCh <- entry:
			case <-doneCh:
				return
			}
		}
	}
	sys.pubsub.Subscribe(subCh, doneCh, filter)
}

// Console returns a console target
func (sys *HTTPConsoleLoggerSys) Console() *HTTPConsoleLoggerSys {
	if sys == nil {
		return sys
	}
	if sys.console == nil {
		sys.console = console.New()
	}
	return sys
}

// Send log message 'e' to console and publish to console
// log pubsub system
func (sys *HTTPConsoleLoggerSys) Send(e interface{}, logKind string) error {
	var lg LogInfo
	switch e := e.(type) {
	case log.Entry:
		lg = LogInfo{Entry: e, NodeName: sys.nodeName}
	case string:
		lg = LogInfo{ConsoleMsg: e, NodeName: sys.nodeName}
	}

	sys.pubsub.Publish(lg)
	sys.logBufLk.Lock()
	// add log to ring buffer
	sys.logBuf.Value = lg
	sys.logBuf = sys.logBuf.Next()
	sys.logBufLk.Unlock()

	return sys.console.Send(e, string(logger.All))
}
