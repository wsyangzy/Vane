package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/response"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
)

func decodeSubscriptionResponse(resp *http.Response) error {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "gzip", "deflate", "br", "compress":
	default:
		return nil
	}
	raw := resp.Body
	decoded, err := response.NewProcessor().DecompressStreamReader(resp)
	if err != nil {
		return err
	}
	// The existing Brotli wrapper already owns its source; other decoders
	// close only decoder state, so retain the actual HTTP body closer.
	resp.Body = decoded
	if encoding != "br" {
		resp.Body = &subscriptionDecodedResponse{ReadCloser: decoded, source: raw}
	}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

type subscriptionDecodedResponse struct {
	io.ReadCloser
	source io.Closer
}

func (r *subscriptionDecodedResponse) Close() error {
	return errors.Join(r.ReadCloser.Close(), r.source.Close())
}

// Recovery observes each SSE event independently so long successful streams do
// not stay paused. The bound applies to one event or a non-stream JSON body;
// oversized or malformed observations remain unconfirmed without changing bytes.
const subscriptionObservationLimit = 1 << 20

type subscriptionResponse struct {
	io.ReadCloser
	lease       *subscription.Lease
	status      int
	headers     http.Header
	countTokens bool
	mode        byte
	jsonBody    bytes.Buffer
	line        bytes.Buffer
	data        bytes.Buffer
	started     bool
	stopped     bool
	failed      bool
	done        bool
}

func (r *subscriptionResponse) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if !r.done {
		r.observe(p[:n])
		if err != nil {
			r.done = true
			success := err == io.EOF && r.protocolSuccess()
			r.lease.Finish(r.status, r.headers, r.jsonBody.Bytes(), success)
			r.jsonBody.Reset()
			r.line.Reset()
			r.data.Reset()
		}
	}
	return n, err
}

func (r *subscriptionResponse) Close() error {
	r.done = true
	r.lease.Finish(r.status, r.headers, nil, false)
	return r.ReadCloser.Close()
}

func (r *subscriptionResponse) observe(chunk []byte) {
	if r.failed {
		return
	}
	if r.mode == 0 {
		chunk = bytes.TrimLeft(chunk, " \t\r\n")
		if len(chunk) == 0 {
			return
		}
		r.mode = chunk[0]
	}
	if r.mode == '{' {
		if r.jsonBody.Len()+len(chunk) > subscriptionObservationLimit {
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
		if r.line.Len()+len(part)+r.data.Len() > subscriptionObservationLimit {
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

func (r *subscriptionResponse) observeLine(line []byte) {
	if len(line) == 0 {
		if r.data.Len() == 0 {
			return
		}
		var frame struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(r.data.Bytes(), &frame) != nil || frame.Type == "" || frame.Type == "error" || hasSubscriptionError(frame.Error) {
			r.failed = true
			if hasSubscriptionError(frame.Error) {
				r.lease.Finish(r.status, r.headers, r.data.Bytes(), false)
			}
		} else {
			switch frame.Type {
			case "message_start":
				r.failed = r.started || r.stopped
				r.started = true
			case "message_stop":
				r.failed = !r.started || r.stopped
				r.stopped = true
			case "ping":
			default:
				r.failed = !r.started || r.stopped
			}
		}
		r.data.Reset()
		return
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		value := bytes.TrimPrefix(line[5:], []byte{' '})
		if r.data.Len()+len(value)+1 > subscriptionObservationLimit {
			r.failed = true
			return
		}
		r.data.Write(value)
		r.data.WriteByte('\n')
	}
}

func hasSubscriptionError(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (r *subscriptionResponse) protocolSuccess() bool {
	if r.failed || r.status < 200 || r.status >= 300 {
		return false
	}
	if r.mode != '{' {
		return !r.countTokens && r.started && r.stopped && r.line.Len() == 0 && r.data.Len() == 0
	}
	var message struct {
		Type        string          `json:"type"`
		StopReason  *string         `json:"stop_reason"`
		InputTokens *int64          `json:"input_tokens"`
		Error       json.RawMessage `json:"error"`
	}
	if json.Unmarshal(r.jsonBody.Bytes(), &message) != nil || hasSubscriptionError(message.Error) {
		return false
	}
	if r.countTokens {
		return message.InputTokens != nil && *message.InputTokens >= 0
	}
	return message.Type == "message" && message.StopReason != nil && *message.StopReason != ""
}
