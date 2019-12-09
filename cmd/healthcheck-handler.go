package cmd

import (
	"fmt"
	"net/http"
	"runtime"

	xhttp "github.com/minio/minio/cmd/http"
)

const (
	minioHealthGoroutineThreshold = 10000
)

// ReadinessCheckHandler -- checks if there are more than threshold
// number of goroutines running, returns service unavailable.
//
// Readiness probes are used to detect situations where application
// is under heavy load and temporarily unable to serve. In a orchestrated
// setup like Kubernetes, containers reporting that they are not ready do
// not receive traffic through Kubernetes Services.
func ReadinessCheckHandler(w http.ResponseWriter, r *http.Request) {
	if err := goroutineCountCheck(minioHealthGoroutineThreshold); err != nil {
		writeResponse(w, http.StatusServiceUnavailable, nil, mimeNone)
		return
	}
	writeResponse(w, http.StatusOK, nil, mimeNone)
}

// LivenessCheckHandler -- checks if server can reach its disks internally.
// If not, server is considered to have failed and needs to be restarted.
// Liveness probes are used to detect situations where application (minio)
// has gone into a state where it can not recover except by being restarted.
func LivenessCheckHandler(w http.ResponseWriter, r *http.Request) {
	objLayer := newObjectLayerFn()
	// Service not initialized yet
	if objLayer == nil {
		// Respond with 200 OK while server initializes to ensure a distributed cluster
		// is able to start on orchestration platforms like Docker Swarm.
		// Refer https://github.com/minio/minio/issues/8140 for more details.
		// Make sure to add server not initialized status in header
		w.Header().Set(xhttp.MinIOServerStatus, "server-not-initialized")
		writeSuccessResponseHeadersOnly(w)
		return
	}

	writeResponse(w, http.StatusOK, nil, mimeNone)
}

// checks threshold against total number of go-routines in the system and
// throws error if more than threshold go-routines are running.
func goroutineCountCheck(threshold int) error {
	count := runtime.NumGoroutine()
	if count > threshold {
		return fmt.Errorf("too many goroutines (%d > %d)", count, threshold)
	}
	return nil
}
