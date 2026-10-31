package routing

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// CandidateEvaluation contains safe configuration identities and decision
// reasons only. Candidate remains unchanged so its baseline Load can be reused
// when reserving; Load is the effective account load used for this decision.
type CandidateEvaluation struct {
	Candidate          DispatchCandidate `json:"candidate"`
	Eligible           bool              `json:"eligible"`
	Reason             string            `json:"reason"`
	QuotaKnown         bool              `json:"quota_known"`
	UsedPercent        *float64          `json:"used_percent,omitempty"`
	ResetAt            *time.Time        `json:"reset_at,omitempty"`
	PaceScore          *float64          `json:"pace_score,omitempty"`
	Load               LoadView          `json:"load"`
	Preferred          bool              `json:"preferred"`
	StateCompatibility string            `json:"state_compatibility"`
}

type dispatchAccount struct {
	provider string
	channel  int64
	group    int64
	hash     string
}

type dispatchBucket struct {
	provider string
	profile  int64
	revision int64
	channel  int64
	mode     CandidateMode
}

type dispatchLoad struct {
	concurrent int
	tokens     float64
	requests   float64
	updated    time.Time
}

type dispatchRotation struct {
	last         dispatchAccount
	lastPosition int
	lastIndex    int
	weights      map[dispatchAccount]int64
	touched      time.Time
}

const (
	maxDispatchRotations = 2048
	maxDispatchAccounts  = 4096
)

// Scheduler owns cross-request rotation, smooth weights and account load.
// Order is also the preview implementation and never changes these maps.
type Scheduler struct {
	mu       sync.Mutex
	loads    map[dispatchAccount]dispatchLoad
	rotation map[dispatchBucket]dispatchRotation
}

func NewScheduler() *Scheduler {
	return &Scheduler{loads: map[dispatchAccount]dispatchLoad{}, rotation: map[dispatchBucket]dispatchRotation{}}
}

func accountFor(c DispatchCandidate) dispatchAccount {
	if c.AccountIDHash != "" {
		return dispatchAccount{provider: c.Provider, hash: c.AccountIDHash}
	}
	return dispatchAccount{provider: c.Provider, channel: c.ChannelID, group: c.GroupID}
}

func bucketFor(p RoutingProfile, c DispatchCandidate) dispatchBucket {
	b := dispatchBucket{provider: p.Provider, profile: p.ID, revision: p.Revision, mode: p.CandidateMode}
	if p.CandidateMode != UnifiedCandidates {
		b.channel = c.ChannelID
	}
	return b
}

func decayLoad(load dispatchLoad, now time.Time) dispatchLoad {
	if !load.updated.IsZero() && now.After(load.updated) {
		factor := math.Exp2(-now.Sub(load.updated).Hours())
		load.tokens *= factor
		load.requests *= factor
	}
	load.updated = now
	return load
}

func (s *Scheduler) loadLocked(c DispatchCandidate, now time.Time) LoadView {
	local := decayLoad(s.loads[accountFor(c)], now)
	load := c.Load
	load.Concurrent += local.concurrent
	load.DecayedTokens += local.tokens
	load.DecayedRequests += local.requests
	return load
}

// Load reports an account's effective load without persisting decay updates.
func (s *Scheduler) Load(c DispatchCandidate, now time.Time) LoadView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(c, now)
}

type dispatchQuota struct {
	known   bool
	stale   bool
	used    float64
	reset   time.Time
	pace    float64
	paceSet bool
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func applicableQuota(c DispatchCandidate, now time.Time) dispatchQuota {
	var result dispatchQuota
	model := c.UpstreamModel
	if model == "" {
		model = c.Model
	}
	missingReset := false
	for _, q := range c.Quota.Windows {
		accountMismatch := q.AccountIDHash != "" && c.AccountIDHash != "" && q.AccountIDHash != c.AccountIDHash
		bindingMismatch := (q.ChannelID != 0 && q.ChannelID != c.ChannelID) || (q.GroupID != 0 && q.GroupID != c.GroupID)
		if q.AccountIDHash != "" && q.AccountIDHash == c.AccountIDHash {
			bindingMismatch = false
		}
		if (q.Provider != "" && q.Provider != c.Provider) || accountMismatch || bindingMismatch ||
			(q.ModelScope != "" && q.ModelScope != "*" && q.ModelScope != model) {
			continue
		}
		if q.Confidence == "stale" || q.Confidence == "unknown" || q.ExpiresAt.IsZero() ||
			!now.Before(q.ExpiresAt) || (q.ResetAt != nil && !now.Before(*q.ResetAt)) {
			result.stale = true
			continue
		}
		used := -1.0
		if q.UsedPercent != nil && finite(*q.UsedPercent) && *q.UsedPercent >= 0 {
			used = math.Min(*q.UsedPercent, 100)
		} else if q.Remaining != nil && finite(*q.Remaining) {
			if *q.Remaining <= 0 {
				used = 100
			} else if q.Limit != nil && finite(*q.Limit) && *q.Limit > 0 && *q.Remaining <= *q.Limit {
				used = 100 * (1 - *q.Remaining / *q.Limit)
			}
		}
		if used < 0 {
			continue
		}
		result.known = true
		result.used = math.Max(result.used, used)
		if q.ResetAt == nil || q.ResetAt.IsZero() {
			missingReset = true
			continue
		}
		if result.reset.IsZero() || q.ResetAt.Before(result.reset) {
			result.reset = *q.ResetAt
		}
		// Independent limits constrain one another. Adding their remaining
		// percentages would manufacture capacity that the account cannot use.
		score := (100 - used) / math.Max(q.ResetAt.Sub(now).Hours(), 1)
		if !result.paceSet || score < result.pace {
			result.pace, result.paceSet = score, true
		}
	}
	if missingReset {
		result.paceSet = false
	}
	return result
}

func candidateEligibility(c DispatchCandidate, load LoadView, q dispatchQuota, now time.Time) string {
	if c.Balance.Exhausted() {
		return "manual_balance_exhausted"
	}
	if !c.Enabled {
		return "candidate_disabled"
	}
	if c.Health.RepairRequired {
		return "credential_repair_required"
	}
	if c.Health.CoolingDown && (c.Health.Until.IsZero() || now.Before(c.Health.Until)) {
		return "cooldown"
	}
	if q.known && q.used >= 100 {
		return "quota_exhausted"
	}
	if load.MaxConcurrent > 0 && load.Concurrent >= load.MaxConcurrent {
		return "concurrency_limit"
	}
	return ""
}

type dispatchSeat struct {
	account     dispatchAccount
	channel     int64
	evaluations []CandidateEvaluation
	quota       dispatchQuota
	weight      int64
	position    int
	index       int
	load        LoadView
	preferred   bool
}

type dispatchSeatKey struct {
	account dispatchAccount
	channel int64
}

func quotaFallback(q dispatchQuota, suffix string) string {
	if q.stale {
		return "quota_stale_" + suffix
	}
	return "quota_unknown_" + suffix
}

// Order evaluates and orders all candidates. Ineligible entries follow the
// eligible entries and retain a precise skip reason. Each account votes once;
// adding endpoints cannot increase a member's weight or rotation frequency.
func (s *Scheduler) Order(p RoutingProfile, candidates []DispatchCandidate, preferred *Candidate, now time.Time) []CandidateEvaluation {
	s.mu.Lock()
	defer s.mu.Unlock()
	seats := make([]dispatchSeat, 0, len(candidates))
	seatIndexes := map[dispatchSeatKey]int{}
	skipped := make([]CandidateEvaluation, 0)
	seen := map[Candidate]bool{}
	for i, c := range candidates {
		q := applicableQuota(c, now)
		load := s.loadLocked(c, now)
		e := CandidateEvaluation{Candidate: c, Eligible: true, QuotaKnown: q.known, Load: load, StateCompatibility: "unknown"}
		if q.known {
			e.UsedPercent = new(float64)
			*e.UsedPercent = q.used
		}
		if !q.reset.IsZero() {
			e.ResetAt = new(time.Time)
			*e.ResetAt = q.reset
		}
		if q.paceSet {
			e.PaceScore = new(float64)
			*e.PaceScore = q.pace
		}
		e.Reason = candidateEligibility(c, load, q, now)
		if !p.Enabled {
			e.Reason = "profile_disabled"
		} else if c.Provider != p.Provider {
			e.Reason = "provider_mismatch"
		} else if seen[c.Identity()] {
			e.Reason = "candidate_duplicate"
		}
		seen[c.Identity()] = true
		if e.Reason != "" {
			e.Eligible = false
			skipped = append(skipped, e)
			continue
		}
		key := accountFor(c)
		seatKey := dispatchSeatKey{account: key}
		if p.CandidateMode != UnifiedCandidates {
			seatKey.channel = c.ChannelID
		}
		e.Preferred = preferred != nil && p.AffinityMode != AffinityNone && c.Identity() == *preferred
		if at, exists := seatIndexes[seatKey]; exists {
			seat := &seats[at]
			seat.evaluations = append(seat.evaluations, e)
			seat.preferred = seat.preferred || e.Preferred
			seat.weight = max(seat.weight, int64(max(c.Weight, 1)))
			continue
		}
		seatIndexes[seatKey] = len(seats)
		seats = append(seats, dispatchSeat{account: key, channel: c.ChannelID, evaluations: []CandidateEvaluation{e}, quota: q, weight: int64(max(c.Weight, 1)), position: c.Position, index: i, load: load, preferred: e.Preferred})
	}
	sort.SliceStable(seats, func(i, j int) bool { return seats[i].position < seats[j].position })
	// Ordered channels retain member priority independently of their account
	// strategy. Unknown/externally referenced channels keep expansion order.
	channelRanks := map[int64]int{}
	for _, m := range p.Members {
		if _, exists := channelRanks[m.ChannelID]; !exists {
			channelRanks[m.ChannelID] = len(channelRanks)
		}
	}
	for _, seat := range seats {
		if _, exists := channelRanks[seat.channel]; !exists {
			channelRanks[seat.channel] = len(channelRanks)
		}
	}
	buckets := [][]dispatchSeat{}
	if p.CandidateMode == UnifiedCandidates {
		buckets = append(buckets, seats)
	} else {
		sort.SliceStable(seats, func(i, j int) bool {
			return channelRanks[seats[i].channel] < channelRanks[seats[j].channel]
		})
		for _, seat := range seats {
			if len(buckets) == 0 || buckets[len(buckets)-1][0].channel != seat.channel {
				buckets = append(buckets, []dispatchSeat{})
			}
			buckets[len(buckets)-1] = append(buckets[len(buckets)-1], seat)
		}
	}
	result := make([]CandidateEvaluation, 0, len(candidates))
	for _, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		state := s.rotation[bucketFor(p, bucket[0].evaluations[0].Candidate)]
		bucket = orderDispatchSeats(p, bucket, state)
		for _, seat := range bucket {
			sort.SliceStable(seat.evaluations, func(i, j int) bool { return seat.evaluations[i].Preferred && !seat.evaluations[j].Preferred })
			for _, e := range seat.evaluations {
				e.Reason = strategyReason(p.Strategy, seat.quota, bucket)
				if e.Preferred {
					e.Reason = "affinity_hit"
				}
				result = append(result, e)
			}
		}
	}
	// A retained turn/session decision precedes configured channel priority.
	sort.SliceStable(result, func(i, j int) bool { return result[i].Preferred && !result[j].Preferred })
	return append(result, skipped...)
}

func strategyReason(strategy string, q dispatchQuota, seats []dispatchSeat) string {
	if !q.known {
		if strategy == StrategyUsage || strategy == StrategyPace {
			return quotaFallback(q, "recent_load")
		}
		return quotaFallback(q, "config_order")
	}
	switch strategy {
	case StrategyUsage:
		for _, seat := range seats {
			if !seat.quota.known {
				return "quota_mixed_recent_load"
			}
		}
		return "quota_used_least"
	case StrategyPace:
		if !q.paceSet {
			return "quota_reset_unknown_recent_load"
		}
		return "quota_pace"
	case StrategyRotate:
		return "rotation_order"
	case StrategyWeighted:
		return "weighted_order"
	case StrategyOrder:
		return "configured_order"
	default:
		if q.used >= 98 {
			return "quota_nearly_spent"
		}
		if q.used >= 90 {
			return "quota_low"
		}
		return "quota_reset_soon"
	}
}

func quotaTier(q dispatchQuota) int {
	if q.known && q.used >= 98 {
		return 2
	}
	if q.known && q.used >= 90 {
		return 1
	}
	return 0
}

func orderDispatchSeats(p RoutingProfile, seats []dispatchSeat, state dispatchRotation) []dispatchSeat {
	allKnown := true
	for _, seat := range seats {
		allKnown = allKnown && seat.quota.known
	}
	rotationRank := map[dispatchAccount]int{}
	start := 0
	foundLast := false
	for i, seat := range seats {
		if seat.account == state.last {
			start = (i + 1) % len(seats)
			foundLast = true
			break
		}
	}
	if !foundLast && state.last.provider != "" {
		for i, seat := range seats {
			if seat.position > state.lastPosition || (seat.position == state.lastPosition && seat.index > state.lastIndex) {
				start = i
				break
			}
		}
	}
	for i := range seats {
		rotationRank[seats[(start+i)%len(seats)].account] = i
	}
	if p.Strategy == StrategyWeighted {
		return orderWeightedDispatchSeats(seats, state)
	}
	sort.SliceStable(seats, func(i, j int) bool {
		a, b := seats[i], seats[j]
		if a.preferred != b.preferred {
			return a.preferred
		}
		// Even a busy available account precedes nearly exhausted quota;
		// concurrency only spreads work within the same smart quota tier.
		if (p.Strategy == StrategySmart || p.Strategy == "") && quotaTier(a.quota) != quotaTier(b.quota) {
			return quotaTier(a.quota) < quotaTier(b.quota)
		}
		// Fixed order deliberately keeps consuming its first usable account.
		// Other strategies avoid concentrating concurrent work on a busy
		// account while an equally eligible idle account is available.
		if p.Strategy != StrategyOrder && a.load.Concurrent != b.load.Concurrent {
			return a.load.Concurrent < b.load.Concurrent
		}
		switch p.Strategy {
		case StrategyRotate:
			return rotationRank[a.account] < rotationRank[b.account]
		case StrategyUsage:
			if allKnown && a.quota.used != b.quota.used {
				return a.quota.used < b.quota.used
			}
			if a.load.DecayedTokens != b.load.DecayedTokens {
				return a.load.DecayedTokens < b.load.DecayedTokens
			}
			if a.load.DecayedRequests != b.load.DecayedRequests {
				return a.load.DecayedRequests < b.load.DecayedRequests
			}
		case StrategyPace:
			if a.quota.paceSet != b.quota.paceSet {
				return a.quota.paceSet
			}
			if a.quota.paceSet && a.quota.pace != b.quota.pace {
				return a.quota.pace > b.quota.pace
			}
			if a.load.DecayedTokens != b.load.DecayedTokens {
				return a.load.DecayedTokens < b.load.DecayedTokens
			}
		case StrategyOrder:
		default:
			if quotaTier(a.quota) != quotaTier(b.quota) {
				return quotaTier(a.quota) < quotaTier(b.quota)
			}
			if quotaTier(a.quota) > 0 {
				return a.quota.used < b.quota.used
			}
			if a.evaluations[0].Candidate.Quota.Learns != b.evaluations[0].Candidate.Quota.Learns {
				return a.evaluations[0].Candidate.Quota.Learns
			}
			if a.quota.reset.IsZero() != b.quota.reset.IsZero() {
				return !a.quota.reset.IsZero()
			}
			if !a.quota.reset.Equal(b.quota.reset) {
				return a.quota.reset.Before(b.quota.reset)
			}
		}
		return false
	})
	return seats
}

func orderWeightedDispatchSeats(seats []dispatchSeat, state dispatchRotation) []dispatchSeat {
	weights := map[dispatchAccount]int64{}
	for _, seat := range seats {
		weights[seat.account] = state.weights[seat.account]
	}
	remaining := append([]dispatchSeat(nil), seats...)
	result := make([]dispatchSeat, 0, len(seats))
	for len(remaining) > 0 {
		var total int64
		best := 0
		for i, seat := range remaining {
			weights[seat.account] += seat.weight
			total += seat.weight
			if i == 0 {
				continue
			}
			prior := remaining[best]
			better := weights[seat.account] > weights[prior.account]
			if seat.load.Concurrent != prior.load.Concurrent {
				better = seat.load.Concurrent < prior.load.Concurrent
			}
			if seat.preferred != prior.preferred {
				better = seat.preferred
			}
			if better {
				best = i
			}
		}
		winner := remaining[best]
		weights[winner.account] -= total
		result = append(result, winner)
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	return result
}

// TryReserve rechecks eligibility under the account lock. The caller releases
// exactly once, including cancellations before sending. No rotation or load
// usage is committed until CommitSelection immediately before the actual send.
func (s *Scheduler) TryReserve(c DispatchCandidate, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if candidateEligibility(c, s.loadLocked(c, now), applicableQuota(c, now), now) != "" {
		return false
	}
	key := accountFor(c)
	load := decayLoad(s.loads[key], now)
	load.concurrent++
	s.loads[key] = load
	s.pruneLoadsLocked(key)
	return true
}

// CommitSelection records an actual send. newSelection must be false for tool
// continuations, affinity hits and subsequent attempts in the same request.
// candidates is the same eligible domain supplied to Order, not a fresh query.
func (s *Scheduler) CommitSelection(p RoutingProfile, candidates []DispatchCandidate, selected DispatchCandidate, newSelection bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := accountFor(selected)
	load := decayLoad(s.loads[key], now)
	load.requests++
	s.loads[key] = load
	if !newSelection || (p.Strategy != StrategyRotate && p.Strategy != StrategyWeighted) {
		return
	}
	bucket := bucketFor(p, selected)
	state := s.rotation[bucket]
	state.last = key
	state.touched = now
	state.lastPosition = selected.Position
	for i, candidate := range candidates {
		if candidate.Identity() == selected.Identity() {
			state.lastIndex = i
			break
		}
	}
	if p.Strategy == StrategyWeighted {
		weights := map[dispatchAccount]int64{}
		for _, c := range candidates {
			if bucketFor(p, c) != bucket || c.Provider != p.Provider {
				continue
			}
			candidateLoad := s.loadLocked(c, now)
			if accountFor(c) == key {
				candidateLoad.Concurrent = max(0, candidateLoad.Concurrent-1)
			}
			if candidateEligibility(c, candidateLoad, applicableQuota(c, now), now) != "" {
				continue
			}
			account := accountFor(c)
			weights[account] = max(weights[account], int64(max(c.Weight, 1)))
		}
		if state.weights == nil {
			state.weights = map[dispatchAccount]int64{}
		}
		var total int64
		for account, weight := range weights {
			state.weights[account] += weight
			total += weight
		}
		state.weights[key] -= total
		// Removed members must not retain accumulated credit indefinitely.
		for account := range state.weights {
			if _, exists := weights[account]; !exists {
				delete(state.weights, account)
			}
		}
	}
	s.rotation[bucket] = state
	s.pruneRotationsLocked(bucket)
}

// Release accounts for actual tokens, including a partial failed response.
// successful is protocol completion, not merely that response bytes started;
// failed attempts still contribute to recent load and never create success.
func (s *Scheduler) Release(c DispatchCandidate, tokens int64, successful bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := accountFor(c)
	load := decayLoad(s.loads[key], now)
	if load.concurrent <= 0 {
		return
	}
	load.concurrent--
	if successful && tokens <= 0 {
		// Some completed responses omit usage. Preserve a nonzero local
		// signal without presenting this estimate as provider quota.
		tokens = 1
	}
	load.tokens += float64(max(tokens, 0))
	s.loads[key] = load
	s.pruneLoadsLocked(key)
}

func (s *Scheduler) pruneRotationsLocked(keep dispatchBucket) {
	for len(s.rotation) > maxDispatchRotations {
		var oldest dispatchBucket
		var oldestTime time.Time
		found := false
		for key, state := range s.rotation {
			if key == keep {
				continue
			}
			if !found || state.touched.Before(oldestTime) {
				oldest, oldestTime, found = key, state.touched, true
			}
		}
		if !found {
			return
		}
		delete(s.rotation, oldest)
	}
}

func (s *Scheduler) pruneLoadsLocked(keep dispatchAccount) {
	for len(s.loads) > maxDispatchAccounts {
		var oldest dispatchAccount
		var oldestTime time.Time
		found := false
		for key, load := range s.loads {
			// Reservations outlive profile revisions. Never evict an active
			// account, even if this temporarily exceeds the idle-state limit.
			if key == keep || load.concurrent > 0 {
				continue
			}
			if !found || load.updated.Before(oldestTime) {
				oldest, oldestTime, found = key, load.updated, true
			}
		}
		if !found {
			return
		}
		delete(s.loads, oldest)
	}
}

// Preview is intentionally identical to the runtime's read-only ordering.
func (s *Scheduler) Preview(p RoutingProfile, candidates []DispatchCandidate, preferred *Candidate, now time.Time) []CandidateEvaluation {
	return s.Order(p, candidates, preferred, now)
}

// Keep quota reasons machine-readable; callers localize them at the UI edge.
func IsQuotaUnknownReason(reason string) bool {
	return strings.HasPrefix(reason, "quota_unknown_") || strings.HasPrefix(reason, "quota_stale_") || reason == "quota_reset_unknown_recent_load"
}
