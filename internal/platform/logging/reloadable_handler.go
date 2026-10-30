package logging

import (
	"context"
	"log/slog"
	"sync"
)

type reloadableHandlerState struct {
	mu      sync.RWMutex
	handler slog.Handler
}

type handlerBinding struct {
	attrs []slog.Attr
	group string
}

// ReloadableHandler keeps existing loggers and their attributes attached to the
// current output. Update waits for writes to finish before retiring an output.
type ReloadableHandler struct {
	state    *reloadableHandlerState
	bindings []handlerBinding
}

func NewReloadableHandler(handler slog.Handler) *ReloadableHandler {
	return &ReloadableHandler{state: &reloadableHandlerState{handler: handler}}
}

func (h *ReloadableHandler) Update(handler slog.Handler) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.handler = handler
}

func (h *ReloadableHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()
	return h.state.handler.Enabled(ctx, level)
}

func (h *ReloadableHandler) Handle(ctx context.Context, record slog.Record) error {
	h.state.mu.RLock()
	defer h.state.mu.RUnlock()
	if len(h.bindings) == 0 {
		return h.state.handler.Handle(ctx, record)
	}
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	// Materialize inherited attributes in the record so routing, file output,
	// history and live events all observe the same fields.
	for i := len(h.bindings) - 1; i >= 0; i-- {
		binding := h.bindings[i]
		if binding.group != "" {
			attrs = []slog.Attr{{Key: binding.group, Value: slog.GroupValue(attrs...)}}
		} else {
			combined := make([]slog.Attr, 0, len(binding.attrs)+len(attrs))
			combined = append(combined, binding.attrs...)
			attrs = append(combined, attrs...)
		}
	}
	combined := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	combined.AddAttrs(attrs...)
	return h.state.handler.Handle(ctx, combined)
}

func (h *ReloadableHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withBinding(handlerBinding{attrs: append([]slog.Attr(nil), attrs...)})
}

func (h *ReloadableHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withBinding(handlerBinding{group: name})
}

func (h *ReloadableHandler) withBinding(binding handlerBinding) *ReloadableHandler {
	bindings := make([]handlerBinding, len(h.bindings), len(h.bindings)+1)
	copy(bindings, h.bindings)
	return &ReloadableHandler{state: h.state, bindings: append(bindings, binding)}
}
