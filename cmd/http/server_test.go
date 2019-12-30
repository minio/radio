package http

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/minio/minio/pkg/certs"
)

func TestNewServer(t *testing.T) {
	nonLoopBackIP := getNonLoopBackIP(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, world")
	})

	testCases := []struct {
		addrs   []string
		handler http.Handler
		certFn  certs.GetCertificateFunc
	}{
		{[]string{"127.0.0.1:9000"}, handler, nil},
		{[]string{nonLoopBackIP + ":9000"}, handler, nil},
		{[]string{"127.0.0.1:9000", nonLoopBackIP + ":9000"}, handler, nil},
		{[]string{"127.0.0.1:9000"}, handler, getCert},
		{[]string{nonLoopBackIP + ":9000"}, handler, getCert},
		{[]string{"127.0.0.1:9000", nonLoopBackIP + ":9000"}, handler, getCert},
	}

	for i, testCase := range testCases {
		server := NewServer(testCase.addrs, testCase.handler, testCase.certFn)
		if server == nil {
			t.Fatalf("Case %v: server: expected: <non-nil>, got: <nil>", (i + 1))
		}

		if !reflect.DeepEqual(server.Addrs, testCase.addrs) {
			t.Fatalf("Case %v: server.Addrs: expected: %v, got: %v", (i + 1), testCase.addrs, server.Addrs)
		}

		// Interfaces are not comparable even with reflection.
		// if !reflect.DeepEqual(server.Handler, testCase.handler) {
		// 	t.Fatalf("Case %v: server.Handler: expected: %v, got: %v", (i + 1), testCase.handler, server.Handler)
		// }

		if testCase.certFn == nil {
			if server.TLSConfig != nil {
				t.Fatalf("Case %v: server.TLSConfig: expected: <nil>, got: %v", (i + 1), server.TLSConfig)
			}
		} else {
			if server.TLSConfig == nil {
				t.Fatalf("Case %v: server.TLSConfig: expected: <non-nil>, got: <nil>", (i + 1))
			}
		}

		if server.ShutdownTimeout != DefaultShutdownTimeout {
			t.Fatalf("Case %v: server.ShutdownTimeout: expected: %v, got: %v", (i + 1), DefaultShutdownTimeout, server.ShutdownTimeout)
		}

		if server.TCPKeepAliveTimeout != DefaultTCPKeepAliveTimeout {
			t.Fatalf("Case %v: server.TCPKeepAliveTimeout: expected: %v, got: %v", (i + 1), DefaultTCPKeepAliveTimeout, server.TCPKeepAliveTimeout)
		}

		if server.MaxHeaderBytes != DefaultMaxHeaderBytes {
			t.Fatalf("Case %v: server.MaxHeaderBytes: expected: %v, got: %v", (i + 1), DefaultMaxHeaderBytes, server.MaxHeaderBytes)
		}
	}
}
