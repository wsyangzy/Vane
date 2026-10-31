package sessionusage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type observation struct {
	at       time.Time
	usage    Tokens
	versions map[Tokens]bool
}

type parser struct {
	provider, session, model, turn string
	project                        string
	title                          string
	titlePriority                  int
	lastEvent                      time.Time
	activity                       map[string]Activity
	prompts                        map[string]bool
	subagent                       bool
	calls                          map[string]Call
	claude                         map[string]*observation
	high                           *nativeTokens
	highAt                         time.Time
	epoch                          int
	checkpoints                    map[string]bool
	pending                        string
	pendingNamed                   bool
}

func newParser(provider, path string) *parser {
	return &parser{provider: provider, session: fmt.Sprintf("unidentified-%x", sha256.Sum256([]byte(path))), subagent: strings.Contains(strings.ReplaceAll(path, "\\", "/"), "/subagents/"), calls: make(map[string]Call), activity: make(map[string]Activity), prompts: make(map[string]bool), claude: make(map[string]*observation), checkpoints: make(map[string]bool)}
}

func (p *parser) observe(at time.Time, promptID string) {
	if at.IsZero() || p.subagent {
		return
	}
	item := Activity{Session: p.session, Project: p.project, Provider: p.provider, Model: p.model, At: at}
	// Match Magpie's active-work convention: count consecutive timestamp gaps
	// shorter than five minutes; a suspended or overnight session adds no idle time.
	if gap := at.Sub(p.lastEvent); !p.lastEvent.IsZero() && gap > 0 && gap < 5*time.Minute {
		item.Milliseconds = gap.Milliseconds()
	}
	if at.After(p.lastEvent) {
		p.lastEvent = at
	}
	if promptID != "" && !p.prompts[promptID] {
		p.prompts[promptID] = true
		item.Prompts = 1
	}
	// Keep one metadata bucket per minute/model/project instead of retaining
	// one entry for every tool/progress event in long client transcripts.
	keyData, _ := json.Marshal([]any{p.provider, p.session, p.project, p.model, at.Unix() / 60})
	key := string(keyData)
	if old, ok := p.activity[key]; ok {
		item.Milliseconds += old.Milliseconds
		item.Prompts += old.Prompts
		if old.At.After(item.At) {
			item.At = old.At
		}
	}
	if item.Milliseconds > 0 || item.Prompts > 0 {
		p.activity[key] = item
	}
}

func (p *parser) call(id string, at time.Time, tokens Tokens, model string) Call {
	return Call{ID: id, Provider: p.provider, Model: model, Session: p.session, Project: p.project, At: at, Tokens: tokens}
}

type nativeTokens struct {
	Input     int64 `json:"input_tokens"`
	Output    int64 `json:"output_tokens"`
	Cached    int64 `json:"cached_input_tokens"`
	Written   int64 `json:"cache_write_input_tokens"`
	Reasoning int64 `json:"reasoning_output_tokens"`
}

func (u nativeTokens) tokens() Tokens {
	return Tokens{Input: max(0, u.Input-u.Cached-u.Written), Output: max(0, u.Output), CacheRead: max(0, u.Cached), CacheWrite: max(0, u.Written)}
}

func (u nativeTokens) zero() bool {
	return u.Input == 0 && u.Output == 0 && u.Cached == 0 && u.Written == 0
}
func (u nativeTokens) less(v nativeTokens) nativeTokens {
	return nativeTokens{Input: max(0, u.Input-v.Input), Output: max(0, u.Output-v.Output), Cached: max(0, u.Cached-v.Cached), Written: max(0, u.Written-v.Written)}
}

// Typed decoding discards message bodies, tool arguments and credentials.
func (p *parser) read(line []byte) {
	if p.provider == "claude" {
		p.readClaude(line)
		return
	}
	p.readCodex(line)
}

func (p *parser) readClaude(line []byte) {
	var event struct {
		Type, Timestamp, SessionID, UUID, Cwd string
		AITitle, CustomTitle, Summary         string
		IsMeta, IsSidechain                   bool
		RequestID                             json.RawMessage
		Message                               struct {
			ID, Model string
			Usage     *struct {
				Input    int64 `json:"input_tokens"`
				Output   int64 `json:"output_tokens"`
				Read     int64 `json:"cache_read_input_tokens"`
				Written  int64 `json:"cache_creation_input_tokens"`
				Creation struct {
					Five int64 `json:"ephemeral_5m_input_tokens"`
					Hour int64 `json:"ephemeral_1h_input_tokens"`
				} `json:"cache_creation"`
			}
		}
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	switch event.Type {
	case "custom-title":
		p.name(event.CustomTitle, 4)
	case "ai-title":
		p.name(event.AITitle, 3)
	case "summary":
		p.name(event.Summary, 2)
	case "user":
		if !event.IsMeta && !event.IsSidechain {
			p.firstPrompt(line, "claude")
		}
	}
	at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		return
	}
	if event.SessionID != "" {
		p.session = event.SessionID
	}
	if event.Cwd != "" {
		p.project = normalizeProject(event.Cwd)
	}
	if event.Message.Model != "" {
		p.model = event.Message.Model
	}
	promptID := ""
	if event.Type == "user" {
		promptID = event.UUID
		if promptID == "" {
			promptID = event.Timestamp
		}
	}
	p.observe(at, promptID)
	if event.Type != "assistant" || event.Message.Usage == nil {
		return
	}
	identity := event.Message.ID
	var requestID string
	_ = json.Unmarshal(event.RequestID, &requestID)
	if identity == "" {
		identity = requestID
	}
	if identity == "" {
		identity = event.UUID
	}
	if identity == "" {
		return
	}
	base := "claude/" + p.session + "/" + identity + "/"
	key := base + requestID
	// An early block can omit requestId. Adopt it when the ID arrives.
	if requestID != "" && p.claude[key] == nil && p.claude[base] != nil {
		p.claude[key] = p.claude[base]
		delete(p.claude, base)
		if call, ok := p.calls[base]; ok {
			call.ID = key
			p.calls[key] = call
			delete(p.calls, base)
		}
	} else if requestID == "" {
		var known string
		for candidate := range p.claude {
			if strings.HasPrefix(candidate, base) {
				if known != "" {
					known = ""
					break
				}
				known = candidate
			}
		}
		if known != "" {
			key = known
		}
	}
	u := event.Message.Usage
	tokens := Tokens{Input: max(0, u.Input), Output: max(0, u.Output), CacheRead: max(0, u.Read), CacheWrite: max(0, u.Written)}
	if tokens.CacheWrite == 0 {
		tokens.CacheWrite = max(0, u.Creation.Five) + max(0, u.Creation.Hour)
	}
	tokens.CacheWrite1h = min(tokens.CacheWrite, max(0, u.Creation.Hour))
	old := p.claude[key]
	if old != nil {
		if tokens == old.usage || old.versions[tokens] || !at.After(old.at) {
			return
		}
		old.versions[old.usage] = true
	} else {
		old = &observation{versions: make(map[Tokens]bool)}
		p.claude[key] = old
	}
	old.at, old.usage = at, tokens
	if tokens.Total() > 0 {
		p.calls[key] = p.call(key, at, tokens, event.Message.Model)
	} else {
		delete(p.calls, key)
	}
}

func (p *parser) readCodex(line []byte) {
	var event struct {
		Type, Timestamp string
		Payload         struct {
			Type, ID, Model, Role, Cwd string
			SessionID                  string        `json:"session_id"`
			TurnID                     string        `json:"turn_id"`
			ResponseID                 string        `json:"response_id"`
			Usage                      *nativeTokens `json:"usage"`
			Info                       *struct {
				Total *nativeTokens `json:"total_token_usage"`
				Last  *nativeTokens `json:"last_token_usage"`
			} `json:"info"`
			Settings struct{ Model string } `json:"thread_settings"`
			Latest   *struct {
				ResponseID string        `json:"response_id"`
				Usage      *nativeTokens `json:"usage"`
			} `json:"latest_token_usage_record"`
		}
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	v := event.Payload
	if v.Type == "user_message" || event.Type == "response_item" && v.Role == "user" {
		p.firstPrompt(line, "codex")
	}
	if event.Type == "session_meta" {
		if v.ID != "" {
			p.session = v.ID
		}
		if v.Cwd != "" {
			p.project = normalizeProject(v.Cwd)
		}
		return
	}
	if event.Type == "turn_context" {
		if v.Model != "" {
			p.model = v.Model
		}
		if v.TurnID != p.turn {
			p.pending = ""
		}
		p.turn = v.TurnID
		if v.Cwd != "" {
			p.project = normalizeProject(v.Cwd)
		}
		return
	}
	if v.Type == "user_message" || event.Type == "response_item" && v.Role == "user" {
		p.pending = ""
	}
	if v.Type == "thread_settings_applied" && v.Settings.Model != "" {
		p.model = v.Settings.Model
	}
	at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		return
	}
	promptID := ""
	// event_msg/user_message is the native user turn; response_item is its
	// second representation and must not count the same prompt twice.
	if event.Type == "event_msg" && v.Type == "user_message" {
		promptID = event.Timestamp
	}
	p.observe(at, promptID)
	if event.Type == "compacted" && v.Latest != nil {
		v.ResponseID, v.Usage = v.Latest.ResponseID, v.Latest.Usage
	}
	if (event.Type == "token_usage_record" || event.Type == "compacted") && v.ResponseID != "" && v.Usage != nil {
		key := "codex/" + p.session + "/response/" + v.ResponseID
		if _, exists := p.calls[key]; exists {
			return
		}
		tokens := v.Usage.tokens()
		// RECORD and token_count are two observations of the same response.
		// Their cumulative counters can be in different compaction domains.
		if prior, ok := p.calls[p.pending]; ok && !p.pendingNamed && prior.Tokens == tokens {
			delete(p.calls, p.pending)
			p.pending = ""
		} else if event.Type != "compacted" {
			p.pending, p.pendingNamed = key, true
		}
		if tokens.Total() > 0 {
			p.calls[key] = p.call(key, at, tokens, p.model)
		}
		return
	}
	if event.Type != "event_msg" || v.Type != "token_count" || v.Info == nil || v.Info.Total == nil {
		return
	}
	total, last := *v.Info.Total, v.Info.Last
	fingerprint := fmt.Sprintf("%s/%s/%s/%v", p.session, p.turn, event.Timestamp, total)
	if p.checkpoints[fingerprint] {
		return
	}
	p.checkpoints[fingerprint] = true
	if p.high != nil && (total == *p.high || at.Before(p.highAt)) {
		return
	}
	if p.high != nil && (total.Input < p.high.Input || total.Output < p.high.Output || last != nil && total == *last && total != *p.high) {
		p.epoch++
	}
	var usage nativeTokens
	if last != nil {
		usage = *last
	} else if p.high != nil {
		usage = total.less(*p.high)
	} else {
		usage = total
	}
	p.high, p.highAt = &total, at
	tokens := usage.tokens()
	if prior, ok := p.calls[p.pending]; ok && p.pendingNamed && prior.Tokens == tokens {
		p.pending = ""
		return
	}
	if usage.zero() {
		return
	} // Fork history checkpoints often have last=0.
	key := fmt.Sprintf("codex/%s/count/%s/%d/%v", p.session, p.turn, p.epoch, total)
	p.calls[key] = p.call(key, at, tokens, p.model)
	p.pending, p.pendingNamed = key, false
}

func normalizeProject(value string) string {
	return strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/")
}
