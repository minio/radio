package cmd

import (
	"crypto/subtle"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/minio/radio/cmd/logger"

	"github.com/gorilla/mux"
)

// To abstract a node over network.
type peerRESTServer struct {
	token string
}

// Authenticates storage client's requests and validates for skewed time.
func storageServerRequestValidate(r *http.Request, token string) error {
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

func (s *peerRESTServer) writeErrorResponse(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(err.Error()))
}

// IsValid - To authenticate and verify the time difference.
func (s *peerRESTServer) IsValid(w http.ResponseWriter, r *http.Request) bool {
	if err := storageServerRequestValidate(r, s.token); err != nil {
		s.writeErrorResponse(w, err)
		return false
	}
	return true
}

// PutJournalEntryHandler - handles PUT journal entry.
func (s *peerRESTServer) PutJournalEntryHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		s.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}
	ctx := newContext(r, w, "PutJournalEntry")
	var jlog journalEntry
	if r.ContentLength < 0 {
		s.writeErrorResponse(w, errInvalidArgument)
		return
	}

	err := gob.NewDecoder(r.Body).Decode(&jlog)
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	jdir := globalHealSys.getJournalDir(jlog.ReplicaBucket, jlog.Bucket, jlog.Object)
	// if local journal on node has a entry for this object, replace the log only
	// if it is older than current entry
	currLog, err := globalHealSys.readJournalEntry(ctx, jdir)
	if os.IsNotExist(err) {
		if err := globalHealSys.saveJournalEntry(ctx, jlog); err != nil {
			logger.LogIf(ctx, err)
		}
		return
	}
	if currLog.Timestamp.Before(jlog.Timestamp) {
		globalHealSys.saveJournalEntry(ctx, jlog)
	}

	w.(http.Flusher).Flush()
}

// RemoveJournalEntryHandler - handles DELETE journal entry.
func (s *peerRESTServer) RemoveJournalEntryHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		s.writeErrorResponse(w, errors.New("Invalid request"))
		return
	}
	ctx := newContext(r, w, "RemoveJournalEntry")

	vars := mux.Vars(r)
	journalDir := vars[peerRESTJournalDir]
	if journalDir == "" {
		s.writeErrorResponse(w, errors.New("Journal dir is missing"))
		return
	}
	globalHealSys.removeJournalEntry(ctx, journalDir)
	w.(http.Flusher).Flush()
}

// registerPeerRESTHandlers - register peer rest router.
func registerPeerRESTHandlers(router *mux.Router, endpoints Endpoints, token string) {
	for _, endpoint := range endpoints {
		if !endpoint.IsLocal {
			continue
		}
		server := &peerRESTServer{token: token}
		subrouter := router.PathPrefix(peerRESTPrefix).Subrouter()
		subrouter.Methods(http.MethodPost).Path(peerRESTVersionPrefix + peerRESTMethodDeleteJournalRec).HandlerFunc(httpTraceHdrs(server.RemoveJournalEntryHandler)).Queries(restQueries(peerRESTJournalDir)...)
		subrouter.Methods(http.MethodPost).Path(peerRESTVersionPrefix + peerRESTMethodPutJournalRec).HandlerFunc(httpTraceHdrs(server.PutJournalEntryHandler))
	}
}
