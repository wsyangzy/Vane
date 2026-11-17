package routing

import "time"

// Blockage is public diagnostic evidence captured when admission fails. It must
// never contain credentials, account hashes, request content or upstream bodies.
type Blockage struct {
	Provider      string     `json:"provider"`
	ChannelID     int64      `json:"channel_id"`
	GroupID       int64      `json:"group_id"`
	EndpointID    string     `json:"endpoint_id"`
	ChannelName   string     `json:"channel_name"`
	GroupName     string     `json:"group_name"`
	EndpointName  string     `json:"endpoint_name"`
	Model         string     `json:"model,omitempty"`
	Reason        string     `json:"reason"`
	Cause         string     `json:"cause,omitempty"`
	Recovery      string     `json:"recovery"`
	RecoverAt     *time.Time `json:"recover_at,omitempty"`
	DailyUsedUSD  *float64   `json:"daily_used_usd,omitempty"`
	DailyLimitUSD *float64   `json:"daily_limit_usd,omitempty"`
	ObservedAt    time.Time  `json:"observed_at"`
}

func describeBlockage(config RuntimeConfig, candidate DispatchCandidate, reason string) *Blockage {
	if config.DescribeBlockage == nil {
		return nil
	}
	return config.DescribeBlockage(candidate, reason)
}

// QuotaRecoveryAt applies the scheduler's model, identity and freshness rules.
// Every exhausted applicable window must have a known reset.
func QuotaRecoveryAt(candidate DispatchCandidate, now time.Time) (time.Time, bool) {
	var next time.Time
	for _, window := range candidate.Quota.Windows {
		one := candidate
		one.Quota.Windows = []QuotaSnapshot{window}
		quota := applicableQuota(one, now)
		if !quota.known || quota.used < 100 {
			continue
		}
		if window.ResetAt == nil || window.ResetAt.IsZero() {
			return time.Time{}, false
		}
		if window.ResetAt.After(next) {
			next = *window.ResetAt
		}
	}
	return next, !next.IsZero()
}
