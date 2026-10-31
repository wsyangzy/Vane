package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

func TestReloadableHandlerPreservesSlogAttributesAndGroups(t *testing.T) {
	var output bytes.Buffer
	handler := NewReloadableHandler(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := slogtest.TestHandler(handler, func() []map[string]any {
		var records []map[string]any
		for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
			var record map[string]any
			if err := json.Unmarshal(line, &record); err != nil {
				t.Fatalf("decode log record: %v", err)
			}
			records = append(records, record)
		}
		return records
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReloadableHandlerUpdateWaitsForActiveWrite(t *testing.T) {
	var previous, current bytes.Buffer
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	handler := NewReloadableHandler(&blockingLogHandler{
		Handler: slog.NewJSONHandler(&previous, nil),
		entered: entered,
		release: release,
	})
	logger := slog.New(handler)
	written := make(chan struct{})
	go func() {
		logger.Info("before-update")
		close(written)
	}()
	<-entered
	started, updated := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		handler.Update(slog.NewJSONHandler(&current, nil))
		close(updated)
	}()
	<-started
	select {
	case <-updated:
		t.Fatal("old output was retired before its active write completed")
	case <-time.After(20 * time.Millisecond):
	}
	unblock.Do(func() { close(release) })
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("output update did not finish after the old write completed")
	}
	<-written
	logger.Info("after-update")
	if !bytes.Contains(previous.Bytes(), []byte("before-update")) || bytes.Contains(previous.Bytes(), []byte("after-update")) || !bytes.Contains(current.Bytes(), []byte("after-update")) {
		t.Fatalf("logs did not follow the output switch: old=%s new=%s", previous.Bytes(), current.Bytes())
	}
}

type blockingLogHandler struct {
	slog.Handler
	entered chan struct{}
	release chan struct{}
}

func (h *blockingLogHandler) Handle(ctx context.Context, record slog.Record) error {
	close(h.entered)
	<-h.release
	return h.Handler.Handle(ctx, record)
}
