package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RuntimeConfig struct {
	Provider         string
	Profiles         []RoutingProfile
	Candidates       []DispatchCandidate
	Classify         ClassifierCall
	PrepareCandidate func(DispatchCandidate, string, string) DispatchCandidate
	Supports         func(DispatchCandidate, string) bool
	Compatible       func(context.Context, DispatchCandidate) (bool, string)
	OnTrace          func(DispatchTrace)
	CheckAdmission   func(DispatchCandidate) string
	OnSettlement     func(AttemptSettlement)
}

type IntentRoute struct {
	IntentID          string        `json:"intent_id"`
	Mode              string        `json:"mode"`
	Targets           []RouteTarget `json:"targets"`
	FallbackToDefault bool          `json:"fallback_to_default"`
}

type DispatchDecision struct {
	Kind              string        `json:"kind"`
	RuleID            int64         `json:"rule_id,omitempty"`
	IntentID          string        `json:"intent_id,omitempty"`
	Mode              string        `json:"mode,omitempty"`
	Targets           []RouteTarget `json:"targets,omitempty"`
	FallbackToDefault bool          `json:"fallback_to_default"`
	FallbackIntents   []IntentRoute `json:"fallback_intents,omitempty"`
	ReusedTurn        bool          `json:"reused_turn,omitempty"`
}

type DispatchPlanStage struct {
	Index              int                   `json:"index"`
	Kind               string                `json:"kind"`
	Evaluations        []CandidateEvaluation `json:"evaluations"`
	EligibleCandidates []DispatchCandidate   `json:"eligible_candidates"`
}

type DispatchTraceEvent struct {
	Candidate Candidate    `json:"candidate"`
	Action    string       `json:"action"`
	Reason    string       `json:"reason,omitempty"`
	Scope     FailureScope `json:"failure_scope,omitempty"`
}

// DispatchTrace is the persistence boundary. It deliberately has no profile
// descriptions, input, caller/session identifiers, quota error text or secrets.
type DispatchTrace struct {
	RequestID       string               `json:"request_id"`
	Provider        string               `json:"provider"`
	ProfileID       int64                `json:"profile_id"`
	ProfileName     string               `json:"profile_name"`
	Strategy        string               `json:"strategy"`
	AffinityMode    AffinityMode         `json:"affinity_mode"`
	Decision        DispatchDecision     `json:"decision"`
	Intent          IntentResult         `json:"intent"`
	Candidates      []DispatchTraceEvent `json:"candidates"`
	Attempts        int                  `json:"attempts"`
	WaitMS          int64                `json:"wait_ms"`
	ResponseStarted bool                 `json:"response_started"`
	Successful      bool                 `json:"successful"`
	StopReason      string               `json:"stop_reason,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	FinishedAt      *time.Time           `json:"finished_at,omitempty"`
}

type DispatchPlan struct {
	RequestID          string                `json:"request_id"`
	Profile            RoutingProfile        `json:"profile"`
	Decision           DispatchDecision      `json:"decision"`
	Intent             IntentResult          `json:"intent"`
	Model              string                `json:"model"`
	Stages             []DispatchPlanStage   `json:"stages"`
	Evaluations        []CandidateEvaluation `json:"evaluations"`
	EligibleCandidates []DispatchCandidate   `json:"eligible_candidates"`
	Error              string                `json:"error,omitempty"`
	Preview            bool                  `json:"preview"`
	Turn               TurnInput             `json:"-"`
	Budget             *Budget               `json:"-"`
	state              *dispatchRequestState
}

type dispatchRequestState struct {
	mu              sync.Mutex
	runtime         *ProfileRuntime
	ledger          Ledger
	reserved        bool
	selected        bool
	responseStarted bool
	successful      bool
	stopped         string
	created         time.Time
	finished        *time.Time
	events          []DispatchTraceEvent
	stateRejected   map[Candidate]string
	turnKey         string
	sessionKey      string
	scopeHash       string
	previous        *Candidate
	publish         func(DispatchTrace)
}

type profileAffinity struct {
	session                bool
	profileID              int64
	revision               int64
	candidate              Candidate
	decision               DispatchDecision
	intent                 string
	model                  string
	cachedTokens           int64
	failures               int
	expires, used, success time.Time
}

type ProfileRuntime struct {
	mu              sync.Mutex
	config          RuntimeConfig
	healthEndpoints map[string]struct{}
	scheduler       *Scheduler
	classifier      *IntentClassifier
	affinity        map[string]profileAffinity
	now             func() time.Time
}

var dispatchRequestSequence atomic.Uint64

func NewProfileRuntime(config RuntimeConfig) *ProfileRuntime {
	r := &ProfileRuntime{scheduler: NewScheduler(), classifier: NewIntentClassifier(), affinity: map[string]profileAffinity{}, now: time.Now}
	r.Replace(config)
	return r
}

// Replace atomically replaces immutable resource snapshots. Scheduler state and
// existing turn ownership survive; every new plan rechecks current resources.
func (r *ProfileRuntime) Replace(config RuntimeConfig) {
	type resources struct {
		Profiles   []RoutingProfile
		Candidates []DispatchCandidate
	}
	data, _ := json.Marshal(resources{config.Profiles, config.Candidates})
	var copy resources
	_ = json.Unmarshal(data, &copy)
	config.Profiles, config.Candidates = copy.Profiles, copy.Candidates
	healthEndpoints := make(map[string]struct{})
	if profile, ok := runtimeProfile(config, 0); ok {
		for _, candidate := range config.Candidates {
			if !candidate.Enabled || candidate.EndpointID == "" || candidate.Provider != profile.Provider {
				continue
			}
			for _, member := range profile.Members {
				if member.Enabled && member.ChannelID == candidate.ChannelID && (member.GroupID == 0 || member.GroupID == candidate.GroupID) {
					healthEndpoints[candidate.EndpointID] = struct{}{}
					break
				}
			}
		}
	}
	r.mu.Lock()
	r.config = config
	r.healthEndpoints = healthEndpoints
	r.mu.Unlock()
}

// HealthCheckEnabled limits automatic probes to enabled members of the current
// default profile. Health and cooldown state must not prevent recovery probes.
// A missing, disabled or ambiguous default profile enables no automatic probes.
func (r *ProfileRuntime) HealthCheckEnabled(endpointID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.healthEndpoints[endpointID]
	return ok
}

func (r *ProfileRuntime) snapshot() RuntimeConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config
}

func runtimeProfile(config RuntimeConfig, id int64) (RoutingProfile, bool) {
	var selected RoutingProfile
	found := false
	for _, p := range config.Profiles {
		if config.Provider != "" && p.Provider != config.Provider {
			continue
		}
		if id > 0 && p.ID == id {
			return p, p.Enabled
		}
		if id == 0 && p.Enabled && p.IsDefault {
			if found {
				return RoutingProfile{}, false
			}
			selected, found = p, true
		}
	}
	return selected, found
}

func (r *ProfileRuntime) Profile(id int64) (RoutingProfile, bool) {
	return runtimeProfile(r.snapshot(), id)
}

func boolDefault(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

func profileRuleTier(rule RoutingRule) int {
	switch rule.Kind {
	case "model", "mapping":
		if rule.RequireStream != nil || rule.IntentID != "" {
			return 2
		}
		if !strings.Contains(rule.MatchModel, "*") {
			return 0
		}
		return 1
	case "composite":
		return 2
	case "non_stream":
		return 3
	case "intent":
		return 4
	default:
		return 9
	}
}

// ResolveDecision is pure. Target profile references are expanded later and
// never cause their own rules or classifiers to run recursively.
func ResolveDecision(p RoutingProfile, model string, streaming bool, intentID string) DispatchDecision {
	if p.Classifier == nil || !p.Classifier.Enabled {
		intentID = ""
	}
	matched := []RoutingRule{}
	for _, rule := range p.Rules {
		if !rule.Enabled || len(rule.Targets) == 0 || rule.RequireStream != nil && *rule.RequireStream != streaming || rule.IntentID != "" && rule.IntentID != intentID {
			continue
		}
		switch rule.Kind {
		case "model", "mapping", "composite":
			if !MatchModel(rule.MatchModel, model) {
				continue
			}
		case "non_stream":
			if streaming {
				continue
			}
		case "intent":
			if intentID == "" || rule.IntentID != intentID {
				continue
			}
		default:
			continue
		}
		matched = append(matched, rule)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if profileRuleTier(a) != profileRuleTier(b) {
			return profileRuleTier(a) < profileRuleTier(b)
		}
		return RuleLess(&ModelRule{ID: a.ID, Priority: a.Priority, MatchModel: a.MatchModel}, &ModelRule{ID: b.ID, Priority: b.Priority, MatchModel: b.MatchModel})
	})
	if len(matched) > 0 {
		rule := matched[0]
		mode := NormalizeMode(rule.Mode)
		if mode == "" {
			mode = ModePrefer
		}
		return DispatchDecision{Kind: rule.Kind, RuleID: rule.ID, IntentID: intentID, Mode: mode, Targets: rule.Targets, FallbackToDefault: boolDefault(rule.FallbackToDefault, rule.Kind != "non_stream" && rule.Kind != "mapping")}
	}
	for _, in := range p.Intents {
		if !in.Enabled || in.ID != intentID {
			continue
		}
		decision := DispatchDecision{Kind: "intent", IntentID: in.ID, Mode: in.Mode, Targets: in.Targets, FallbackToDefault: boolDefault(in.FallbackToDefault, true)}
		if decision.Mode == "" {
			decision.Mode = ModePrefer
		}
		seen := map[string]bool{in.ID: true}
		next := in.FallbackIntent
		for next != "" && !seen[next] {
			seen[next] = true
			found := false
			for _, fallback := range p.Intents {
				if fallback.ID == next && fallback.Enabled {
					mode := fallback.Mode
					if mode == "" {
						mode = ModePrefer
					}
					decision.FallbackIntents = append(decision.FallbackIntents, IntentRoute{fallback.ID, mode, fallback.Targets, boolDefault(fallback.FallbackToDefault, true)})
					next, found = fallback.FallbackIntent, true
					break
				}
			}
			if !found {
				break
			}
		}
		return decision
	}
	return DispatchDecision{Kind: "default"}
}

func CopyCandidateForModel(c DispatchCandidate, requestModel, override string) DispatchCandidate {
	c.Model = requestModel
	if override != "" {
		c.UpstreamModel = override
	}
	return c
}

func expandProfileTargets(config RuntimeConfig, source RoutingProfile, targets []RouteTarget, model string) []DispatchCandidate {
	result := []DispatchCandidate{}
	prepare := config.PrepareCandidate
	if prepare == nil {
		prepare = CopyCandidateForModel
	}
	appendMember := func(member RoutingMember, override string) {
		if !member.Enabled {
			return
		}
		for _, resource := range config.Candidates {
			if resource.Provider != source.Provider || resource.ChannelID != member.ChannelID || member.GroupID != 0 && member.GroupID != resource.GroupID {
				continue
			}
			candidate := prepare(resource, model, override)
			candidate.ProfileID = source.ID
			candidate.Position = len(result)
			candidate.Weight = max(member.Weight, 1)
			result = append(result, candidate)
		}
	}
	for _, target := range targets {
		if target.ProfileID > 0 {
			for _, p := range config.Profiles {
				if p.ID == target.ProfileID && p.Provider == source.Provider && p.Enabled {
					for _, member := range p.Members {
						appendMember(member, target.UpstreamModel)
					}
					break
				}
			}
		} else if target.ChannelID > 0 {
			appendMember(RoutingMember{ChannelID: target.ChannelID, GroupID: target.GroupID, Weight: 1, Enabled: true}, target.UpstreamModel)
		}
	}
	return result
}

func (r *ProfileRuntime) ExpandTargets(p RoutingProfile, targets []RouteTarget, model string) []DispatchCandidate {
	return expandProfileTargets(r.snapshot(), p, targets, model)
}

// PreviewTargets evaluates explicit resources using business routing eligibility
// and ordering, without running rules, classification, affinity or reservations.
func (r *ProfileRuntime) PreviewTargets(ctx context.Context, p RoutingProfile, targets []RouteTarget, model string) []CandidateEvaluation {
	plan := DispatchPlan{
		Profile: p,
		Model:   model,
		Decision: DispatchDecision{
			Kind:    "explicit",
			Mode:    ModePrefer,
			Targets: targets,
		},
		Preview: true,
		state:   &dispatchRequestState{},
	}
	r.populateDispatchPlan(ctx, r.snapshot(), &plan, nil, r.now())
	return plan.Evaluations
}

func (r *ProfileRuntime) CandidateLoad(candidate DispatchCandidate) LoadView {
	return r.scheduler.Load(candidate, r.now())
}

type profileTargetStage struct {
	kind    string
	targets []RouteTarget
}

func decisionTargetStages(p RoutingProfile, d DispatchDecision) []profileTargetStage {
	stages := []profileTargetStage{}
	defaultAdded := false
	addDefault := func() {
		if !defaultAdded {
			stages = append(stages, profileTargetStage{"default", []RouteTarget{{ProfileID: p.ID}}})
			defaultAdded = true
		}
	}
	if d.Kind == "default" {
		addDefault()
		return stages
	}
	if d.Mode == ModeFallback {
		addDefault()
	}
	stages = append(stages, profileTargetStage{d.Kind, d.Targets})
	allowDefault := d.FallbackToDefault
	for _, fallback := range d.FallbackIntents {
		if fallback.Mode == ModeFallback {
			addDefault()
		}
		stages = append(stages, profileTargetStage{"intent_fallback", fallback.Targets})
		allowDefault = allowDefault || fallback.FallbackToDefault
	}
	if allowDefault {
		addDefault()
	}
	return stages
}

func affinityKeys(ctx context.Context, p RoutingProfile, t TurnInput) (string, string) {
	base := []string{CallerScope(ctx), p.Provider, fmt.Sprint(p.ID), t.ConversationID, t.BranchID}
	turn, session := "", ""
	if t.TurnID != "" {
		turn = routingHash(append(base, "turn", t.TurnID)...)
	}
	if t.ConversationID != "" {
		session = routingHash(append(base, "session")...)
	}
	return turn, session
}

func (r *ProfileRuntime) remembered(key string, now time.Time, preview bool) (profileAffinity, bool) {
	if key == "" {
		return profileAffinity{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.affinity[key]
	if !exists || !now.Before(entry.expires) {
		return profileAffinity{}, false
	}
	if !preview {
		entry.used = now
		r.affinity[key] = entry
	}
	return entry, true
}

func (r *ProfileRuntime) Plan(ctx context.Context, profileID int64, model string, streaming bool, turn TurnInput, preview bool) DispatchPlan {
	config := r.snapshot()
	now := r.now()
	p, exists := runtimeProfile(config, profileID)
	plan := DispatchPlan{RequestID: fmt.Sprintf("dispatch-%d-%d", now.UnixNano(), dispatchRequestSequence.Add(1)), Profile: p, Model: model, Turn: turn, Preview: preview, Stages: []DispatchPlanStage{}, Evaluations: []CandidateEvaluation{}, EligibleCandidates: []DispatchCandidate{}}
	plan.state = &dispatchRequestState{runtime: r, created: now, publish: config.OnTrace}
	if !exists {
		plan.Error = "routing_profile_missing"
		return plan
	}
	if !preview {
		plan.Budget = BudgetFromContext(ctx)
		if plan.Budget == nil {
			attempts, duration := p.MaxAttempts, p.MaxDuration
			if attempts <= 0 {
				attempts = 10
			}
			if duration <= 0 {
				duration = time.Minute
			}
			plan.Budget = NewBudget(min(attempts, 100), min(duration, 10*time.Minute))
		}
		plan.Budget.Limit(p.MaxAttempts, p.MaxDuration)
	}
	turnKey, sessionKey := affinityKeys(ctx, p, turn)
	plan.state.turnKey, plan.state.sessionKey = turnKey, sessionKey
	plan.state.scopeHash = routingHash(CallerScope(ctx), p.Provider, fmt.Sprint(p.ID))
	sameTurn, hasTurn := r.remembered(turnKey, now, preview)
	fromState := false
	if !hasTurn && !turn.NewTurn {
		for _, key := range profileStateKeysFromContext(ctx) {
			if entry, exists := r.remembered(routingHash(plan.state.scopeHash, "state", key), now, preview); exists {
				sameTurn, hasTurn, fromState = entry, true, true
				break
			}
		}
	}
	previous, hasSession := r.remembered(sessionKey, now, preview)
	// A saved policy takes effect on the next request. Keep the successful
	// resource hint, but do not replay a decision made by an older revision.
	if hasTurn && sameTurn.revision != p.Revision {
		hasTurn = false
	}
	// Turning off intent routing also disables previously remembered intent
	// decisions; the upstream remains responsible for protocol-state validation.
	if hasTurn && (p.Classifier == nil || !p.Classifier.Enabled) && (sameTurn.intent != "" || sameTurn.decision.IntentID != "" || sameTurn.decision.Kind == "intent") {
		hasTurn = false
	}
	var preferred *Candidate
	if hasTurn && (p.AffinityMode != AffinityNone || fromState) {
		plan.Decision = sameTurn.decision
		plan.Decision.ReusedTurn = true
		plan.Model = sameTurn.model
		plan.Intent = IntentResult{IntentID: sameTurn.intent, Cached: true, Reason: "turn_reused"}
		preferred = &sameTurn.candidate
	} else {
		plan.Decision = ResolveDecision(p, model, streaming, "")
		if plan.Decision.Kind == "default" && p.Classifier != nil && p.Classifier.Enabled {
			if preview {
				plan.Intent = IntentResult{Reason: "classifier_preview_not_requested"}
			} else {
				plan.Intent = r.classifier.Classify(ctx, p, turn, previous.intent, config.Classify)
				plan.Decision = ResolveDecision(p, model, streaming, plan.Intent.IntentID)
			}
		}
		if hasSession && (p.AffinityMode == AffinitySession || turn.TurnID == "" && (p.AffinityMode == AffinityTurn || p.AffinityMode == AffinityAdaptive) || p.AffinityMode == AffinityAdaptive && previous.failures == 0 && now.Sub(previous.success) < 10*time.Minute && (turn.Continuation || previous.cachedTokens > 0)) {
			preferred = &previous.candidate
		}
		if hasTurn && !turn.NewTurn {
			// none selects resources afresh, while intent classification still
			// remains a once-per-turn operation.
			plan.Intent = IntentResult{IntentID: sameTurn.intent, Cached: true, Reason: "turn_reused"}
			plan.Decision = ResolveDecision(p, model, streaming, sameTurn.intent)
		}
	}
	if owner, ok := ctx.Value(statePreferenceKey{}).(Candidate); ok {
		preferred = &owner
	}
	plan.state.previous = preferred
	r.populateDispatchPlan(ctx, config, &plan, preferred, now)
	return plan
}

func (r *ProfileRuntime) populateDispatchPlan(ctx context.Context, config RuntimeConfig, plan *DispatchPlan, preferred *Candidate, now time.Time) {
	p := plan.Profile
	plan.Stages = []DispatchPlanStage{}
	plan.Evaluations = []CandidateEvaluation{}
	plan.EligibleCandidates = []DispatchCandidate{}
	plan.Error = ""
	seen := map[Candidate]bool{}
	for index, stage := range decisionTargetStages(p, plan.Decision) {
		targets := stage.targets
		if stage.kind == "mapping" {
			targets = nil
			for _, target := range stage.targets {
				if mappingTargetAllowed(p, target) {
					targets = append(targets, target)
				}
			}
		}
		cs := expandProfileTargets(config, p, targets, plan.Model)
		admitted, rejected := []DispatchCandidate{}, []CandidateEvaluation{}
		compatibility := map[Candidate]string{}
		for _, c := range cs {
			id := c.Identity()
			reason := ""
			if rejected := plan.state.stateRejected[id]; rejected != "" {
				reason = rejected
				compatibility[id] = "incompatible"
			} else if seen[id] {
				reason = "candidate_duplicate"
			} else if config.Supports != nil && !config.Supports(c, id.Model) {
				reason = "model_unsupported"
			} else if config.Compatible != nil {
				if allowed, why := config.Compatible(ctx, c); !allowed {
					reason = safeDispatchReason(why, "state_backend_incompatible")
					compatibility[id] = "incompatible"
				} else {
					compatibility[id] = "compatible"
				}
			}
			seen[id] = true
			if reason != "" {
				rejected = append(rejected, CandidateEvaluation{Candidate: c, Reason: reason, StateCompatibility: compatibility[id]})
			} else {
				admitted = append(admitted, c)
			}
		}
		stageProfile := p
		// Explicit target order must survive source profile member priority.
		if stage.kind != "default" {
			stageProfile.Members = nil
		}
		evaluations := r.scheduler.Order(stageProfile, admitted, preferred, now)
		for i := range evaluations {
			if value := compatibility[evaluations[i].Candidate.Identity()]; value != "" {
				evaluations[i].StateCompatibility = value
			}
			if p.AffinityMode == AffinityAdaptive && !plan.Decision.ReusedTurn && evaluations[i].Preferred && quotaTier(applicableQuota(evaluations[i].Candidate, now)) > 0 {
				// Adaptive affinity gives way to low remaining quota on a new turn.
				preferred = nil
			}
		}
		if preferred == nil && plan.state.previous != nil && p.AffinityMode == AffinityAdaptive && !plan.Decision.ReusedTurn {
			evaluations = r.scheduler.Order(stageProfile, admitted, nil, now)
			for i := range evaluations {
				if value := compatibility[evaluations[i].Candidate.Identity()]; value != "" {
					evaluations[i].StateCompatibility = value
				}
			}
		}
		evaluations = append(evaluations, rejected...)
		planned := DispatchPlanStage{Index: index, Kind: stage.kind, Evaluations: evaluations, EligibleCandidates: []DispatchCandidate{}}
		for _, evaluation := range evaluations {
			if evaluation.Eligible {
				planned.EligibleCandidates = append(planned.EligibleCandidates, evaluation.Candidate)
				plan.EligibleCandidates = append(plan.EligibleCandidates, evaluation.Candidate)
			}
		}
		plan.Stages = append(plan.Stages, planned)
		plan.Evaluations = append(plan.Evaluations, evaluations...)
	}
	if len(plan.EligibleCandidates) == 0 {
		plan.Error = "route_unavailable"
	}
	plan.state.previous = preferred
}

func (r *ProfileRuntime) Preview(ctx context.Context, profileID int64, model string, streaming bool, turn TurnInput) DispatchPlan {
	return r.Plan(ctx, profileID, model, streaming, turn, true)
}

func (r *ProfileRuntime) ClassifyIntent(ctx context.Context, profileID int64, turn TurnInput) IntentResult {
	config := r.snapshot()
	p, exists := runtimeProfile(config, profileID)
	if !exists {
		return IntentResult{Reason: "routing_profile_missing"}
	}
	_, session := affinityKeys(ctx, p, turn)
	previous, _ := r.remembered(session, r.now(), false)
	return r.classifier.Classify(ctx, p, turn, previous.intent, config.Classify)
}

type profileRequestContextKey struct{}
type profileStateContextKey struct{}

// WithProfileStateKeys accepts only the provider's already-hashed StateKeys.
// It conveys a routing preference without imposing local protocol validation.
func WithProfileStateKeys(ctx context.Context, keys []string) context.Context {
	return context.WithValue(ctx, profileStateContextKey{}, append([]string(nil), keys...))
}

func profileStateKeysFromContext(ctx context.Context) []string {
	keys, _ := ctx.Value(profileStateContextKey{}).([]string)
	return keys
}

func WithProfileRequest(ctx context.Context, plan *DispatchPlan) context.Context {
	if plan != nil && plan.Budget != nil {
		ctx = WithBudget(ctx, plan.Budget)
	}
	return context.WithValue(ctx, profileRequestContextKey{}, plan)
}

func ProfileRequestFromContext(ctx context.Context) *DispatchPlan {
	plan, _ := ctx.Value(profileRequestContextKey{}).(*DispatchPlan)
	return plan
}

type DispatchReservation struct {
	mu           sync.Mutex
	plan         *DispatchPlan
	candidate    DispatchCandidate
	committed    bool
	finished     bool
	cachedTokens int64
	usage        AttemptUsage
	startedAt    time.Time
	settlementID string
}

func (r *ProfileRuntime) Reserve(ctx context.Context, plan *DispatchPlan, candidate DispatchCandidate) (*DispatchReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan == nil || plan.Preview || plan.state == nil || plan.state.runtime != r || plan.Error != "" {
		return nil, errors.New("dispatch_plan_unavailable")
	}
	state := plan.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.responseStarted || state.successful || state.stopped != "" {
		return nil, errors.New("response_already_started")
	}
	if state.reserved {
		return nil, errors.New("dispatch_attempt_inflight")
	}
	if reason := state.stateRejected[candidate.Identity()]; reason != "" {
		return nil, errors.New(reason)
	}
	found := false
	for _, allowed := range plan.EligibleCandidates {
		if allowed.Identity() == candidate.Identity() {
			candidate, found = allowed, true
			break
		}
	}
	if !found {
		return nil, errors.New("candidate_not_in_plan")
	}
	if allowed, reason := state.ledger.Allowed(candidate.Identity()); !allowed {
		return nil, errors.New(reason)
	}
	if err := plan.Budget.Check(); err != nil {
		state.stopped = "retry_budget_exhausted"
		return nil, err
	}
	if check := r.snapshot().CheckAdmission; check != nil {
		if reason := check(candidate); reason != "" {
			return nil, errors.New(reason)
		}
	}
	if !r.scheduler.TryReserve(candidate, r.now()) {
		return nil, errors.New("candidate_no_longer_eligible")
	}
	state.reserved = true
	return &DispatchReservation{plan: plan, candidate: candidate}, nil
}

func (a *DispatchReservation) Candidate() DispatchCandidate { return a.candidate }

// Begin consumes the shared budget; Commit is the integration variant for
// provider paths that have already called the same Budget.Begin themselves.
func (a *DispatchReservation) Begin(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || a.committed {
		return errors.New("dispatch_attempt_already_committed")
	}
	if check := a.plan.state.runtime.snapshot().CheckAdmission; check != nil {
		if reason := check(a.candidate); reason != "" {
			return errors.New(reason)
		}
	}
	if err := a.plan.Budget.Begin(ctx); err != nil {
		return err
	}
	return a.commitLocked(ctx)
}

func (a *DispatchReservation) Commit(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if check := a.plan.state.runtime.snapshot().CheckAdmission; check != nil {
		if reason := check(a.candidate); reason != "" {
			return errors.New(reason)
		}
	}
	return a.commitLocked(ctx)
}

func (a *DispatchReservation) commitLocked(ctx context.Context) error {
	if a.finished || a.committed {
		return errors.New("dispatch_attempt_already_committed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := a.plan.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.responseStarted || state.successful || state.stopped != "" {
		return errors.New("response_already_started")
	}
	newSelection := !state.selected && a.plan.Turn.NewTurn && !a.plan.Decision.ReusedTurn
	if state.previous != nil && *state.previous == a.candidate.Identity() {
		newSelection = false
	}
	domain := a.plan.EligibleCandidates
	for _, stage := range a.plan.Stages {
		for _, candidate := range stage.EligibleCandidates {
			if candidate.Identity() == a.candidate.Identity() {
				domain = stage.EligibleCandidates
				break
			}
		}
	}
	state.runtime.scheduler.CommitSelection(a.plan.Profile, domain, a.candidate, newSelection, state.runtime.now())
	state.ledger.Begin(a.candidate.Identity())
	state.selected, a.committed = true, true
	a.startedAt = state.runtime.now()
	a.settlementID = fmt.Sprintf("%s/%d/%d", a.plan.RequestID, a.startedAt.UnixNano(), len(state.events))
	state.appendEvent(DispatchTraceEvent{Candidate: a.candidate.Identity(), Action: "attempt"})
	return nil
}

func (a *DispatchReservation) ResponseStarted() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || !a.committed {
		return
	}
	state := a.plan.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.responseStarted {
		state.responseStarted = true
		state.appendEvent(DispatchTraceEvent{Candidate: a.candidate.Identity(), Action: "response_started"})
	}
}

func (a *DispatchReservation) SetCachedTokens(tokens int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cachedTokens = max(tokens, 0)
}

func (a *DispatchReservation) SetUsage(usage AttemptUsage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		a.usage = usage
	}
}

func (a *DispatchReservation) Finish(tokens int64, successful bool, failure Failure) {
	a.mu.Lock()
	if a.finished {
		a.mu.Unlock()
		return
	}
	a.finished = true
	committed, cached := a.committed, a.cachedTokens
	settlement := AttemptSettlement{Candidate: a.candidate, ID: a.settlementID, StartedAt: a.startedAt, Usage: a.usage}
	a.mu.Unlock()
	state := a.plan.state
	now := state.runtime.now()
	state.mu.Lock()
	state.reserved = false
	state.runtime.scheduler.Release(a.candidate, tokens, committed && successful, now)
	if committed && successful {
		state.successful, state.responseStarted = true, true
		state.finished = &now
		state.appendEvent(DispatchTraceEvent{Candidate: a.candidate.Identity(), Action: "success"})
		state.runtime.rememberSuccess(a.plan, a.candidate, cached, now)
	} else if committed {
		failure.Reason = safeDispatchReason(failure.Reason, "upstream_failure")
		if failure.Scope == "" {
			failure.Scope = ScopeCandidate
		}
		state.ledger.Fail(a.candidate.Identity(), failure)
		state.appendEvent(DispatchTraceEvent{Candidate: a.candidate.Identity(), Action: "failed", Reason: failure.Reason, Scope: failure.Scope})
		state.runtime.rememberFailure(a.plan, a.candidate)
		if state.responseStarted || failure.Terminal || failure.Scope == ScopeRequest {
			state.stopped = failure.Reason
			state.finished = &now
		}
	}
	publish := state.publish
	state.mu.Unlock()
	if committed {
		if settle := state.runtime.snapshot().OnSettlement; settle != nil {
			settle(settlement)
		}
	}
	if publish != nil && committed {
		publish(a.plan.Trace())
	}
}

func (s *dispatchRequestState) appendEvent(event DispatchTraceEvent) {
	if len(s.events) < 128 {
		s.events = append(s.events, event)
	}
}

func safeDispatchReason(reason, fallback string) string {
	if reason == "" || len(reason) > 100 {
		return fallback
	}
	for _, char := range reason {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == ':' || char == '-') {
			return fallback
		}
	}
	return reason
}

func (r *ProfileRuntime) rememberSuccess(plan *DispatchPlan, candidate DispatchCandidate, cachedTokens int64, now time.Time) {
	state := plan.state
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := profileAffinity{candidate: candidate.Identity(), revision: plan.Profile.Revision, decision: plan.Decision, intent: plan.Intent.IntentID, model: plan.Model, cachedTokens: cachedTokens, expires: now.Add(time.Hour), used: now, success: now}
	// Intent memory is independent of whether resource affinity is enabled.
	for _, key := range []string{state.turnKey, state.sessionKey} {
		if key != "" {
			copy := entry
			copy.session = key == state.sessionKey
			copy.profileID = plan.Profile.ID
			r.affinity[key] = copy
		}
	}
	r.trimAffinityLocked(now)
}

func (r *ProfileRuntime) trimAffinityLocked(now time.Time) {
	for key, value := range r.affinity {
		if !now.Before(value.expires) {
			delete(r.affinity, key)
		}
	}
	for len(r.affinity) > 4096 {
		oldest := ""
		var used time.Time
		for key, value := range r.affinity {
			if oldest == "" || value.used.Before(used) {
				oldest, used = key, value.used
			}
		}
		delete(r.affinity, oldest)
	}
}

// RememberResponseStates links protocol state hashes to the completed decision
// so a previous_response_id-only tool request can retain the original route.
func (r *ProfileRuntime) RememberResponseStates(plan *DispatchPlan, candidate DispatchCandidate, keys []string) {
	if plan == nil || plan.state == nil || plan.Preview || len(keys) == 0 {
		return
	}
	plan.state.mu.Lock()
	if !plan.state.successful {
		plan.state.mu.Unlock()
		return
	}
	scope := plan.state.scopeHash
	plan.state.mu.Unlock()
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := profileAffinity{candidate: candidate.Identity(), profileID: plan.Profile.ID, revision: plan.Profile.Revision, decision: plan.Decision, intent: plan.Intent.IntentID, model: plan.Model, expires: now.Add(time.Hour), used: now, success: now}
	for i, key := range keys {
		if i >= 256 {
			break
		}
		r.affinity[routingHash(scope, "state", key)] = entry
	}
	r.trimAffinityLocked(now)
}

func (r *ProfileRuntime) rememberFailure(plan *DispatchPlan, candidate DispatchCandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range []string{plan.state.turnKey, plan.state.sessionKey} {
		if entry, exists := r.affinity[key]; exists && entry.candidate == candidate.Identity() {
			entry.failures++
			r.affinity[key] = entry
		}
	}
}

func (p *DispatchPlan) Trace() DispatchTrace {
	trace := DispatchTrace{RequestID: p.RequestID, Provider: p.Profile.Provider, ProfileID: p.Profile.ID, ProfileName: p.Profile.Name, Strategy: p.Profile.Strategy, AffinityMode: p.Profile.AffinityMode, Decision: p.Decision, Intent: p.Intent, StopReason: p.Error, Candidates: []DispatchTraceEvent{}}
	if p.state == nil {
		return trace
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	trace.CreatedAt, trace.FinishedAt = p.state.created, p.state.finished
	trace.ResponseStarted, trace.Successful = p.state.responseStarted, p.state.successful
	trace.Candidates = append(trace.Candidates, p.state.events...)
	if p.state.stopped != "" {
		trace.StopReason = p.state.stopped
	}
	for _, evaluation := range p.Evaluations {
		if !evaluation.Eligible && len(trace.Candidates) < 128 {
			trace.Candidates = append(trace.Candidates, DispatchTraceEvent{Candidate: evaluation.Candidate.Identity(), Action: "skipped", Reason: safeDispatchReason(evaluation.Reason, "candidate_unavailable")})
		}
	}
	trace.Attempts, trace.WaitMS = p.Budget.Stats()
	return trace
}

func (p *DispatchPlan) Skip(candidate DispatchCandidate, reason string) {
	if p.state == nil || p.Preview {
		return
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	reason = safeDispatchReason(reason, "candidate_unavailable")
	if strings.HasPrefix(reason, "state_") {
		if p.state.stateRejected == nil {
			p.state.stateRejected = map[Candidate]string{}
		}
		p.state.stateRejected[candidate.Identity()] = reason
	}
	p.state.appendEvent(DispatchTraceEvent{Candidate: candidate.Identity(), Action: "skipped", Reason: reason})
}

// End publishes the exhausted/cancelled terminal trace when no attempt can
// complete it. It never converts response_started into protocol success.
func (p *DispatchPlan) End(reason string) {
	if p.state == nil || p.Preview {
		return
	}
	p.state.mu.Lock()
	if !p.state.successful {
		p.state.stopped = safeDispatchReason(reason, "route_unavailable")
		now := p.state.runtime.now()
		p.state.finished = &now
	}
	publish := p.state.publish
	p.state.mu.Unlock()
	if publish != nil {
		publish(p.Trace())
	}
}
