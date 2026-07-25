// Package network provides idiomatic outbound HTTP for Lahijan plugins.
// The host applies a process-wide URL allowlist and a default timeout; no
// direct socket access (per ADR-0023 — WASI Preview 1 has no sockets, so
// outbound HTTP always goes through this host function).
//
// The response-buffer protocol (ADR-0038): the host writes a 4-byte
// little-endian uint32 length prefix followed by the data into each
// response buffer (headers JSON + body). If either buffer is too small the
// host returns StatusBufferTooSmall and the SDK grows both buffers and
// retries automatically.
package network

import (
	"encoding/binary"
	"encoding/json"

	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Request describes an outbound HTTP call.
type Request struct {
	Method  string            // "GET", "POST", "PUT", "DELETE", ...
	URL     string            // absolute URL
	Headers map[string]string // optional; JSON-encoded by the SDK
	Body    []byte            // optional
}

// Response holds the full HTTP response.
type Response struct {
	StatusCode int                // HTTP status code (100-599)
	Headers    map[string]string // response headers
	Body       []byte            // response body
}

const (
	initialHdrBufSize  = 1024            // 1 KiB start for response headers
	initialBodyBufSize = 4 * 1024        // 4 KiB start for response body
	maxHdrBufSize      = 8 * 1024        // matches MaxHTTPHeaderLen on the host
	maxBodyBufSize     = 1024 * 1024     // matches MaxHTTPBodyLen on the host (1 MiB)
	lenPrefixSize      = 4               // 4-byte LE uint32 length prefix per buffer
)

// Do performs an outbound HTTP request and returns the full response
// (status code, headers, body). The BufferTooSmall retry is handled
// internally: the SDK starts with 1 KiB / 4 KiB buffers and doubles them
// until the response fits or the host-side cap is reached.
func Do(req Request) (Response, error) {
	method := []byte(req.Method)
	url := []byte(req.URL)

	var hdrJSON []byte
	if len(req.Headers) > 0 {
		var err error
		hdrJSON, err = json.Marshal(req.Headers)
		if err != nil {
			return Response{}, err
		}
	}

	hdrBuf := make([]byte, initialHdrBufSize)
	bodyBuf := make([]byte, initialBodyBufSize)

	for {
		code := networkHTTPRequest(
			mem.Ptr(method), mem.Len(method),
			mem.Ptr(url), mem.Len(url),
			mem.Ptr(hdrJSON), mem.Len(hdrJSON),
			mem.Ptr(req.Body), mem.Len(req.Body),
			mem.Ptr(hdrBuf), uint32(len(hdrBuf)),
			mem.Ptr(bodyBuf), uint32(len(bodyBuf)),
		)

		if status.Code(code) == status.BufferTooSmall {
			if len(hdrBuf) >= maxHdrBufSize && len(bodyBuf) >= maxBodyBufSize {
				return Response{}, status.ErrBufferTooSmall
			}
			if len(hdrBuf) < maxHdrBufSize {
				hdrBuf = make([]byte, len(hdrBuf)*2)
			}
			if len(bodyBuf) < maxBodyBufSize {
				bodyBuf = make([]byte, len(bodyBuf)*2)
			}
			continue
		}

		if err := status.FromCode(code); err != nil {
			return Response{}, err
		}

		resp := Response{StatusCode: int(code)}
		resp.Headers = readLenPrefixed(hdrBuf)
		resp.Body = readLenPrefixedBytes(bodyBuf)
		return resp, nil
	}
}

// Get is shorthand for Do(Request{Method:"GET", URL:url}).
func Get(url string) (Response, error) {
	return Do(Request{Method: "GET", URL: url})
}

// Post is shorthand for Do(Request{Method:"POST", URL:url, Body:body}).
func Post(url string, body []byte) (Response, error) {
	return Do(Request{Method: "POST", URL: url, Body: body})
}

// PostJSON is shorthand for a POST with Content-Type: application/json.
func PostJSON(url string, body []byte) (Response, error) {
	return Do(Request{
		Method:  "POST",
		URL:     url,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
}

// readLenPrefixed reads a 4-byte LE uint32 length prefix from buf, then
// JSON-decodes that many bytes into a map[string]string. Returns nil on
// any decode failure (the caller gets an empty header map).
func readLenPrefixed(buf []byte) map[string]string {
	if len(buf) < lenPrefixSize {
		return nil
	}
	n := binary.LittleEndian.Uint32(buf[:lenPrefixSize])
	if n == 0 || int(n)+lenPrefixSize > len(buf) {
		return nil
	}
	var hdrs map[string]string
	if json.Unmarshal(buf[lenPrefixSize:lenPrefixSize+n], &hdrs) != nil {
		return nil
	}
	return hdrs
}

// readLenPrefixedBytes reads a 4-byte LE uint32 length prefix from buf,
// then copies that many bytes into a new slice. Returns nil if the buffer
// is too small or the length is zero.
func readLenPrefixedBytes(buf []byte) []byte {
	if len(buf) < lenPrefixSize {
		return nil
	}
	n := binary.LittleEndian.Uint32(buf[:lenPrefixSize])
	if n == 0 || int(n)+lenPrefixSize > len(buf) {
		return nil
	}
	out := make([]byte, n)
	copy(out, buf[lenPrefixSize:lenPrefixSize+n])
	return out
}
