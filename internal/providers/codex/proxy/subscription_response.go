package codexproxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
)

type codexDecodedBody struct {
	io.Reader
	decoder io.Closer
	source  io.Closer
}

func (b *codexDecodedBody) Close() error {
	var e error
	if b.decoder != nil {
		e = b.decoder.Close()
	}
	return errors.Join(e, b.source.Close())
}

func decodeCodexSubscriptionResponse(resp *http.Response) error {
	raw := resp.Body
	var reader io.Reader
	var closer io.Closer
	var err error
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return nil
	case "gzip":
		reader, err = gzip.NewReader(raw)
		if err == nil {
			closer = reader.(io.Closer)
		}
	case "br":
		reader = brotli.NewReader(raw)
	case "deflate":
		br := bufio.NewReader(raw)
		header, e := br.Peek(2)
		if e == nil && len(header) == 2 && header[0]&15 == 8 && (int(header[0])*256+int(header[1]))%31 == 0 {
			reader, err = zlib.NewReader(br)
			if err == nil {
				closer = reader.(io.Closer)
			}
		} else {
			reader = flate.NewReader(br)
			closer = reader.(io.Closer)
		}
	default:
		return errors.New("unsupported_subscription_encoding")
	}
	if err != nil {
		return errors.New("invalid_subscription_encoding")
	}
	resp.Body = &codexDecodedBody{Reader: reader, decoder: closer, source: raw}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

func observeCodexSubscriptionResponse(resp *http.Response, lease *subscription.Lease, path string) {
	if lease == nil {
		return
	}
	decodeErr := decodeCodexSubscriptionResponse(resp)
	resp.Body = &codexSubscriptionResponse{ReadCloser: resp.Body, lease: lease, status: resp.StatusCode, headers: resp.Header.Clone(), path: path, failed: decodeErr != nil}
}

const codexSubscriptionObservationLimit = 1 << 20

type codexSubscriptionResponse struct {
	io.ReadCloser
	lease                            *subscription.Lease
	status                           int
	headers                          http.Header
	path                             string
	mode                             byte
	jsonBody                         bytes.Buffer
	line                             bytes.Buffer
	data                             bytes.Buffer
	started, completed, failed, done bool
}

func (r *codexSubscriptionResponse) Read(p []byte) (int, error) {
	n, e := r.ReadCloser.Read(p)
	if !r.done {
		r.observe(p[:n])
		if e != nil {
			r.done = true
			r.lease.Finish(r.status, r.headers, r.jsonBody.Bytes(), e == io.EOF && r.success())
			r.jsonBody.Reset()
			r.line.Reset()
			r.data.Reset()
		}
	}
	return n, e
}
func (r *codexSubscriptionResponse) Close() error {
	r.done = true
	r.lease.Finish(r.status, r.headers, nil, false)
	return r.ReadCloser.Close()
}
func (r *codexSubscriptionResponse) observe(chunk []byte) {
	if r.failed {
		return
	}
	if r.mode == 0 {
		chunk = bytes.TrimLeft(chunk, " \r\n\t")
		if len(chunk) == 0 {
			return
		}
		r.mode = chunk[0]
	}
	if r.mode == '{' {
		if r.jsonBody.Len()+len(chunk) > codexSubscriptionObservationLimit {
			r.failed = true
			r.jsonBody.Reset()
			return
		}
		r.jsonBody.Write(chunk)
		return
	}
	for len(chunk) > 0 && !r.failed {
		end := bytes.IndexByte(chunk, '\n')
		part := chunk
		if end >= 0 {
			part = chunk[:end]
		}
		if r.line.Len()+r.data.Len()+len(part) > codexSubscriptionObservationLimit {
			r.failed = true
			return
		}
		r.line.Write(part)
		if end < 0 {
			return
		}
		r.observeLine(bytes.TrimSuffix(r.line.Bytes(), []byte{'\r'}))
		r.line.Reset()
		chunk = chunk[end+1:]
	}
}
func (r *codexSubscriptionResponse) observeLine(line []byte) {
	if len(line) == 0 {
		if r.data.Len() == 0 {
			return
		}
		data := bytes.TrimSpace(r.data.Bytes())
		if bytes.Equal(data, []byte("[DONE]")) {
			r.failed = !r.completed
			r.data.Reset()
			return
		}
		var frame struct {
			Type     string          `json:"type"`
			Error    json.RawMessage `json:"error"`
			Response struct {
				Status string          `json:"status"`
				Error  json.RawMessage `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &frame) != nil || frame.Type == "" || hasCodexSubscriptionError(frame.Error) || hasCodexSubscriptionError(frame.Response.Error) {
			r.failed = true
			errorBody := frame.Error
			if !hasCodexSubscriptionError(errorBody) {
				errorBody = frame.Response.Error
			}
			if hasCodexSubscriptionError(errorBody) {
				quotaBody, _ := json.Marshal(map[string]json.RawMessage{"error": errorBody})
				r.lease.Finish(r.status, r.headers, quotaBody, false)
			}
		} else {
			switch frame.Type {
			case "response.created":
				r.failed = r.started || r.completed
				r.started = true
			case "response.completed":
				r.failed = !r.started || r.completed || (frame.Response.Status != "" && frame.Response.Status != "completed")
				r.completed = true
			case "error", "response.failed", "response.incomplete":
				r.failed = true
			default:
				r.failed = !r.started || r.completed
			}
		}
		r.data.Reset()
		return
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		v := bytes.TrimPrefix(line[5:], []byte{' '})
		if r.data.Len()+len(v)+1 > codexSubscriptionObservationLimit {
			r.failed = true
			return
		}
		r.data.Write(v)
		r.data.WriteByte('\n')
	}
}
func hasCodexSubscriptionError(v json.RawMessage) bool {
	return len(v) > 0 && !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}
func (r *codexSubscriptionResponse) success() bool {
	if r.failed || r.status < 200 || r.status >= 300 {
		return false
	}
	if r.mode != '{' {
		return r.started && r.completed && r.line.Len() == 0 && r.data.Len() == 0
	}
	var p struct {
		Object string          `json:"object"`
		Type   string          `json:"type"`
		Status string          `json:"status"`
		Models json.RawMessage `json:"models"`
		Output json.RawMessage `json:"output"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(r.jsonBody.Bytes(), &p) != nil || hasCodexSubscriptionError(p.Error) {
		return false
	}
	if r.path == "/models" {
		var models []json.RawMessage
		return len(p.Models) > 0 && p.Models[0] == '[' && json.Unmarshal(p.Models, &models) == nil
	}
	if r.path == "/responses/compact" {
		return (p.Object == "response.compaction" || p.Type == "response.compaction") && len(p.Output) > 0 && p.Output[0] == '['
	}
	return p.Status == "completed"
}
