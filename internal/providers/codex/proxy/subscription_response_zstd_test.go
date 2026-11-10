package codexproxy

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type codexZstdSource struct {
	io.Reader
	closed bool
}

func (s *codexZstdSource) Close() error {
	s.closed = true
	return nil
}

// zstd previously failed with unsupported_subscription_encoding, rejecting a
// successful upstream response for clients that advertise zstd.
func TestDecodeCodexSubscriptionResponseDecodesZstd(t *testing.T) {
	body := []byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	source := &codexZstdSource{Reader: bytes.NewReader(encoder.EncodeAll(body, nil))}
	_ = encoder.Close()
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"zstd"}}, Body: source}

	if err := decodeCodexSubscriptionResponse(resp); err != nil {
		t.Fatalf("decodeCodexSubscriptionResponse: %v", err)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("Content-Encoding retained after decoding")
	}
	decoded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, body) {
		t.Fatalf("decoded body mismatch: %q", decoded)
	}
	if err := resp.Body.Close(); err != nil || !source.closed {
		t.Fatalf("upstream body not closed: err=%v closed=%v", err, source.closed)
	}
}
