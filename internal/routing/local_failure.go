package routing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxLocalFailures = 256

type localRequestKey struct{}
type localRequest struct{ key [32]byte }

// LocalRejection contains only local scheduling evidence, never an upstream
// response. Repeated requests refer to the first recorded failure.
type LocalRejection struct {
	Reason    string
	Detail    string
	RequestID string
	RetryAt   time.Time
	Repeated  bool
	Retryable bool
}

func (f *LocalRejection) Write(w http.ResponseWriter) {
	seconds := max(1, int(time.Until(f.RetryAt).Seconds()+0.999))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// A timed gate is a temporary service failure, not an invalid request.
	// Clients can retry it while the local gate still prevents upstream sends.
	status := http.StatusBadRequest
	if f.Retryable {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	message := f.Reason
	if f.Detail != "" && f.Detail != f.Reason {
		message += ": " + f.Detail
	}
	if f.Repeated {
		message += fmt.Sprintf("；相同请求冷却中，请在 %d 秒后重试（首次记录 %s）", seconds, f.RequestID)
	}
	_, _ = io.WriteString(w, message)
}

type localFailure struct {
	rejection   LocalRejection
	generation  uint64
	evaluations []CandidateEvaluation
}

func (r *ProfileRuntime) updateLocalFailureConfigLocked(config *RuntimeConfig) {
	// Periodic quota/health observations must not reset a failure window. Those
	// gates are rechecked on lookup; only routing configuration invalidates it.
	candidates := append([]DispatchCandidate(nil), config.Candidates...)
	for i := range candidates {
		candidates[i].Balance = nil
		candidates[i].Health = HealthState{}
		candidates[i].Quota = QuotaView{}
		candidates[i].Load = LoadView{MaxConcurrent: candidates[i].Load.MaxConcurrent}
	}
	identity, _ := json.Marshal([]any{config.Profiles, candidates, config.Catalog, config.Provider, config.RuleDefaults, configurationFailureDetail(config.LoadError)})
	resourceHash := sha256.Sum256(identity)
	config.generation = r.config.generation
	if resourceHash != r.resourceHash {
		config.generation++
		r.localFailures = nil
		r.resourceHash = resourceHash
	}
}

// Only recognizable database diagnostics cross into request history. Arbitrary
// store errors may contain values or credentials; those stay in application logs.
var schemaFailure = regexp.MustCompile(`(?i)\bno such (?:column|table): [a-z_][a-z0-9_.]*`)

func configurationFailureDetail(err error) string {
	if err == nil {
		return ""
	}
	if match := schemaFailure.FindString(err.Error()); match != "" {
		return "调度配置加载失败：" + match + "。应用版本与数据库结构不兼容，请使用与当前数据库匹配的版本。"
	}
	return "调度配置加载失败，请查看应用日志中的加载错误；修复配置后重试。"
}

// AdmitLocalRequest runs before tracking starts. Only a previously failed,
// identical request is blocked. The full body and caller identity are hashed
// together and are never retained or added to the trace/database.
func (r *ProfileRuntime) AdmitLocalRequest(request *http.Request, body []byte) (*http.Request, *LocalRejection) {
	if r == nil {
		return request, nil
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	identity := []string{CallerScope(request.Context()), host, request.Method, request.URL.RequestURI()}
	for _, name := range []string{"Authorization", "X-Api-Key", "ChatGPT-Account-ID", "OpenAI-Organization", "OpenAI-Project", "User-Agent", "Content-Type", "Anthropic-Version", "Anthropic-Beta", "OpenAI-Beta", "X-Session-Id", "X-Branch-Id", "X-Claude-Session-Id", "X-Codex-Session-Id", "Session_id", "Conversation_id", "X-Claude-Turn-Id", "X-Claude-Prompt-Id", "X-Codex-Turn-Id", "X-Codex-Prompt-Id", "X-Claude-User-Message-ID", "X-Codex-User-Message-ID"} {
		identity = append(identity, name, strings.Join(request.Header.Values(name), "\x00"))
	}
	// Normalize JSON formatting/key order without changing numbers or the
	// request bytes that will actually be forwarded.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) == nil {
		var extra any
		if decoder.Decode(&extra) == io.EOF {
			if canonical, err := json.Marshal(value); err == nil {
				body = canonical
			}
		}
	}
	hash := sha256.New()
	_ = json.NewEncoder(hash).Encode(identity)
	_, _ = hash.Write(body)
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	now := r.now()
	r.mu.Lock()
	config := r.config
	entry, exists := r.localFailures[key]
	if exists && (entry.generation != config.generation || !now.Before(entry.rejection.RetryAt)) {
		delete(r.localFailures, key)
		exists = false
	}
	r.mu.Unlock()
	if exists && !r.localFailureRecovered(config, entry, now) {
		r.mu.Lock()
		current := r.config.generation == entry.generation
		r.mu.Unlock()
		if current {
			rejection := entry.rejection
			rejection.Repeated = true
			return request, &rejection
		}
	}
	if exists {
		r.mu.Lock()
		if current, ok := r.localFailures[key]; ok && current.rejection.RequestID == entry.rejection.RequestID {
			delete(r.localFailures, key)
		}
		r.mu.Unlock()
	}
	return request.WithContext(context.WithValue(request.Context(), localRequestKey{}, localRequest{key: key})), nil
}

// Recheck dynamic health/admission without reserving candidates, advancing the
// scheduler, or sending a probe. A manual recovery can open the gate early.
func (r *ProfileRuntime) localFailureRecovered(config RuntimeConfig, entry localFailure, now time.Time) bool {
	for _, evaluation := range entry.evaluations {
		if evaluation.Reason == "candidate_duplicate" || strings.HasPrefix(evaluation.Reason, "state_") {
			continue
		}
		candidate, exists := recoveryResource(config, evaluation.Candidate)
		if !exists || config.Supports != nil && !config.Supports(candidate, candidate.Identity().Model) {
			continue
		}
		if candidateEligibility(candidate, r.scheduler.Load(candidate, now), applicableQuota(candidate, now), now) != "" {
			continue
		}
		if config.CheckAdmission == nil || config.CheckAdmission(candidate) == "" {
			return true
		}
	}
	return false
}

func (p *DispatchPlan) FailureDetail() string {
	if p.failureDetail != "" {
		return p.failureDetail
	}
	return p.Error
}

// RejectLocal remembers only terminal pre-send scheduling failures. A real
// upstream attempt, cancellation, or successful response is never deduplicated.
func (p *DispatchPlan) RejectLocal(reason string) *LocalRejection {
	if p == nil || p.Preview || p.state == nil {
		return nil
	}
	attempts, _ := p.Budget.Stats()
	if attempts != 0 || reason == "client_cancelled" {
		return nil
	}
	switch reason {
	case "routing_config_unavailable", "routing_profile_missing", "route_unavailable", "recovery_outside_budget":
	default:
		return nil
	}
	r := p.state.runtime
	now := r.now()
	config := r.snapshot()
	until := now.Add(config.CooldownPolicy.normalized().Initial)
	var recovery time.Time
	for _, evaluation := range p.Evaluations {
		candidate := evaluation.Candidate
		if !candidate.Enabled || candidate.Health.RepairRequired || evaluation.Reason == "candidate_duplicate" || strings.HasPrefix(evaluation.Reason, "state_") {
			continue
		}
		if blockage := evaluation.Blockage; blockage != nil && blockage.Recovery == "automatic" && blockage.RecoverAt != nil && blockage.RecoverAt.After(now) {
			if recovery.IsZero() || blockage.RecoverAt.Before(recovery) {
				recovery = *blockage.RecoverAt
			}
		}
		if at := candidate.Health.Until; candidate.Health.CoolingDown && (evaluation.Reason == "cooldown" || evaluation.Reason == "account_cooling_down") && at.After(now) && (recovery.IsZero() || at.Before(recovery)) {
			recovery = at
		}
	}
	if !recovery.IsZero() {
		until = recovery
	}
	retryable := !recovery.IsZero() && (reason == "route_unavailable" || reason == StopRecoveryOutsideBudget)
	rejection := LocalRejection{Reason: reason, Detail: p.FailureDetail(), RequestID: p.RequestID, RetryAt: until, Retryable: retryable}
	p.state.mu.Lock()
	if p.state.successful || p.state.responseStarted {
		p.state.mu.Unlock()
		return nil
	}
	ticket, generation := p.state.localRequest, p.state.generation
	p.state.mu.Unlock()
	if ticket == nil {
		return &rejection
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.config.generation {
		return &rejection
	}
	if r.localFailures == nil {
		r.localFailures = make(map[[32]byte]localFailure)
	}
	for key, entry := range r.localFailures {
		if !now.Before(entry.rejection.RetryAt) {
			delete(r.localFailures, key)
		}
	}
	if old, exists := r.localFailures[ticket.key]; exists && old.generation == generation && now.Before(old.rejection.RetryAt) {
		// Concurrent completions must not move the original deadline forward.
		return &rejection
	}
	if len(r.localFailures) >= maxLocalFailures {
		var oldest [32]byte
		var at time.Time
		for key, entry := range r.localFailures {
			if at.IsZero() || entry.rejection.RetryAt.Before(at) {
				oldest, at = key, entry.rejection.RetryAt
			}
		}
		delete(r.localFailures, oldest)
	}
	r.localFailures[ticket.key] = localFailure{rejection: rejection, generation: generation, evaluations: append([]CandidateEvaluation(nil), p.Evaluations...)}
	return &rejection
}
