package routing

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Money is stored as integer billionths of USD. Decimal strings cross the UI
// boundary so neither JS precision nor repeated float subtraction changes it.
const USDScale int64 = 1_000_000_000

func ParseUSD(value string) (int64, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 10 {
		return 0, errors.New("balance_amount_invalid")
	}
	for _, ch := range value {
		if ch != '.' && (ch < '0' || ch > '9') {
			return 0, errors.New("balance_amount_invalid")
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("balance_amount_invalid")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 9 {
		return 0, errors.New("balance_amount_precision")
	}
	minor, _ := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
	if whole > math.MaxInt64/USDScale || whole == math.MaxInt64/USDScale && minor > math.MaxInt64%USDScale {
		return 0, errors.New("balance_amount_overflow")
	}
	return whole*USDScale + minor, nil
}

func FormatUSD(value int64) string {
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(value + 1)) + 1
	} else {
		magnitude = uint64(value)
	}
	result := fmt.Sprintf("%d.%09d", magnitude/uint64(USDScale), magnitude%uint64(USDScale))
	result = strings.TrimRight(strings.TrimRight(result, "0"), ".")
	if negative {
		result = "-" + result
	}
	return result
}

func CostToUSD(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= 1e9 {
		return 0, errors.New("balance_cost_invalid")
	}
	return int64(math.Round(value * float64(USDScale))), nil
}

type ManualBalance struct {
	Provider          string    `json:"provider"`
	ChannelID         int64     `json:"channel_id"`
	GroupID           int64     `json:"group_id"`
	AccountIDHash     string    `json:"-"`
	BindingChanged    bool      `json:"binding_changed"`
	Currency          string    `json:"currency"`
	Enabled           bool      `json:"enabled"`
	StopWhenExhausted bool      `json:"stop_when_exhausted"`
	BalanceUSD        string    `json:"balance_usd"`
	BaselineUSD       string    `json:"baseline_usd"`
	CreditedUSD       string    `json:"credited_usd"`
	SpentUSD          string    `json:"spent_usd"`
	IncompleteCount   int64     `json:"incomplete_count"`
	Revision          int64     `json:"revision"`
	CalibratedAt      time.Time `json:"calibrated_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (b *ManualBalance) Exhausted() bool {
	return b != nil && b.Enabled && b.StopWhenExhausted && (b.BalanceUSD == "0" || strings.HasPrefix(b.BalanceUSD, "-"))
}

type ManualBalanceUpdate struct {
	Provider          string `json:"provider"`
	ChannelID         int64  `json:"channel_id"`
	GroupID           int64  `json:"group_id"`
	Action            string `json:"action"`
	AmountUSD         string `json:"amount_usd"`
	StopWhenExhausted bool   `json:"stop_when_exhausted"`
	Revision          int64  `json:"revision"`
	OperationID       string `json:"operation_id"`
}

type BalanceEntry struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	AmountUSD  string    `json:"amount_usd"`
	BalanceUSD string    `json:"balance_usd"`
	Estimated  bool      `json:"estimated"`
	CreatedAt  time.Time `json:"created_at"`
}

// AttemptUsage contains accounting facts only; no messages, response content,
// credentials or caller identifiers enter the wallet or routing trace.
type AttemptUsage struct {
	Exempt                                                                             bool
	Known                                                                              bool
	Complete                                                                           bool
	InputTokens, OutputTokens, ReasoningTokens                                         int64
	CacheReadTokens, CacheCreationTokens, CacheCreation5mTokens, CacheCreation1hTokens int64
	InputIncludesCacheRead                                                             bool
	ServiceTier                                                                        string
	RequestedServiceTier                                                               string
	CacheWrite30mTokens                                                                int64
	CacheWrite12hTokens                                                                int64
}

type AttemptSettlement struct {
	Candidate DispatchCandidate
	ID        string
	StartedAt time.Time
	Usage     AttemptUsage
}
