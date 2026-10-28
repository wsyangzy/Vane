package privacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const maxRestoreBuffer = 8 << 20

type responseTail struct {
	text  string
	event []byte
	path  string
	inner bool
}
type restoringWriter struct {
	http.ResponseWriter
	r                                    *Restoration
	buffer                               []byte
	tails                                map[string]responseTail
	headerWritten, finished, passthrough bool
	err                                  error
}

// WrapResponse restores only response text and tool input strings. Usage, IDs,
// signatures and upstream accounting remain untouched. Finish must be deferred
// by the request owner, outside its retry loop.
func (result ApplyResult) WrapResponse(w http.ResponseWriter) (http.ResponseWriter, func() error) {
	if existing, ok := w.(*restoringWriter); ok && existing.r == result.Restoration {
		return w, func() error { return nil }
	}
	if result.Restoration == nil {
		return w, func() error { return nil }
	}
	rw := &restoringWriter{ResponseWriter: w, r: result.Restoration, tails: make(map[string]responseTail)}
	return rw, rw.Finish
}
func (w *restoringWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *restoringWriter) WriteHeader(status int) {
	if w.headerWritten {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.headerWritten = true
	w.Header().Del("Content-Length")
	if enc := w.Header().Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		w.passthrough = true
		w.skip("encoded_response")
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *restoringWriter) skip(reason string) { w.r.mu.Lock(); w.r.skipped = reason; w.r.mu.Unlock() }
func (w *restoringWriter) emit(p []byte) error {
	if w.err != nil {
		return w.err
	}
	n, err := w.ResponseWriter.Write(p)
	if err == nil && n != len(p) {
		err = fmt.Errorf("privacy response: short write")
	}
	w.err = err
	return err
}
func (w *restoringWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.passthrough {
		return w.ResponseWriter.Write(p)
	}
	w.buffer = append(w.buffer, p...)
	if strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		for {
			i, sep := eventBoundary(w.buffer)
			if i < 0 {
				break
			}
			event := append([]byte(nil), w.buffer[:i+sep]...)
			w.buffer = w.buffer[i+sep:]
			if err := w.event(event); err != nil {
				return 0, err
			}
		}
	} else if json.Valid(w.buffer) {
		// Most non-stream providers write the complete JSON in one call. Emit it
		// now so transport errors reach the caller before it records success.
		if err := w.emit(w.restoreJSON(w.buffer)); err != nil {
			return 0, err
		}
		w.buffer = nil
	}
	if len(w.buffer) > maxRestoreBuffer {
		if err := w.flushTails(""); err != nil {
			return 0, err
		}
		w.skip("response_too_large")
		w.passthrough = true
		if err := w.emit(w.buffer); err != nil {
			return 0, err
		}
		w.buffer = nil
	}
	return len(p), nil
}
func eventBoundary(b []byte) (int, int) {
	a, c := bytes.Index(b, []byte("\n\n")), bytes.Index(b, []byte("\r\n\r\n"))
	if c >= 0 && (a < 0 || c < a) {
		return c, 4
	}
	return a, 2
}
func (w *restoringWriter) Flush() {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func responseTextPath(path []pathElem) bool {
	if len(path) == 0 {
		return false
	}
	if isToolArgumentPath(path) {
		return true
	}
	for _, p := range path {
		if p.Index >= 0 {
			continue
		}
		switch p.Key {
		case "signature", "thinking", "encrypted_content", "data", "thought_signature", "thoughtSignature", "model", "usage", "metadata", "annotations", "citations", "reasoning_details":
			return false
		}
	}
	last := path[len(path)-1].Key
	switch last {
	case "text", "content", "output", "output_text", "arguments", "partial_json", "delta", "input":
		return true
	}
	return false
}
func innerArgumentPath(path string) bool {
	return strings.HasSuffix(path, ".arguments") || path == "arguments" || strings.HasSuffix(path, ".partial_json")
}
func (w *restoringWriter) restoreJSON(body []byte) []byte {
	segments, paths, err := collectJSONTextSegmentsWithPaths(body, responseTextPath)
	if err != nil {
		return body
	}
	replacements := map[int]string{}
	for i, seg := range segments {
		value, _ := w.r.restoreText(seg.Value, innerArgumentPath(paths[i]), true)
		if value != seg.Value {
			replacements[i] = value
		}
	}
	return replaceSegments(body, segments, replacements)
}

// SSE framing and non-data lines stay byte-identical, including heartbeats.
// Every stream keeps its own suffix; the final suffix is emitted before stop.
func (w *restoringWriter) event(event []byte) error {
	lines := bytes.SplitAfter(event, []byte("\n"))
	dataIndex := -1
	var data []byte
	for i, line := range lines {
		trim := bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(trim, []byte("data:")) {
			if dataIndex >= 0 {
				return w.emit(event)
			}
			dataIndex = i
			data = bytes.TrimPrefix(trim, []byte("data:"))
			data = bytes.TrimPrefix(data, []byte(" "))
		}
	}
	if dataIndex < 0 {
		return w.emit(event)
	}
	if string(data) == "[DONE]" {
		if err := w.flushTails(""); err != nil {
			return err
		}
		return w.emit(event)
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return w.emit(event)
	}
	var typ string
	_ = json.Unmarshal(root["type"], &typ)
	group := streamGroup(root)
	terminal := typ == "content_block_stop" || typ == "message_stop" || typ == "message_delta" || strings.HasSuffix(typ, ".done") || typ == "response.completed" || typ == "response.failed" || typ == "response.incomplete" || typ == "error"
	if terminal {
		prefix := group
		if typ == "message_stop" || typ == "message_delta" || strings.HasPrefix(typ, "response.") {
			prefix = ""
		}
		if err := w.flushTails(prefix); err != nil {
			return err
		}
	}
	segments, paths, err := collectJSONTextSegmentsWithPaths(data, responseTextPath)
	if err != nil {
		return w.emit(event)
	}
	replacements := map[int]string{}
	for i, seg := range segments {
		incremental := !terminal && (typ == "content_block_delta" || strings.HasSuffix(typ, ".delta") || strings.Contains(paths[i], ".delta."))
		if incremental && len(event) > 64<<10 {
			incremental = false
			w.skip("large_stream_event")
		}
		key := group + "|" + paths[i]
		text := seg.Value
		if incremental {
			text = w.tails[key].text + text
		}
		inner := innerArgumentPath(paths[i]) || strings.Contains(typ, "function_call_arguments") && paths[i] == "delta"
		value, tail := w.r.restoreText(text, inner, !incremental)
		if incremental {
			if len(w.tails) >= 128 && w.tails[key].text == "" {
				if err := w.flushTails(""); err != nil {
					return err
				}
			}
			if tail != "" {
				w.tails[key] = responseTail{text: tail, event: append([]byte(nil), event...), path: paths[i], inner: inner}
			} else {
				delete(w.tails, key)
			}
		}
		if value != seg.Value {
			replacements[i] = value
		}
	}
	if len(replacements) == 0 {
		return w.emit(event)
	}
	newData := replaceSegments(data, segments, replacements)
	line := lines[dataIndex]
	suffix := []byte("\n")
	if bytes.HasSuffix(line, []byte("\r\n")) {
		suffix = []byte("\r\n")
	}
	lines[dataIndex] = append(append([]byte("data: "), newData...), suffix...)
	return w.emit(bytes.Join(lines, nil))
}
func streamGroup(root map[string]json.RawMessage) string {
	var b strings.Builder
	for _, key := range []string{"index", "item_id", "output_index", "content_index"} {
		if raw := root[key]; len(raw) > 0 {
			b.WriteString(key)
			b.Write(raw)
			b.WriteByte('|')
		}
	}
	return b.String()
}
func (w *restoringWriter) flushTails(prefix string) error {
	keys := make([]string, 0, len(w.tails))
	for key := range w.tails {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		tail := w.tails[key]
		delete(w.tails, key)
		lines := bytes.SplitAfter(tail.event, []byte("\n"))
		for i, line := range lines {
			trim := bytes.TrimRight(line, "\r\n")
			if !bytes.HasPrefix(trim, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(trim, []byte("data:")))
			segs, paths, err := collectJSONTextSegmentsWithPaths(data, responseTextPath)
			if err != nil {
				continue
			}
			replacements := map[int]string{}
			for j := range segs {
				replacements[j] = ""
				if paths[j] == tail.path {
					replacements[j], _ = w.r.restoreText(tail.text, tail.inner, true)
				}
			}
			suffix := []byte("\n")
			if bytes.HasSuffix(line, []byte("\r\n")) {
				suffix = []byte("\r\n")
			}
			lines[i] = append(append([]byte("data: "), replaceSegments(data, segs, replacements)...), suffix...)
		}
		if err := w.emit(bytes.Join(lines, nil)); err != nil {
			return err
		}
	}
	return nil
}
func (w *restoringWriter) Finish() error {
	if w.finished {
		return w.err
	}
	w.finished = true
	if w.err == nil {
		if strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
			if len(w.buffer) > 0 {
				w.err = w.event(w.buffer)
			}
			if w.err == nil {
				w.err = w.flushTails("")
			}
		} else if len(w.buffer) > 0 {
			w.err = w.emit(w.restoreJSON(w.buffer))
		}
	}
	w.buffer = nil
	w.r.mu.Lock()
	count, skipped := w.r.restored, w.r.skipped
	w.r.mu.Unlock()
	w.r.activity.update(false, count, skipped)
	return w.err
}
