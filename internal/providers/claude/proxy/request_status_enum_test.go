package proxy

import (
	"testing"
)

func TestReqStatus_IsValidStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{ReqStatusPending, true},
		{ReqStatusForwarding, true},
		{ReqStatusStreaming, true},
		{ReqStatusProcessing, true},
		{ReqStatusRetry, true},
		{ReqStatusSuspended, true},
		{ReqStatusCompleted, true},
		{ReqStatusFailed, true},
		{ReqStatusCancelled, true},
		{"invalid_status", false},
		{"", false},
		{"random_text", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := isValidReqStatus(tt.status)
			if got != tt.want {
				t.Errorf("isValidReqStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestReqStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{ReqStatusCompleted, true},
		{ReqStatusFailed, true},
		{ReqStatusCancelled, true},
		{ReqStatusPending, false},
		{ReqStatusForwarding, false},
		{ReqStatusStreaming, false},
		{ReqStatusProcessing, false},
		{ReqStatusRetry, false},
		{ReqStatusSuspended, false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := isTerminalReqStatus(tt.status)
			if got != tt.want {
				t.Errorf("isTerminalReqStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestReqStatus_ValidTransition(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// 非终态之间转换 - 允许
		{"pending → forwarding", ReqStatusPending, ReqStatusForwarding, true},
		{"forwarding → streaming", ReqStatusForwarding, ReqStatusStreaming, true},
		{"streaming → completed", ReqStatusStreaming, ReqStatusCompleted, true},
		{"forwarding → retry", ReqStatusForwarding, ReqStatusRetry, true},
		{"retry → forwarding", ReqStatusRetry, ReqStatusForwarding, true},
		{"forwarding → suspended", ReqStatusForwarding, ReqStatusSuspended, true},

		// 非终态 → 终态 - 允许
		{"forwarding → failed", ReqStatusForwarding, ReqStatusFailed, true},
		{"streaming → cancelled", ReqStatusStreaming, ReqStatusCancelled, true},

		// 终态 → 任意状态 - 拒绝（除特例外）
		{"completed → forwarding (非法)", ReqStatusCompleted, ReqStatusForwarding, false},
		{"failed → forwarding (非法)", ReqStatusFailed, ReqStatusForwarding, false},
		{"cancelled → forwarding (非法)", ReqStatusCancelled, ReqStatusForwarding, false},

		// 终态特例：cancelled ↔ failed 允许
		{"cancelled → failed (允许特例)", ReqStatusCancelled, ReqStatusFailed, true},
		{"failed → cancelled (允许特例)", ReqStatusFailed, ReqStatusCancelled, true},

		// 终态 → completed 拒绝
		{"failed → completed (非法)", ReqStatusFailed, ReqStatusCompleted, false},
		{"cancelled → completed (非法)", ReqStatusCancelled, ReqStatusCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidReqStatusTransition(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("isValidReqStatusTransition(%q → %q) = %v, want %v",
					tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestReqStatus_ValidValuesContainsAll(t *testing.T) {
	values := validReqStatusValues()
	if len(values) != 9 {
		t.Errorf("expected 9 valid status values, got %d", len(values))
	}

	required := []string{
		ReqStatusPending, ReqStatusForwarding, ReqStatusStreaming, ReqStatusProcessing,
		ReqStatusRetry, ReqStatusSuspended,
		ReqStatusCompleted, ReqStatusFailed, ReqStatusCancelled,
	}

	for _, want := range required {
		found := false
		for _, got := range values {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("validReqStatusValues() missing %q", want)
		}
	}
}
