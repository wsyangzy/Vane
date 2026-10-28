package routing

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type CandidateMode string
type AffinityMode string

const (
	OrderedChannels   CandidateMode = "ordered_channels"
	UnifiedCandidates CandidateMode = "unified_candidates"
	AffinityTurn      AffinityMode  = "turn"
	AffinitySession   AffinityMode  = "session"
	AffinityAdaptive  AffinityMode  = "adaptive"
	AffinityNone      AffinityMode  = "none"
	StrategySmart                   = "smart"
	StrategyOrder                   = "order"
	StrategyRotate                  = "rotate"
	StrategyUsage                   = "usage"
	StrategyPace                    = "pace"
	StrategyWeighted                = "weighted"
)

// Durations use Go's nanosecond representation at the API boundary. The UI
// bridge must explicitly convert them to milliseconds; secrets never belong here.
type RoutingProfile struct {
	ID              int64             `json:"id"`
	Provider        string            `json:"provider"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	IsDefault       bool              `json:"is_default"`
	CandidateMode   CandidateMode     `json:"candidate_mode"`
	Strategy        string            `json:"strategy"`
	AffinityMode    AffinityMode      `json:"affinity_mode"`
	MaxAttempts     int               `json:"max_attempts"`
	MaxDuration     time.Duration     `json:"max_duration"`
	WaitForRecovery bool              `json:"wait_for_recovery"`
	Members         []RoutingMember   `json:"members"`
	Rules           []RoutingRule     `json:"rules"`
	Intents         []IntentRule      `json:"intents"`
	Classifier      *ClassifierConfig `json:"classifier,omitempty"`
	Revision        int64             `json:"revision"`
}

type RoutingMember struct {
	ProfileID int64 `json:"profile_id"`
	ChannelID int64 `json:"channel_id"`
	GroupID   int64 `json:"group_id"`
	Position  int   `json:"position"`
	Weight    int   `json:"weight"`
	Enabled   bool  `json:"enabled"`
	// Retained for stored-data compatibility; member orchestration no longer
	// overrides models. Only explicit rule/intent targets may specify one.
	UpstreamModel string `json:"upstream_model"`
}

type RouteTarget struct {
	ProfileID     int64  `json:"profile_id"`
	ChannelID     int64  `json:"channel_id"`
	GroupID       int64  `json:"group_id"`
	UpstreamModel string `json:"upstream_model"`
}

type RoutingRule struct {
	ID            int64         `json:"id"`
	ProfileID     int64         `json:"profile_id"`
	Enabled       bool          `json:"enabled"`
	Kind          string        `json:"kind"`
	Name          string        `json:"name"`
	Priority      int           `json:"priority"`
	MatchModel    string        `json:"match_model"`
	RequireStream *bool         `json:"require_stream,omitempty"`
	IntentID      string        `json:"intent_id"`
	Mode          string        `json:"mode"`
	Targets       []RouteTarget `json:"targets"`
	// nil uses the documented model=true, non_stream=false default.
	FallbackToDefault *bool `json:"fallback_to_default,omitempty"`
}

type IntentRule struct {
	ID                string        `json:"id"`
	ProfileID         int64         `json:"profile_id"`
	Name              string        `json:"name"`
	Description       string        `json:"description"`
	Enabled           bool          `json:"enabled"`
	Priority          int           `json:"priority"`
	Targets           []RouteTarget `json:"targets"`
	Mode              string        `json:"mode"`
	FallbackIntent    string        `json:"fallback_intent"`
	FallbackToDefault *bool         `json:"fallback_to_default,omitempty"`
}

type ClassifierConfig struct {
	Enabled     bool          `json:"enabled"`
	Model       string        `json:"model"`
	Provider    string        `json:"provider"`
	ChannelID   int64         `json:"channel_id"`
	GroupID     int64         `json:"group_id"`
	Timeout     time.Duration `json:"timeout"`
	Confidence  float64       `json:"confidence"`
	FailureMode string        `json:"failure_mode"`
}

type QuotaSnapshot struct {
	Provider      string     `json:"provider"`
	ChannelID     int64      `json:"channel_id"`
	GroupID       int64      `json:"group_id"`
	AccountIDHash string     `json:"account_id_hash"`
	ModelScope    string     `json:"model_scope"`
	WindowName    string     `json:"window_name"`
	UsedPercent   *float64   `json:"used_percent,omitempty"`
	Remaining     *float64   `json:"remaining,omitempty"`
	Limit         *float64   `json:"limit,omitempty"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	ObservedAt    time.Time  `json:"observed_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	Source        string     `json:"source"`
	Confidence    string     `json:"confidence"`
	Error         string     `json:"error,omitempty"`
}

type HealthState struct {
	RepairRequired bool      `json:"repair_required"`
	CoolingDown    bool      `json:"cooling_down"`
	Reason         string    `json:"reason"`
	Until          time.Time `json:"until"`
}
type LoadView struct {
	Concurrent      int     `json:"concurrent"`
	MaxConcurrent   int     `json:"max_concurrent"`
	DecayedTokens   float64 `json:"decayed_tokens"`
	DecayedRequests float64 `json:"decayed_requests"`
}
type QuotaView struct {
	Windows []QuotaSnapshot `json:"windows"`
	Learns  bool            `json:"learns"`
}
type DispatchCandidate struct {
	Balance        *ManualBalance `json:"balance,omitempty"`
	AccountIDHash  string         `json:"account_id_hash,omitempty"`
	Provider       string         `json:"provider"`
	ProfileID      int64          `json:"profile_id"`
	ChannelID      int64          `json:"channel_id"`
	GroupID        int64          `json:"group_id"`
	EndpointID     string         `json:"endpoint_id"`
	Model          string         `json:"model"`
	UpstreamModel  string         `json:"upstream_model"`
	CredentialMode string         `json:"credential_mode"`
	Protocol       string         `json:"protocol"`
	StateVersion   uint64         `json:"state_version"`
	Enabled        bool           `json:"enabled"`
	Position       int            `json:"position"`
	Weight         int            `json:"weight"`
	Health         HealthState    `json:"health"`
	Quota          QuotaView      `json:"quota"`
	Load           LoadView       `json:"load"`
}

func (c DispatchCandidate) Identity() Candidate {
	model := c.UpstreamModel
	if model == "" {
		model = c.Model
	}
	credential := fmt.Sprintf("%d/%d", c.ChannelID, c.GroupID)
	if c.AccountIDHash != "" {
		credential = "account:" + c.AccountIDHash
	}
	return Candidate{Provider: c.Provider, Protocol: c.Protocol, Channel: fmt.Sprint(c.ChannelID), Credential: credential, Endpoint: c.EndpointID, Model: model, StateVersion: c.StateVersion}
}

func DefaultProfile(provider string) RoutingProfile {
	return RoutingProfile{Provider: provider, Enabled: true, CandidateMode: OrderedChannels, Strategy: StrategySmart, AffinityMode: AffinityTurn, MaxAttempts: 10, MaxDuration: time.Minute, Members: []RoutingMember{}, Rules: []RoutingRule{}, Intents: []IntentRule{}}
}

func mappingTargetAllowed(p RoutingProfile, target RouteTarget) bool {
	if target.ProfileID != 0 || target.ChannelID <= 0 || strings.TrimSpace(target.UpstreamModel) == "" {
		return false
	}
	for _, member := range p.Members {
		if member.Enabled && member.ChannelID == target.ChannelID && (member.GroupID == 0 || member.GroupID == target.GroupID) {
			return true
		}
	}
	return false
}

// ValidateProfile normalizes defaults but never infers members from legacy pools.
func ValidateProfile(p *RoutingProfile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Provider != "claude" && p.Provider != "codex" {
		return errors.New("provider_invalid")
	}
	if p.Name == "" || len([]rune(p.Name)) > 120 {
		return errors.New("profile_name_invalid")
	}
	if p.CandidateMode == "" {
		p.CandidateMode = OrderedChannels
	}
	if p.CandidateMode != OrderedChannels && p.CandidateMode != UnifiedCandidates {
		return errors.New("candidate_mode_invalid")
	}
	if p.Strategy == "" {
		p.Strategy = StrategySmart
	}
	switch p.Strategy {
	case StrategySmart, StrategyOrder, StrategyRotate, StrategyUsage, StrategyPace, StrategyWeighted:
	default:
		return errors.New("strategy_invalid")
	}
	if p.AffinityMode == "" {
		p.AffinityMode = AffinityTurn
	}
	switch p.AffinityMode {
	case AffinityTurn, AffinitySession, AffinityAdaptive, AffinityNone:
	default:
		return errors.New("affinity_mode_invalid")
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 10
	}
	if p.MaxDuration == 0 {
		p.MaxDuration = time.Minute
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 100 || p.MaxDuration < time.Second || p.MaxDuration > 10*time.Minute {
		return errors.New("dispatch_budget_invalid")
	}
	if p.IsDefault && !p.Enabled {
		return errors.New("default_profile_disabled")
	}
	seen := map[[2]int64]bool{}
	for i := range p.Members {
		m := &p.Members[i]
		key := [2]int64{m.ChannelID, m.GroupID}
		if m.ChannelID <= 0 || m.GroupID < 0 || seen[key] || m.Weight < 0 || m.Weight > 10000 {
			return errors.New("profile_member_invalid")
		}
		seen[key] = true
		m.ProfileID = p.ID
		m.Position = i
		if m.Weight == 0 {
			m.Weight = 1
		}
		m.UpstreamModel = strings.TrimSpace(m.UpstreamModel)
	}
	intents := map[string]bool{}
	for i := range p.Intents {
		in := &p.Intents[i]
		in.ID = strings.TrimSpace(in.ID)
		if in.ID == "" || intents[in.ID] {
			return errors.New("intent_id_invalid")
		}
		intents[in.ID] = true
		in.ProfileID = p.ID
		if in.Mode == "" {
			in.Mode = ModePrefer
		}
	}
	validateTargets := func(ts []RouteTarget) error {
		for _, t := range ts {
			if (t.ProfileID > 0) == (t.ChannelID > 0) || t.GroupID < 0 || t.ProfileID < 0 || t.ChannelID < 0 {
				return errors.New("route_target_invalid")
			}
		}
		return nil
	}
	ruleIDs := map[int64]bool{}
	var maxRuleID int64
	for _, rule := range p.Rules {
		if rule.ID < 0 || rule.ID > 0 && ruleIDs[rule.ID] {
			return errors.New("rule_id_invalid")
		}
		if rule.ID > 0 {
			ruleIDs[rule.ID] = true
			maxRuleID = max(maxRuleID, rule.ID)
		}
	}
	for i := range p.Rules {
		r := &p.Rules[i]
		r.ProfileID = p.ID
		if r.ID == 0 {
			if maxRuleID == math.MaxInt64 {
				return errors.New("rule_id_exhausted")
			}
			maxRuleID++
			r.ID = maxRuleID
		}
		if r.Mode == "" {
			r.Mode = ModePrefer
		}
		if NormalizeMode(r.Mode) == "" {
			return errors.New("rule_mode_invalid")
		}
		switch r.Kind {
		case "model", "mapping", "composite":
			if strings.TrimSpace(r.MatchModel) == "" {
				return errors.New("rule_model_missing")
			}
		case "non_stream":
		case "intent":
			if !intents[r.IntentID] {
				return errors.New("rule_intent_missing")
			}
		default:
			return errors.New("rule_kind_invalid")
		}
		if err := validateTargets(r.Targets); err != nil {
			return err
		}
		if r.Kind == "mapping" && r.Enabled {
			if len(r.Targets) == 0 {
				return errors.New("mapping_target_invalid")
			}
			for _, target := range r.Targets {
				if !mappingTargetAllowed(*p, target) {
					return errors.New("mapping_target_invalid")
				}
			}
		}
	}
	for _, in := range p.Intents {
		if NormalizeMode(in.Mode) == "" {
			return errors.New("intent_mode_invalid")
		}
		if err := validateTargets(in.Targets); err != nil {
			return err
		}
		if in.FallbackIntent != "" && !intents[in.FallbackIntent] {
			return errors.New("fallback_intent_missing")
		}
		visited := map[string]bool{in.ID: true}
		next := in.FallbackIntent
		for next != "" {
			if visited[next] {
				return errors.New("fallback_intent_cycle")
			}
			visited[next] = true
			for _, other := range p.Intents {
				if other.ID == next {
					next = other.FallbackIntent
					break
				}
			}
		}
	}
	if c := p.Classifier; c != nil {
		if c.Timeout == 0 {
			c.Timeout = 8 * time.Second
		}
		if c.FailureMode == "" {
			c.FailureMode = "default"
		}
		if c.Timeout < time.Millisecond || c.Timeout > 30*time.Second || math.IsNaN(c.Confidence) || math.IsInf(c.Confidence, 0) || c.Confidence < 0 || c.Confidence > 1 {
			return errors.New("classifier_config_invalid")
		}
		switch c.FailureMode {
		case "previous", "default", "no_intent":
		default:
			return errors.New("classifier_failure_mode_invalid")
		}
		if c.Enabled && (c.Model == "" || c.ChannelID <= 0 || c.GroupID <= 0 || (c.Provider != "claude" && c.Provider != "codex")) {
			return errors.New("classifier_candidate_missing")
		}
	}
	return nil
}
