package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// conditionalGET adds a strong ETag to a successful list response and answers
// a matching If-None-Match with 304 and no body.
//
// The tag is a hash of the serialised body. That is correct by construction:
// any field a client can see changes the bytes, so a bulk update or a toggle
// changes the tag with no per-endpoint bookkeeping. The cost is that the
// handler still runs its query and serialisation to find out the answer; only
// the transfer is saved. A cheaper key (max updated_at plus row count) would
// skip the query but misses changes the tables do not timestamp, such as
// bulk toggles and channel health.
//
// writeJSON sets Cache-Control: no-store, which forbids a client from keeping
// the response at all, so it would never have anything to revalidate. These
// responses use "private, no-cache" instead: a client may store the body but
// must revalidate before each reuse, and a shared proxy may not store it.
func conditionalGET(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		h := w.Header()
		for k, v := range rec.header {
			h[k] = v
		}
		if rec.status == http.StatusOK {
			sum := sha256.Sum256(rec.body.Bytes())
			etag := `"` + hex.EncodeToString(sum[:16]) + `"`
			h.Set("ETag", etag)
			h.Set("Cache-Control", "private, no-cache")
			if etagMatches(r.Header.Get("If-None-Match"), etag) {
				h.Del("Content-Type")
				h.Del("Content-Length")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

// etagMatches reports whether an If-None-Match value names etag. "*" matches
// anything, and a weak validator (W/"x") compares equal to the strong "x".
func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}

// bufferedResponse holds a handler's response so its body can be hashed
// before anything is sent.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(status int)      { b.status = status }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }
