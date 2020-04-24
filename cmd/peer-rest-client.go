package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/gob"
	"io"
	"net/url"
	"sync/atomic"

	"github.com/minio/minio-go/v6/pkg/set"
	xnet "github.com/minio/minio/pkg/net"
	"github.com/minio/radio/cmd/http"
	"github.com/minio/radio/cmd/logger"
	"github.com/minio/radio/cmd/rest"
)

// client to talk to peer Nodes.
type peerRESTClient struct {
	host       *xnet.Host
	restClient *rest.Client
	connected  int32
}

// Reconnect to a peer rest server.
func (client *peerRESTClient) reConnect() {
	atomic.StoreInt32(&client.connected, 1)
}

// Wrapper to restClient.Call to handle network errors, in case of network error the connection is marked disconnected
// permanently. The only way to restore the connection is at the xl-sets layer by xlsets.monitorAndConnectEndpoints()
// after verifying format.json
func (client *peerRESTClient) call(method string, values url.Values, body io.Reader, length int64) (respBody io.ReadCloser, err error) {
	return client.callWithContext(GlobalContext, method, values, body, length)
}

// Wrapper to restClient.Call to handle network errors, in case of network error the connection is marked disconnected
// permanently. The only way to restore the connection is at the xl-sets layer by xlsets.monitorAndConnectEndpoints()
// after verifying format.json
func (client *peerRESTClient) callWithContext(ctx context.Context, method string, values url.Values, body io.Reader, length int64) (respBody io.ReadCloser, err error) {
	if !client.IsOnline() {
		client.reConnect()
	}

	if values == nil {
		values = make(url.Values)
	}

	respBody, err = client.restClient.CallWithContext(ctx, method, values, body, length)
	if err == nil {
		return respBody, nil
	}

	if isNetworkError(err) {
		atomic.StoreInt32(&client.connected, 0)
	}

	return nil, err
}

// Stringer provides a canonicalized representation of node.
func (client *peerRESTClient) String() string {
	return client.host.String()
}

// IsOnline - returns whether RPC client failed to connect or not.
func (client *peerRESTClient) IsOnline() bool {
	return atomic.LoadInt32(&client.connected) == 1
}

// Close - marks the client as closed.
func (client *peerRESTClient) Close() error {
	atomic.StoreInt32(&client.connected, 0)
	client.restClient.Close()
	return nil
}

func getRemoteHosts(endpoints []Endpoint) []*xnet.Host {
	var remoteHosts []*xnet.Host
	for _, hostStr := range GetRemotePeers(endpoints) {
		host, err := xnet.ParseHost(hostStr)
		if err != nil {
			logger.LogIf(GlobalContext, err)
			continue
		}
		remoteHosts = append(remoteHosts, host)
	}

	return remoteHosts
}

// GetRemotePeers - get hosts information other than this minio service.
func GetRemotePeers(ep []Endpoint) []string {
	peerSet := set.NewStringSet()
	for _, endpoint := range ep {
		if endpoint.Type() != URLEndpointType {
			continue
		}

		peer := endpoint.Host
		if endpoint.IsLocal {
			if _, port := mustSplitHostPort(peer); port == globalRadioPort {
				continue
			}
		}

		peerSet.Add(peer)
	}
	return peerSet.ToSlice()
}

// newPeerRestClients creates new peer clients.
func newPeerRestClients(endpoints []Endpoint, token string) []*peerRESTClient {
	peerHosts := getRemoteHosts(endpoints)
	restClients := make([]*peerRESTClient, len(peerHosts))
	for i, host := range peerHosts {
		client, err := newPeerRESTClient(host, token)
		if err != nil {
			logger.LogIf(GlobalContext, err)
			continue
		}
		restClients[i] = client
	}

	return restClients
}

// Returns a peer rest client.
func newPeerRESTClient(peer *xnet.Host, token string) (*peerRESTClient, error) {
	scheme := "http"
	if globalIsSSL {
		scheme = "https"
	}

	serverURL := &url.URL{
		Scheme: scheme,
		Host:   peer.String(),
		Path:   peerRESTPath,
	}

	var tlsConfig *tls.Config
	if globalIsSSL {
		tlsConfig = &tls.Config{
			ServerName: peer.Name,
			RootCAs:    globalRootCAs,
		}
	}

	trFn := newCustomHTTPTransport(tlsConfig, rest.DefaultRESTTimeout)
	restClient, err := rest.NewClient(serverURL, trFn, func() string {
		return token
	})
	if err != nil {
		return nil, err
	}

	return &peerRESTClient{host: peer, restClient: restClient, connected: 1}, nil
}

// PutJournalRec - PUT journal entry on peer node.
func (client *peerRESTClient) PutJournalRec(entry journalEntry) error {
	values := make(url.Values)
	// values.Set(peerRESTJournalDir, journalDir)

	var reader bytes.Buffer
	err := gob.NewEncoder(&reader).Encode(&entry)
	if err != nil {
		return err
	}

	respBody, err := client.call(peerRESTMethodPutJournalRec, values, &reader, -1)
	if err != nil {
		return err
	}
	defer http.DrainBody(respBody)
	return nil
}

// RemoveJournalRec - Remove journal rec on the peer node.
func (client *peerRESTClient) RemoveJournalRec(journalDir string) error {
	values := make(url.Values)
	values.Set(peerRESTJournalDir, journalDir)
	respBody, err := client.call(peerRESTMethodDeleteJournalRec, values, nil, -1)
	if err != nil {
		return err
	}
	defer http.DrainBody(respBody)
	return nil
}
