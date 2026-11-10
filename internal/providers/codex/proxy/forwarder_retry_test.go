package codexproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryUnsentRoundTripperRetriesNewHTTP1Writes(t *testing.T) {
	var calls atomic.Int32
	var gotConn atomic.Int32
	rt := retryUnsentRoundTripper{next: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		trace := httptrace.ContextClientTrace(r.Context())
		if trace == nil {
			return nil, errors.New("missing client trace")
		}
		trace.GotConn(httptrace.GotConnInfo{Conn: retryTestConn{protocol: "http/1.1"}})
		gotConn.Add(1)
		return nil, &net.OpError{Op: "write", Err: errors.New("connection reset")}
	})}
	r, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", strings.NewReader("payload"))
	_, err := rt.RoundTrip(r)
	if err == nil || calls.Load() != int32(len(unsentRetryDelays)+1) {
		t.Fatalf("calls=%d got_conn=%d get_body=%t err=%v, want bounded retries and final error", calls.Load(), gotConn.Load(), r.GetBody != nil, err)
	}
}

func TestRetryUnsentRoundTripperDoesNotReplayUnsafeConnections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		conn  retryTestConn
		wrote bool
	}{
		{name: "reused connection", conn: retryTestConn{protocol: "http/1.1", reused: true}},
		{name: "request fully written", conn: retryTestConn{protocol: "http/1.1"}, wrote: true},
		{name: "http2 connection", conn: retryTestConn{protocol: "h2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			rt := retryUnsentRoundTripper{next: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				trace := httptrace.ContextClientTrace(r.Context())
				trace.GotConn(httptrace.GotConnInfo{Conn: tc.conn, Reused: tc.conn.reused})
				if tc.wrote {
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				return nil, io.EOF
			})}
			r, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", strings.NewReader("payload"))
			_, _ = rt.RoundTrip(r)
			if got := calls.Load(); got != 1 {
				t.Fatalf("calls=%d, want 1", got)
			}
		})
	}
}

func TestRetryUnsentRoundTripperHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := retryUnsentRoundTripper{next: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		trace := httptrace.ContextClientTrace(r.Context())
		if trace == nil {
			return nil, errors.New("missing client trace")
		}
		trace.GotConn(httptrace.GotConnInfo{Conn: retryTestConn{protocol: "http/1.1"}})
		cancel()
		return nil, &net.OpError{Op: "write", Err: errors.New("connection reset")}
	})}
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/v1/responses", strings.NewReader("payload"))
	if _, err := rt.RoundTrip(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want cancellation", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type retryTestConn struct {
	protocol string
	reused   bool
}

func (c retryTestConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c retryTestConn) Write([]byte) (int, error)        { return 0, io.EOF }
func (c retryTestConn) Close() error                     { return nil }
func (c retryTestConn) LocalAddr() net.Addr              { return retryTestAddr("local") }
func (c retryTestConn) RemoteAddr() net.Addr             { return retryTestAddr("remote") }
func (c retryTestConn) SetDeadline(time.Time) error      { return nil }
func (c retryTestConn) SetReadDeadline(time.Time) error  { return nil }
func (c retryTestConn) SetWriteDeadline(time.Time) error { return nil }
func (c retryTestConn) ConnectionState() tls.ConnectionState {
	return tls.ConnectionState{NegotiatedProtocol: c.protocol}
}

type retryTestAddr string

func (a retryTestAddr) Network() string { return "test" }
func (a retryTestAddr) String() string  { return string(a) }
