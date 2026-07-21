package endpoint

import (
	"errors"
	"testing"
	"time"

	"ai-relay-forwarder/config"
)

func TestTriggerRequestFailoverWithFailedEndpoints_SelectsNextChannelByPriority(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{
			Type: "fastest",
		},
		Failover: config.FailoverConfig{
			Enabled: true,
		},
		EndpointsStorage: config.EndpointsStorageConfig{
			Type: "yaml",
		},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1, Timeout: 2 * time.Second},
			{Name: "a2", URL: "http://example.invalid/a2", Channel: "A", Priority: 2, Timeout: 2 * time.Second},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 1, Timeout: 2 * time.Second},
			{Name: "c1", URL: "http://example.invalid/c1", Channel: "C", Priority: 1, Timeout: 2 * time.Second},
		},
	}

	mgr := NewManager(cfg)

	// 设定健康状态与响应时间（模拟健康检查历史）
	for _, ep := range mgr.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		switch ep.Config.Channel {
		case "B":
			ep.Status.ResponseTime = 200 * time.Millisecond
		case "C":
			ep.Status.ResponseTime = 50 * time.Millisecond
		default:
			ep.Status.ResponseTime = 150 * time.Millisecond
		}
		ep.mutex.Unlock()
	}

	newChannel, err := mgr.TriggerRequestFailoverWithFailedEndpoints([]string{"a1", "a2"}, "all_retries_exhausted")
	if err != nil {
		t.Fatalf("TriggerRequestFailoverWithFailedEndpoints error: %v", err)
	}
	if newChannel != "B" {
		t.Fatalf("newChannel mismatch: got %q, want %q", newChannel, "B")
	}
}

func TestTriggerRequestFailoverWithFailedEndpoints_UsesChannelPriorityAndSkipHealthOverride(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{
			Type: "fastest",
		},
		Failover: config.FailoverConfig{
			Enabled: true,
		},
		EndpointsStorage: config.EndpointsStorageConfig{
			Type: "yaml",
		},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1, Timeout: 2 * time.Second},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 0, Timeout: 2 * time.Second},
			{Name: "c1", URL: "http://example.invalid/c1", Channel: "C", Priority: 50, SkipHealthCheck: true, Timeout: 2 * time.Second},
		},
	}

	mgr := NewManager(cfg)
	mgr.UpdateChannelPriorities(map[string]int{
		"A": 1,
		"B": 2,
		"C": 20,
	})

	for _, ep := range mgr.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = false
		ep.Status.NeverChecked = false
		switch ep.Config.Channel {
		case "B":
			ep.Status.ResponseTime = 10 * time.Millisecond
		case "C":
			ep.Status.ResponseTime = 200 * time.Millisecond
		}
		ep.mutex.Unlock()
	}

	newChannel, err := mgr.TriggerRequestFailoverWithFailedEndpoints([]string{"a1"}, "all_retries_exhausted")
	if err != nil {
		t.Fatalf("TriggerRequestFailoverWithFailedEndpoints error: %v", err)
	}
	if newChannel != "C" {
		t.Fatalf("newChannel mismatch: got %q, want %q", newChannel, "C")
	}
}

func TestTriggerRequestFailoverWithFailedEndpoints_PrioritySkipsPausedOrCooldown(t *testing.T) {
	failoverFalse := false

	cfg := &config.Config{
		Strategy: config.StrategyConfig{
			Type: "fastest",
		},
		Failover: config.FailoverConfig{
			Enabled: true,
		},
		EndpointsStorage: config.EndpointsStorageConfig{
			Type: "yaml",
		},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1, Timeout: 2 * time.Second},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 1, Timeout: 2 * time.Second},
			// C 渠道端点不参与故障转移 => 组级暂停（ManuallyPaused=true）
			{Name: "c1", URL: "http://example.invalid/c1", Channel: "C", Priority: 1, FailoverEnabled: &failoverFalse, Timeout: 2 * time.Second},
		},
	}

	mgr := NewManager(cfg)

	for _, ep := range mgr.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		switch ep.Config.Channel {
		case "B":
			ep.Status.ResponseTime = 200 * time.Millisecond
		case "C":
			ep.Status.ResponseTime = 10 * time.Millisecond
		}
		ep.mutex.Unlock()
	}

	// 确认 C 组已被标记为暂停（因为全部 failover_enabled=false）
	var groupC *GroupInfo
	for _, g := range mgr.GetGroupManager().GetAllGroups() {
		if g.Name == "C" {
			groupC = g
			break
		}
	}
	if groupC == nil {
		t.Fatalf("expected group C to exist")
	}
	if !groupC.ManuallyPaused {
		t.Fatalf("expected group C to be ManuallyPaused when all endpoints are excluded from failover")
	}

	newChannel, err := mgr.TriggerRequestFailoverWithFailedEndpoints([]string{"a1"}, "all_retries_exhausted")
	if err != nil {
		t.Fatalf("TriggerRequestFailoverWithFailedEndpoints error: %v", err)
	}
	if newChannel != "B" {
		t.Fatalf("newChannel mismatch: got %q, want %q", newChannel, "B")
	}
}

func TestActivateGroupForFailover_RejectsStaleAutomaticSwitch(t *testing.T) {
	cfg := &config.Config{
		Failover: config.FailoverConfig{Enabled: true},
		EndpointsStorage: config.EndpointsStorageConfig{
			Type: "sqlite",
		},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 1},
			{Name: "c1", URL: "http://example.invalid/c1", Channel: "C", Priority: 1},
		},
	}

	mgr := NewManager(cfg)
	gm := mgr.GetGroupManager()
	if err := gm.ManualActivateGroup("A"); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	_, revisionA := gm.ActiveGroupSnapshot()
	if err := gm.ManualActivateGroup("C"); err != nil {
		t.Fatalf("manual activate C: %v", err)
	}

	activeChannel, _, activated, err := gm.ActivateGroupForFailover("B", "A", revisionA, false)
	if err != nil {
		t.Fatalf("stale failover activation: %v", err)
	}
	if activated {
		t.Fatal("stale automatic activation must not replace the newer manual channel")
	}
	if activeChannel != "C" {
		t.Fatalf("active channel = %q, want C", activeChannel)
	}

	activeGroups := gm.GetActiveGroups()
	if len(activeGroups) != 1 || activeGroups[0].Name != "C" {
		t.Fatalf("active groups = %#v, want only C", activeGroups)
	}

	// 即使渠道名称经过 A -> C -> A 回到原值，旧请求携带的版本也必须失效。
	if err := gm.ManualActivateGroup("A"); err != nil {
		t.Fatalf("manual reactivate A: %v", err)
	}
	activeChannel, _, activated, err = gm.ActivateGroupForFailover("B", "A", revisionA, false)
	if err != nil {
		t.Fatalf("ABA stale failover activation: %v", err)
	}
	if activated || activeChannel != "A" {
		t.Fatalf("ABA stale activation result = channel %q, activated %v; want A,false", activeChannel, activated)
	}
}

func TestTriggerRequestFailover_RollsBackRuntimeWhenPersistenceFails(t *testing.T) {
	cfg := &config.Config{
		Failover:         config.FailoverConfig{Enabled: true},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 1},
		},
	}
	mgr := NewManager(cfg)
	gm := mgr.GetGroupManager()
	if err := gm.ManualActivateGroup("A"); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	mgr.SetOnFailoverTriggered(func(_, _ string, _ uint64) (bool, error) {
		return false, errors.New("database unavailable")
	})

	if _, err := mgr.TriggerRequestFailoverWithFailedEndpoints([]string{"a1"}, "test"); err == nil {
		t.Fatal("expected persistence failure")
	}
	activeGroups := gm.GetActiveGroups()
	if len(activeGroups) != 1 || activeGroups[0].Name != "A" {
		t.Fatalf("active groups = %#v, want rolled back A", activeGroups)
	}
}

func TestTriggerRequestFailover_DisabledLoopRetryNeverSwitchesChannel(t *testing.T) {
	cfg := &config.Config{
		Failover: config.FailoverConfig{
			Enabled:   false,
			LoopRetry: true,
			MaxLoops:  3,
		},
		EndpointsStorage: config.EndpointsStorageConfig{Type: "sqlite"},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid/a1", Channel: "A", Priority: 1},
			{Name: "b1", URL: "http://example.invalid/b1", Channel: "B", Priority: 1},
		},
	}
	mgr := NewManager(cfg)
	gm := mgr.GetGroupManager()
	if err := gm.ManualActivateGroup("A"); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	callbackCount := 0
	mgr.SetOnFailoverTriggered(func(_, _ string, _ uint64) (bool, error) {
		callbackCount++
		return true, nil
	})

	if _, err := mgr.TriggerRequestFailoverWithLoopCount([]string{"a1"}, []string{"A"}, "test", 1); err == nil {
		t.Fatal("expected failover-disabled error")
	}
	if callbackCount != 0 {
		t.Fatalf("callback count = %d, want 0", callbackCount)
	}
	activeGroups := gm.GetActiveGroups()
	if len(activeGroups) != 1 || activeGroups[0].Name != "A" {
		t.Fatalf("active groups = %#v, want only A", activeGroups)
	}
}
