package subscriptionprobe

import (
	"encoding/json"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

// Evidence deliberately has no free-form request/header/error fields. The
// observer receives only booleans, counts and fixed probe outcomes; neither
// OAuth nor client payloads may enter the T0 evidence stream.
type Evidence struct {
	ObservedAt       time.Time `json:"observed_at"`
	Path             string    `json:"path,omitempty"`
	Streaming        bool      `json:"streaming"`
	ToolResult       bool      `json:"tool_result"`
	OfficialAttempt  bool      `json:"official_attempt"`
	UpstreamStatus   int       `json:"upstream_status,omitempty"`
	ResponseComplete bool      `json:"response_complete"`
	ProtocolObserved bool      `json:"protocol_observed"`
	ProtocolSuccess  bool      `json:"protocol_success"`
	Outcome          string    `json:"outcome"`
}

type Option func(*Probe)

// WithEvidence must be supplied before serving. The observer can be called
// concurrently. It must not treat an HTTP 200 or an incomplete stream as
// confirmed protocol success, or infer official OAuth refresh from a 401.
func WithEvidence(observer func(Evidence)) Option {
	return func(p *Probe) { p.observer = observer }
}

// WithAttemptLimit bounds actual sends across all callers for this isolated
// run. Validation/local rejection consumes no sends. Zero keeps the existing
// unlimited behavior for mock-only users of New; the CLI requires a limit.
func WithAttemptLimit(limit uint64) Option {
	return func(p *Probe) { p.maxAttempts = limit }
}

func (p *Probe) admitAttempt() bool {
	if p.maxAttempts == 0 {
		return true
	}
	for {
		current := p.attempts.Load()
		if current >= p.maxAttempts {
			return false
		}
		if p.attempts.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func requestEvidence(body []byte, evidence *Evidence) {
	var request struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &request) != nil {
		return
	}
	evidence.Streaming = request.Stream
	for _, message := range request.Messages {
		var blocks []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type == "tool_result" {
				evidence.ToolResult = true
			}
		}
	}
}

func responseEvidence(body []byte, evidence *Evidence) {
	complete, failed := false, false
	routing.JSONFrames(body, func(frame map[string]any) {
		typ, _ := frame["type"].(string)
		if typ == "error" || frame["error"] != nil {
			failed = true
			evidence.ProtocolObserved = true
		}
		if typ == "message_stop" || typ == "message" && frame["stop_reason"] != nil {
			complete = true
			evidence.ProtocolObserved = true
		}
		if evidence.Path == "/v1/messages/count_tokens" {
			if tokens, ok := frame["input_tokens"].(float64); ok && tokens >= 0 {
				complete = true
				evidence.ProtocolObserved = true
			}
		}
	})
	evidence.ProtocolSuccess = complete && !failed && evidence.UpstreamStatus >= 200 && evidence.UpstreamStatus < 300
}
