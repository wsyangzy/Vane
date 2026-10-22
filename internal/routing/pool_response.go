package routing

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type poolAttemptKey struct{}
type PoolAttempt struct {
	runtime         *PoolRuntime
	channel, group  int64
	endpoint, model string
	finish          func(bool)
	once            sync.Once
}

func AdmitPool(ctx context.Context, runtime *PoolRuntime, channel, group int64, endpoint, model string) (context.Context, *PoolAttempt, error) {
	if runtime == nil {
		return ctx, nil, nil
	}
	finish, err := runtime.Acquire(channel, group, endpoint, model)
	if err != nil {
		return ctx, nil, err
	}
	a := &PoolAttempt{runtime: runtime, channel: channel, group: group, endpoint: endpoint, model: model, finish: finish}
	return context.WithValue(ctx, poolAttemptKey{}, a), a, nil
}
func PoolAttemptFromContext(ctx context.Context) *PoolAttempt {
	a, _ := ctx.Value(poolAttemptKey{}).(*PoolAttempt)
	return a
}
func (a *PoolAttempt) Finish(success bool) {
	if a != nil {
		a.once.Do(func() { a.finish(success) })
	}
}
func (a *PoolAttempt) Fail(f Failure, retryAt time.Time) {
	if a != nil {
		a.once.Do(func() { a.finish(false); _ = a.runtime.Fail(a.channel, a.group, a.endpoint, a.model, f, retryAt) })
	}
}

func RetryAfter(header http.Header, now time.Time) time.Time {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= 86400*30 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at
	}
	return time.Time{}
}

// ObservePoolBody sees decoded protocol data. SSE is inspected frame by frame
// so long healthy streams do not need to be retained in memory.
func ObservePoolBody(body io.ReadCloser, attempt *PoolAttempt, stream bool, observe func([]byte) (bool, Failure), retryAt time.Time) io.ReadCloser {
	if attempt == nil {
		return body
	}
	return &poolBody{ReadCloser: body, attempt: attempt, stream: stream, observe: observe, retryAt: retryAt}
}

type poolBody struct {
	io.ReadCloser
	attempt  *PoolAttempt
	stream   bool
	observe  func([]byte) (bool, Failure)
	retryAt  time.Time
	buf      []byte
	success  bool
	failure  Failure
	overflow bool
}

func (b *poolBody) inspect(data []byte) {
	ok, f := b.observe(data)
	b.success = b.success || ok
	if f.Reason != "" {
		b.failure = f
	}
}
func (b *poolBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if !b.overflow {
		b.buf = append(b.buf, p[:n]...)
		if b.stream {
			for {
				i := bytes.IndexByte(b.buf, '\n')
				if i < 0 {
					break
				}
				b.inspect(b.buf[:i])
				b.buf = b.buf[i+1:]
			}
		}
		if len(b.buf) > 32<<20 {
			b.buf = nil
			b.overflow = true
		}
	}
	if err == io.EOF {
		if !b.overflow {
			b.inspect(b.buf)
		}
		if b.failure.Reason != "" {
			b.attempt.Fail(b.failure, b.retryAt)
		} else {
			b.attempt.Finish(b.success && !b.overflow)
		}
		b.buf = nil
	} else if err != nil {
		b.attempt.Finish(false)
	}
	return n, err
}
func (b *poolBody) Close() error { b.attempt.Finish(false); return b.ReadCloser.Close() }
