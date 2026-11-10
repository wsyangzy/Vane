package handlers

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type zstdSourceBody struct {
	io.Reader
	closed bool
}

func (b *zstdSourceBody) Close() error {
	b.closed = true
	return nil
}

// Routed attempts decode before profile observation; an undecoded zstd body was
// judged response_incomplete and put the healthy candidate into cooldown.
func TestDecodeSubscriptionResponseDecodesZstd(t *testing.T) {
	message := []byte(`{"type":"message","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`)
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	source := &zstdSourceBody{Reader: bytes.NewReader(encoder.EncodeAll(message, nil))}
	_ = encoder.Close()
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"zstd"}, "Content-Length": {"99"}}, Body: source, ContentLength: 99}

	if err := decodeSubscriptionResponse(resp); err != nil {
		t.Fatalf("decodeSubscriptionResponse: %v", err)
	}
	if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != "" || resp.ContentLength != -1 {
		t.Fatalf("stale wire metadata retained: %v / %d", resp.Header, resp.ContentLength)
	}
	decoded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, message) {
		t.Fatalf("decoded body mismatch: %q", decoded)
	}
	if err := resp.Body.Close(); err != nil || !source.closed {
		t.Fatalf("upstream body not closed: err=%v closed=%v", err, source.closed)
	}
}
