package tracking

import (
	"testing"
	"time"
)

func TestConfigureRetentionPolicyDoesNotRequestImmediateMaintenance(t *testing.T) {
	tracker := &UsageTracker{
		config:           &Config{},
		retentionChanged: make(chan struct{}, 1),
	}
	tracker.config.CleanupInterval = 2 * time.Hour
	if err := tracker.ConfigureRetentionPolicy(7); err != nil {
		t.Fatalf("configure retention policy: %v", err)
	}

	select {
	case <-tracker.retentionChanged:
		t.Fatal("startup retention configuration requested immediate maintenance")
	default:
	}
	policy := tracker.currentRetentionPolicy()
	if policy.retentionDays != 7 || policy.cleanupInterval != 2*time.Hour {
		t.Fatalf("unexpected configured policy: %+v", policy)
	}
}

func TestUpdateRetentionPolicyRunsCleanupImmediately(t *testing.T) {
	tracker := newUsageStatsCacheTestTracker(t)
	defer tracker.Close()

	oldStartTime := time.Now().AddDate(0, 0, -3).Format("2006-01-02 15:04:05")
	if _, err := tracker.GetWriteDB().Exec(
		`INSERT INTO request_logs (request_id, start_time, status, provider) VALUES (?, ?, ?, ?)`,
		"retention-hot-update", oldStartTime, "completed", "claude",
	); err != nil {
		t.Fatalf("insert old request: %v", err)
	}

	if err := tracker.UpdateRetentionPolicy(1); err != nil {
		t.Fatalf("update retention policy: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		if err := tracker.GetReadDB().QueryRow(`SELECT COUNT(*) FROM request_logs WHERE request_id = ?`, "retention-hot-update").Scan(&count); err != nil {
			t.Fatalf("query old request: %v", err)
		}
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retention update did not trigger cleanup")
		}
		time.Sleep(20 * time.Millisecond)
	}

	policy := tracker.currentRetentionPolicy()
	if policy.retentionDays != 1 || policy.cleanupInterval != 24*time.Hour {
		t.Fatalf("unexpected applied policy: %+v", policy)
	}
}
