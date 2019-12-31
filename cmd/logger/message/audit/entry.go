package audit

import (
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio/pkg/handlers"
	xhttp "github.com/minio/radio/cmd/http"
)

// Version - represents the current version of audit log structure.
const Version = "1"

// Entry - audit entry logs.
type Entry struct {
	Version      string `json:"version"`
	DeploymentID string `json:"deploymentid,omitempty"`
	Time         string `json:"time"`
	API          struct {
		Name            string `json:"name,omitempty"`
		Bucket          string `json:"bucket,omitempty"`
		Object          string `json:"object,omitempty"`
		Status          string `json:"status,omitempty"`
		StatusCode      int    `json:"statusCode,omitempty"`
		TimeToFirstByte string `json:"timeToFirstByte,omitempty"`
		TimeToResponse  string `json:"timeToResponse,omitempty"`
	} `json:"api"`
	RemoteHost string                 `json:"remotehost,omitempty"`
	RequestID  string                 `json:"requestID,omitempty"`
	UserAgent  string                 `json:"userAgent,omitempty"`
	ReqClaims  map[string]interface{} `json:"requestClaims,omitempty"`
	ReqQuery   map[string]string      `json:"requestQuery,omitempty"`
	ReqHeader  map[string]string      `json:"requestHeader,omitempty"`
	RespHeader map[string]string      `json:"responseHeader,omitempty"`
}

// ToEntry - constructs an audit entry object.
func ToEntry(w http.ResponseWriter, r *http.Request, reqClaims map[string]interface{}, deploymentID string) Entry {
	reqQuery := make(map[string]string)
	for k, v := range r.URL.Query() {
		reqQuery[k] = strings.Join(v, ",")
	}
	reqHeader := make(map[string]string)
	for k, v := range r.Header {
		reqHeader[k] = strings.Join(v, ",")
	}
	respHeader := make(map[string]string)
	for k, v := range w.Header() {
		respHeader[k] = strings.Join(v, ",")
	}
	respHeader[xhttp.ETag] = strings.Trim(respHeader[xhttp.ETag], `"`)

	entry := Entry{
		Version:      Version,
		DeploymentID: deploymentID,
		RemoteHost:   handlers.GetSourceIP(r),
		RequestID:    w.Header().Get(xhttp.AmzRequestID),
		UserAgent:    r.UserAgent(),
		Time:         time.Now().UTC().Format(time.RFC3339Nano),
		ReqQuery:     reqQuery,
		ReqHeader:    reqHeader,
		ReqClaims:    reqClaims,
		RespHeader:   respHeader,
	}

	return entry
}
