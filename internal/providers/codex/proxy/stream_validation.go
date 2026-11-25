package codexproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// codexSSEDecoder waits for a whole SSE event before interpreting JSON. In
// particular, event names and words inside output text are not protocol proof.
type codexSSEDecoder struct {
	event string
	data  []byte
	raw   []byte
	first bool
}

type codexSSEEvent struct {
	raw                         []byte
	data                        []byte
	frame                       map[string]any
	output                      bool
	completed                   bool
	err                         *UpstreamStreamError
	outputBytes, reasoningBytes int
}

func (d *codexSSEDecoder) line(line []byte) *codexSSEEvent {
	return d.wireLine(append(append([]byte(nil), line...), '\n'))
}

func (d *codexSSEDecoder) wireLine(line []byte) *codexSSEEvent {
	if len(d.raw)+len(line) > maxPreOutputStreamBufferBytes {
		e := &codexSSEEvent{raw: d.raw, err: invalidCodexStream("stream_event_too_large", "上游 SSE 事件超过缓冲上限")}
		d.event, d.raw, d.data = "", nil, nil
		return e
	}
	d.raw = append(d.raw, line...)
	if !d.first {
		d.first = true
		line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return d.end()
	}
	if bytes.HasPrefix(line, []byte(":")) {
		return nil
	}
	field, value, _ := bytes.Cut(line, []byte(":"))
	value = bytes.TrimPrefix(value, []byte(" "))
	switch string(field) {
	case "event":
		d.event = string(value)
	case "data":
		if len(d.data) > 0 {
			d.data = append(d.data, '\n')
		}
		d.data = append(d.data, value...)
	}
	return nil
}

// bufio.ScanLines discards CR/LF. Keep those bytes for original response
// passthrough when every candidate fails before client output.
func splitCodexSSEWireLine(data []byte, eof bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if eof && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (d *codexSSEDecoder) end() *codexSSEEvent {
	if len(d.raw) == 0 {
		return nil
	}
	e := parseCodexSSEEvent(d.event, d.data)
	e.raw = d.raw
	d.event, d.data, d.raw = "", nil, nil
	return e
}

func parseCodexSSEEvent(event string, data []byte) *codexSSEEvent {
	e := &codexSSEEvent{data: bytes.TrimSpace(data)}
	if len(e.data) == 0 {
		return e
	}
	if bytes.Equal(e.data, sseDoneMarker) {
		e.completed = true
		return e
	}
	if json.Unmarshal(e.data, &e.frame) != nil || e.frame == nil {
		e.err = invalidCodexStream("invalid_stream_event", "上游 SSE 事件不是有效 JSON")
		return e
	}
	typ, _ := e.frame["type"].(string)
	if typ == "" {
		typ = event
	}
	response := e.frame
	if nested, ok := e.frame["response"].(map[string]any); ok {
		response = nested
	}
	status, _ := response["status"].(string)
	if typ == "error" || typ == "response.failed" || typ == "response.incomplete" || e.frame["error"] != nil || response["error"] != nil || status == "failed" || status == "incomplete" {
		e.err = codexStreamError(e.data, typ, status)
		return e
	}
	e.completed = typ == "response.completed" || strings.HasPrefix(typ, "response.") && status == "completed"
	if e.completed {
		e.output = true
		return e
	}
	// These events carry generated text, reasoning, tool arguments or output
	// items. Unknown extensions remain buffered until known output/completion.
	if strings.HasPrefix(typ, "response.") {
		_, delta := e.frame["delta"]
		_, item := e.frame["item"].(map[string]any)
		_, part := e.frame["part"].(map[string]any)
		e.output = strings.HasSuffix(typ, ".delta") && delta || strings.HasPrefix(typ, "response.output_item.") && item || strings.HasPrefix(typ, "response.content_part.") && part
		text, _ := e.frame["delta"].(string)
		switch typ {
		case "response.output_text.delta", "response.refusal.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			e.outputBytes = len(text)
		case "response.reasoning_text.delta":
			e.reasoningBytes = len(text)
		}
	}
	// Keep the existing OpenAI-compatible Chat Completions SSE support. Both
	// consumers share [DONE] handling, including usage-only final chunks.
	if choices, ok := e.frame["choices"].([]any); ok {
		for _, choice := range choices {
			if c, ok := choice.(map[string]any); ok {
				fragment, delta := c["delta"].(map[string]any)
				_, message := c["message"].(map[string]any)
				text, textPresent := c["text"].(string)
				e.output = e.output || delta || message || textPresent
				e.outputBytes += len(text)
				if delta {
					content, _ := fragment["content"].(string)
					reasoning, _ := fragment["reasoning_content"].(string)
					e.outputBytes += len(content)
					e.reasoningBytes += len(reasoning)
					if calls, ok := fragment["tool_calls"].([]any); ok {
						for _, item := range calls {
							call, _ := item.(map[string]any)
							function, _ := call["function"].(map[string]any)
							arguments, _ := function["arguments"].(string)
							e.outputBytes += len(arguments)
						}
					}
				}
			}
		}
	}
	return e
}

func codexStreamError(data []byte, event, status string) *UpstreamStreamError {
	var envelope openAIStreamErrorEnvelope
	_ = json.Unmarshal(data, &envelope)
	detail := parseStreamErrorDetail(envelope.Error)
	if detail == nil && envelope.Response != nil {
		detail = parseStreamErrorDetail(envelope.Response.Error)
	}
	if detail == nil && event == "error" {
		// Responses error events put code/message on the event itself, while
		// compatible providers may wrap them in an error object.
		detail = parseStreamErrorDetail(data)
	}
	if detail == nil {
		detail = &openAIStreamErrorDetail{}
	}
	if event == "response.incomplete" || status == "incomplete" {
		return &UpstreamStreamError{Type: "response_incomplete", Code: detail.Code, Message: string(data)}
	}
	return &UpstreamStreamError{Type: firstNonEmpty(detail.Type, event, "server_error"), Code: detail.Code, Message: string(data)}
}

func invalidCodexStream(reason, message string) *UpstreamStreamError {
	return &UpstreamStreamError{Type: "invalid_stream_response", Code: reason, Message: message, InvalidProtocol: true}
}

func codexStreamPreludeError(raw []byte) *UpstreamStreamError {
	trimmed := bytes.TrimSpace(raw)
	var frame map[string]any
	if json.Unmarshal(trimmed, &frame) == nil && frame != nil {
		if event := parseCodexSSEEvent("", trimmed); event.err != nil {
			event.err.InvalidProtocol = true
			return event.err
		}
		return invalidCodexStream("non_stream_response", "流式请求收到普通 JSON 响应")
	}
	if len(trimmed) == 0 {
		return invalidCodexStream("empty_stream", "上游流式响应为空")
	}
	return invalidCodexStream("incomplete_stream", "上游未输出有效内容或流式结束事件")
}

// ValidateDiagnosticStream checks a bounded, already-read probe response using
// the same protocol rules as live forwarding. It never returns response text.
func ValidateDiagnosticStream(data []byte) (complete bool, failure string) {
	if len(data) > maxPreOutputStreamBufferBytes {
		return false, "stream_probe_too_large"
	}
	var decoder codexSSEDecoder
	accept := func(event *codexSSEEvent) string {
		if event == nil {
			return ""
		}
		if event.err != nil {
			return firstNonEmpty(event.err.Code, event.err.Type)
		}
		complete = complete || event.completed
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), maxPreOutputStreamBufferBytes)
	scanner.Split(splitCodexSSEWireLine)
	for scanner.Scan() {
		if reason := accept(decoder.wireLine(scanner.Bytes())); reason != "" {
			return false, reason
		}
	}
	if scanner.Err() != nil {
		return false, "stream_probe_too_large"
	}
	if reason := accept(decoder.end()); reason != "" {
		return false, reason
	}
	if !complete {
		failure := codexStreamPreludeError(data)
		return false, firstNonEmpty(failure.Code, failure.Type)
	}
	return true, ""
}
