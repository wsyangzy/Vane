package response

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func zstdEncode(t *testing.T, data []byte) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	defer encoder.Close()
	return encoder.EncodeAll(data, nil)
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

// DeepSeek returns zstd when the client advertises it; the decoded message must
// reach token analysis instead of the raw frame bytes.
func TestDecompressStreamReaderDecodesZstdAndClosesSource(t *testing.T) {
	message := []byte(`{"type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":32,"output_tokens":29}}`)
	source := &closeRecorder{Reader: bytes.NewReader(zstdEncode(t, message))}
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"zstd"}}, Body: source}

	reader, err := NewProcessor().DecompressStreamReader(resp)
	if err != nil {
		t.Fatalf("DecompressStreamReader: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read zstd body: %v", err)
	}
	if !bytes.Equal(decoded, message) {
		t.Fatalf("decoded body mismatch:\n got %q\nwant %q", decoded, message)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !source.closed {
		t.Fatal("upstream body was not closed")
	}
}

func TestReadAndDecompressResponseDecodesZstd(t *testing.T) {
	message := []byte(`{"usage":{"input_tokens":1,"output_tokens":2}}`)
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"zstd"}}, Body: io.NopCloser(bytes.NewReader(zstdEncode(t, message)))}
	decoded, err := NewProcessor().ReadAndDecompressResponse(context.Background(), resp, "deepseek")
	if err != nil {
		t.Fatalf("ReadAndDecompressResponse: %v", err)
	}
	if !bytes.Equal(decoded, message) {
		t.Fatalf("decoded body mismatch: %q", decoded)
	}
}
