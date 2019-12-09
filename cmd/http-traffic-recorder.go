package cmd

import (
	"io"
	"net/http"
	"time"
)

// records the incoming bytes from the underlying request.Body.
type recordTrafficRequest struct {
	io.ReadCloser
	isS3Request bool
}

// Records the bytes read.
func (r *recordTrafficRequest) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	globalConnStats.incInputBytes(n)
	if r.isS3Request {
		globalConnStats.incS3InputBytes(n)
	}
	return n, err
}

// Records the outgoing bytes through the responseWriter.
type recordTrafficResponse struct {
	// wrapper for underlying http.ResponseWriter.
	writer      http.ResponseWriter
	isS3Request bool
}

// Calls the underlying WriteHeader.
func (r *recordTrafficResponse) WriteHeader(i int) {
	r.writer.WriteHeader(i)
}

// Calls the underlying Header.
func (r *recordTrafficResponse) Header() http.Header {
	return r.writer.Header()
}

// Records the output bytes
func (r *recordTrafficResponse) Write(p []byte) (n int, err error) {
	n, err = r.writer.Write(p)
	globalConnStats.incOutputBytes(n)
	// Check if it is s3 request
	if r.isS3Request {
		globalConnStats.incS3OutputBytes(n)
	}
	return n, err
}

// Calls the underlying Flush.
func (r *recordTrafficResponse) Flush() {
	r.writer.(http.Flusher).Flush()
}

// Records the outgoing bytes through the responseWriter.
type recordAPIStats struct {
	// wrapper for underlying http.ResponseWriter.
	writer         http.ResponseWriter
	TTFB           time.Time // TimeToFirstByte.
	firstByteRead  bool
	respStatusCode int
	isS3Request    bool
}

// Calls the underlying WriteHeader.
func (r *recordAPIStats) WriteHeader(i int) {
	r.respStatusCode = i
	r.writer.WriteHeader(i)
}

// Calls the underlying Header.
func (r *recordAPIStats) Header() http.Header {
	return r.writer.Header()
}

// Records the TTFB on the first byte write.
func (r *recordAPIStats) Write(p []byte) (n int, err error) {
	if !r.firstByteRead {
		r.TTFB = UTCNow()
		r.firstByteRead = true
	}
	return r.writer.Write(p)
}

// Calls the underlying Flush.
func (r *recordAPIStats) Flush() {
	r.writer.(http.Flusher).Flush()
}
