package cmd

import (
	"context"
	"sync"
	"time"

	xnet "github.com/minio/minio/pkg/net"
	"github.com/minio/minio/pkg/sync/errgroup"
	"github.com/minio/radio/cmd/logger"
)

// NotificationSys - notification system.
type NotificationSys struct {
	sync.RWMutex
	peerClients []*peerRESTClient
}

// NotificationPeerErr returns error associated for a remote peer.
type NotificationPeerErr struct {
	Host xnet.Host // Remote host on which the rpc call was initiated
	Err  error     // Error returned by the remote peer for an rpc call
}

// A NotificationGroup is a collection of goroutines working on subtasks that are part of
// the same overall task.
//
// A zero NotificationGroup is valid and does not cancel on error.
type NotificationGroup struct {
	wg   sync.WaitGroup
	errs []NotificationPeerErr
}

// WithNPeers returns a new NotificationGroup with length of errs slice upto nerrs,
// upon Wait() errors are returned collected from all tasks.
func WithNPeers(nerrs int) *NotificationGroup {
	return &NotificationGroup{errs: make([]NotificationPeerErr, nerrs)}
}

// Wait blocks until all function calls from the Go method have returned, then
// returns the slice of errors from all function calls.
func (g *NotificationGroup) Wait() []NotificationPeerErr {
	g.wg.Wait()
	return g.errs
}

// Go calls the given function in a new goroutine.
//
// The first call to return a non-nil error will be
// collected in errs slice and returned by Wait().
func (g *NotificationGroup) Go(ctx context.Context, f func() error, index int, addr xnet.Host) {
	g.wg.Add(1)

	go func() {
		defer g.wg.Done()
		g.errs[index] = NotificationPeerErr{
			Host: addr,
		}
		for i := 0; i < 3; i++ {
			if err := f(); err != nil {
				g.errs[index].Err = err
				// Last iteration log the error.
				if i == 2 {
					reqInfo := (&logger.ReqInfo{}).AppendTags("peerAddress", addr.String())
					ctx := logger.SetReqInfo(ctx, reqInfo)
					logger.LogIf(ctx, err)
				}
				// Wait for one second and no need wait after last attempt.
				if i < 2 {
					time.Sleep(1 * time.Second)
				}
				continue
			}
			break
		}
	}()
}

// NewNotificationSys - creates new notification system object.
func NewNotificationSys(endpoints []Endpoint, token string) *NotificationSys {
	// bucketRulesMap/bucketRemoteTargetRulesMap are initialized by NotificationSys.Init()
	ns := &NotificationSys{}
	ns.peerClients = newPeerRestClients(endpoints, token)
	return ns
}

// PutJournalRec - PUT journal record on all peers.
func (sys *NotificationSys) PutJournalRec(ctx context.Context, entry journalEntry) {
	if sys == nil {
		return
	}
	g := errgroup.WithNErrs(len(sys.peerClients))
	for index, client := range sys.peerClients {
		if client == nil {
			continue
		}
		index := index
		g.Go(func() error {
			return sys.peerClients[index].PutJournalRec(entry)
		}, index)
	}
	for i, err := range g.Wait() {
		if err != nil {
			logger.GetReqInfo(ctx).AppendTags("remotePeer", sys.peerClients[i].host.String())
			logger.LogIf(ctx, err)
		}
	}
}

// RemoveJournalRec - calls RemoveJournalRec REST call on all peers.
func (sys *NotificationSys) RemoveJournalRec(ctx context.Context, journalDir string) {
	if sys == nil {
		return
	}
	go func() {
		ng := WithNPeers(len(sys.peerClients))
		for idx, client := range sys.peerClients {
			if client == nil {
				continue
			}
			client := client
			ng.Go(ctx, func() error {
				return client.RemoveJournalRec(journalDir)
			}, idx, *client.host)
		}
		ng.Wait()
	}()
}
