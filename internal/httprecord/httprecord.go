// Package httprecord provides record/replay wrappers for http.RoundTripper.
//
// GOLINK_RECORD=<path> records every HTTP exchange to a JSONL cassette file.
// GOLINK_REPLAY=<path> serves responses from a cassette without hitting the network.
// The two modes are mutually exclusive.
//
// WARNING: cassettes may contain PII from response bodies (member names, etc.).
// Authorization headers are always redacted before writing. Operators should
// curate cassettes before sharing.
package httprecord

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

const maxInlineBodyBytes = 1024
const maxCassetteLineBytes = 1 * 1024 * 1024 // 1 MiB

// Entry is one line of a JSONL cassette file.
type Entry struct {
	Seq         int           `json:"seq"`
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	BodySHA256  string        `json:"body_sha256"`
	RequestBody string        `json:"request_body,omitempty"`
	Response    EntryResponse `json:"response"`
}

// EntryResponse holds the recorded HTTP response.
type EntryResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body_base64"`
}

// replayKey uniquely identifies a request for cassette lookup.
type replayKey struct {
	method     string
	url        string
	bodySHA256 string
}

// Cassette holds a collection of recorded entries indexed for replay.
type Cassette struct {
	mu      sync.Mutex
	entries []Entry
	index   map[replayKey]EntryResponse
	seq     int
}

// LoadCassette reads a JSONL cassette from path.
func LoadCassette(path string) (*Cassette, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cassette: %w", err)
	}
	defer func() { _ = f.Close() }()

	c := &Cassette{index: make(map[replayKey]EntryResponse)}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxCassetteLineBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse cassette line: %w", err)
		}
		c.entries = append(c.entries, e)
		k := replayKey{method: e.Method, url: e.URL, bodySHA256: e.BodySHA256}
		c.index[k] = e.Response
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan cassette: %w", err)
	}
	c.seq = len(c.entries)
	return c, nil
}

// newCassette returns an empty cassette for recording.
func newCassette() *Cassette {
	return &Cassette{index: make(map[replayKey]EntryResponse)}
}

// Save persists the current in-memory cassette entries to path.
// Repeated calls are idempotent and overwrite previous file contents.
func (c *Cassette) Save(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open cassette for write: %w", err)
	}
	defer func() { _ = f.Close() }()

	for _, e := range c.entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal cassette entry: %w", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write cassette entry: %w", err)
		}
	}
	return nil
}

// RecordTransport wraps a base RoundTripper to record every exchange into c.
type RecordTransport struct {
	base     http.RoundTripper
	path     string
	cassette *Cassette
}

// RoundTrip performs the request, records the exchange, and returns the response.
func (rt *RecordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read and restore the request body.
	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("record: read request body: %w", err)
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Read and restore the response body.
	respBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	if readErr != nil {
		// Surface read failures to the caller so truncated bodies are not
		// silently accepted. We still return resp so callers that want to
		// inspect the partial body can.
		return resp, fmt.Errorf("record: read response body: %w", readErr)
	}

	bodyHash := bodySHA256(reqBody)
	entry := Entry{
		Seq:        rt.cassette.nextSeq(),
		Method:     req.Method,
		URL:        req.URL.String(),
		BodySHA256: bodyHash,
		Response: EntryResponse{
			Status:  resp.StatusCode,
			Headers: redactResponseHeaders(resp.Header),
			Body:    base64.StdEncoding.EncodeToString(respBody),
		},
	}
	if len(reqBody) > 0 && len(reqBody) <= maxInlineBodyBytes {
		entry.RequestBody = string(reqBody)
	}

	rt.cassette.mu.Lock()
	rt.cassette.entries = append(rt.cassette.entries, entry)
	rt.cassette.mu.Unlock()

	// Flush this entry immediately (append-only).
	if err := appendEntry(rt.path, entry); err != nil {
		// Log failure but don't break the response path.
		_, _ = fmt.Fprintf(os.Stderr, "httprecord: write failed: %v\n", err)
	}

	return resp, nil
}

// ReplayTransport serves responses from a cassette without network access.
type ReplayTransport struct {
	cassette *Cassette
}

// RoundTrip looks up the request in the cassette and returns the stored response.
// It returns an error if no matching entry is found — replay never falls back
// to the network.
func (rt *ReplayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("replay: read request body: %w", err)
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	k := replayKey{
		method:     req.Method,
		url:        req.URL.String(),
		bodySHA256: bodySHA256(reqBody),
	}

	rt.cassette.mu.Lock()
	recorded, ok := rt.cassette.index[k]
	rt.cassette.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("httprecord replay: no recorded response for %s %s body=%s",
			req.Method, req.URL.String(), k.bodySHA256)
	}

	body, err := base64.StdEncoding.DecodeString(recorded.Body)
	if err != nil {
		return nil, fmt.Errorf("replay: decode response body: %w", err)
	}

	header := make(http.Header)
	for k, vs := range recorded.Headers {
		header[k] = vs
	}

	return &http.Response{
		StatusCode: recorded.Status,
		Status:     fmt.Sprintf("%d %s", recorded.Status, http.StatusText(recorded.Status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}, nil
}

// Wrap returns a RoundTripper wrapping base with record or replay behaviour
// depending on which path is non-empty. Both being non-empty is an error.
// If both are empty, base is returned unchanged.
func Wrap(base http.RoundTripper, recordPath, replayPath string) (http.RoundTripper, error) {
	if recordPath != "" && replayPath != "" {
		return nil, errors.New("GOLINK_RECORD and GOLINK_REPLAY are mutually exclusive")
	}
	if base == nil {
		base = http.DefaultTransport
	}
	if recordPath != "" {
		return &RecordTransport{
			base:     base,
			path:     recordPath,
			cassette: newCassette(),
		}, nil
	}
	if replayPath != "" {
		c, err := LoadCassette(replayPath)
		if err != nil {
			return nil, fmt.Errorf("load replay cassette: %w", err)
		}
		return &ReplayTransport{cassette: c}, nil
	}
	return base, nil
}

func bodySHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// redactResponseHeaders returns a copy of the headers with sensitive values removed.
// Authorization and Cookie headers are never written to the cassette.
func redactResponseHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "cookie" || lower == "set-cookie" {
			continue
		}
		out[k] = vs
	}
	return out
}

func (c *Cassette) nextSeq() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func appendEntry(path string, e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
