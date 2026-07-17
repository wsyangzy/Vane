package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CredentialGroupDailyCost returns the settled cost for one credential group
// in the application's configured timezone, plus the next natural-day reset.
func (ut *UsageTracker) CredentialGroupDailyCost(
	ctx context.Context,
	provider string,
	channel string,
	groupName string,
) (float64, time.Time, error) {
	if ut == nil {
		return 0, time.Time{}, fmt.Errorf("usage tracker is not initialized")
	}
	provider = strings.TrimSpace(provider)
	channel = strings.TrimSpace(channel)
	groupName = strings.TrimSpace(groupName)
	if provider == "" || channel == "" || groupName == "" {
		return 0, time.Time{}, fmt.Errorf("provider, channel and group name are required")
	}

	now := ut.now()
	location := now.Location()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	resetAt := dayStart.AddDate(0, 0, 1)
	dayEnd := resetAt.Add(-time.Nanosecond)
	opts := &QueryOptions{
		StartDate: &dayStart,
		EndDate:   &dayEnd,
		Provider:  provider,
		Channel:   channel,
		GroupName: groupName,
	}

	totals, err := ut.QueryUsageStatsTotals(ctx, opts)
	if err != nil {
		return 0, resetAt, err
	}
	usedUSD := 0.0
	if totals != nil {
		usedUSD = totals.TotalCostUSD
	}
	for _, request := range ut.getFilteredHotPoolRequests(opts) {
		usedUSD += request.TotalCostUSD
	}
	return usedUSD, resetAt, nil
}
