package endpoint

import (
	"testing"
	"time"

	"ai-relay-forwarder/config"
)

func TestGetHealthyEndpoints_PriorityScopedToActiveChannel(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid", Channel: "A", Priority: 10, Timeout: 1 * time.Second},
			{Name: "a2", URL: "http://example.invalid", Channel: "A", Priority: 1, Timeout: 1 * time.Second},
			{Name: "b1", URL: "http://example.invalid", Channel: "B", Priority: 0, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	healthy := m.GetHealthyEndpoints()
	if len(healthy) != 2 {
		t.Fatalf("expected 2 healthy endpoints in active channel A, got %d", len(healthy))
	}
	if healthy[0].Config.Name != "a2" || healthy[1].Config.Name != "a1" {
		t.Fatalf("unexpected order: got [%s, %s], want [a2, a1]",
			healthy[0].Config.Name, healthy[1].Config.Name)
	}
}

func TestGetHealthyEndpoints_PriorityKeepsCredentialGroupsContiguous(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "default-p5", URL: "http://example.invalid/default-p5", Channel: "A", CredentialGroupOrder: 0, Priority: 5, Timeout: 1 * time.Second},
			{Name: "github-p0", URL: "http://example.invalid/github-p0", Channel: "A", CredentialGroupOrder: 1, Priority: 0, Timeout: 1 * time.Second},
			{Name: "default-p6", URL: "http://example.invalid/default-p6", Channel: "A", CredentialGroupOrder: 0, Priority: 6, Timeout: 1 * time.Second},
			{Name: "linuxdo-p0", URL: "http://example.invalid/linuxdo-p0", Channel: "A", CredentialGroupOrder: 2, Priority: 0, Timeout: 1 * time.Second},
			{Name: "github-p1", URL: "http://example.invalid/github-p1", Channel: "A", CredentialGroupOrder: 1, Priority: 1, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	healthy := m.GetHealthyEndpoints()
	got := endpointNames(healthy)
	want := []string{"default-p5", "default-p6", "github-p0", "github-p1", "linuxdo-p0"}
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected order: got %v, want %v", got, want)
	}
}

func TestGetHealthyEndpoints_PriorityKeepsEnabledEndpointStickyAfterPrioritySwap(t *testing.T) {
	enabledTrue := true
	enabledFalse := false
	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "active-after-swap", URL: "http://example.invalid/active", Channel: "A", Priority: 3, Enabled: &enabledTrue, Timeout: 1 * time.Second},
			{Name: "edited-to-zero", URL: "http://example.invalid/edited", Channel: "A", Priority: 0, Enabled: &enabledFalse, Timeout: 1 * time.Second},
			{Name: "fallback", URL: "http://example.invalid/fallback", Channel: "A", Priority: 4, Enabled: &enabledFalse, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	healthy := m.GetHealthyEndpoints()
	got := endpointNames(healthy)
	want := []string{"active-after-swap", "edited-to-zero", "fallback"}
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected order: got %v, want %v", got, want)
	}
}

func TestGetHealthyEndpoints_FastestKeepsCredentialGroupsContiguous(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "fastest"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "default-slow", URL: "http://example.invalid/default-slow", Channel: "A", CredentialGroupOrder: 0, Priority: 1, Timeout: 1 * time.Second},
			{Name: "github-fast", URL: "http://example.invalid/github-fast", Channel: "A", CredentialGroupOrder: 1, Priority: 1, Timeout: 1 * time.Second},
			{Name: "default-fast", URL: "http://example.invalid/default-fast", Channel: "A", CredentialGroupOrder: 0, Priority: 2, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		switch ep.Config.Name {
		case "default-slow":
			ep.Status.ResponseTime = 900 * time.Millisecond
		case "default-fast":
			ep.Status.ResponseTime = 300 * time.Millisecond
		case "github-fast":
			ep.Status.ResponseTime = 10 * time.Millisecond
		}
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	healthy := m.GetHealthyEndpoints()
	got := endpointNames(healthy)
	want := []string{"default-fast", "default-slow", "github-fast"}
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected order: got %v, want %v", got, want)
	}
}

func TestGetHealthyEndpoints_FailoverEnabledScopedToActiveChannel(t *testing.T) {
	failoverFalse := false
	failoverTrue := true

	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid", Channel: "A", Priority: 1, FailoverEnabled: &failoverFalse, Timeout: 1 * time.Second},
			{Name: "a2", URL: "http://example.invalid", Channel: "A", Priority: 2, FailoverEnabled: &failoverTrue, Timeout: 1 * time.Second},
			{Name: "b1", URL: "http://example.invalid", Channel: "B", Priority: 0, FailoverEnabled: &failoverTrue, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	healthy := m.GetHealthyEndpoints()
	if len(healthy) != 1 {
		t.Fatalf("expected 1 healthy endpoint in active channel A (a2 only), got %d", len(healthy))
	}
	if healthy[0].Config.Name != "a2" {
		t.Fatalf("unexpected endpoint: got %s, want a2", healthy[0].Config.Name)
	}

	// 组级暂停：只有组内全部 failover_enabled=false 才应暂停
	var groupA, groupB *GroupInfo
	for _, g := range m.groupManager.GetAllGroups() {
		if g.Name == "A" {
			groupA = g
		}
		if g.Name == "B" {
			groupB = g
		}
	}
	if groupA == nil || groupB == nil {
		t.Fatalf("expected groups A and B to exist")
	}
	if groupA.ManuallyPaused {
		t.Fatalf("group A should not be ManuallyPaused when at least one endpoint participates in failover")
	}
	if groupB.ManuallyPaused {
		t.Fatalf("group B should not be ManuallyPaused when its endpoint participates in failover")
	}
}

func TestGetHealthyEndpoints_IgnoresRequestFailedForNewRequests(t *testing.T) {
	cfg := &config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Health:   config.HealthConfig{Timeout: 5 * time.Second},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", URL: "http://example.invalid", Channel: "A", Priority: 1, Timeout: 1 * time.Second},
			{Name: "a2", URL: "http://example.invalid", Channel: "A", Priority: 2, Timeout: 1 * time.Second},
		},
	}

	m := NewManager(cfg)
	for _, ep := range m.GetAllEndpoints() {
		ep.mutex.Lock()
		ep.Status.Healthy = true
		ep.Status.NeverChecked = false
		ep.mutex.Unlock()
	}

	if err := m.ManualActivateGroup("A"); err != nil {
		t.Fatalf("ManualActivateGroup(A) error: %v", err)
	}

	// 模拟上一条请求中 a1 失败过，但这不应污染新的请求候选池。
	m.GetAllEndpoints()[0].mutex.Lock()
	m.GetAllEndpoints()[0].Status.RequestFailed = true
	m.GetAllEndpoints()[0].mutex.Unlock()

	healthy := m.GetHealthyEndpoints()
	if len(healthy) != 2 {
		t.Fatalf("expected both endpoints to remain routable for new requests, got %d", len(healthy))
	}
	if healthy[0].Config.Name != "a1" || healthy[1].Config.Name != "a2" {
		t.Fatalf("unexpected order: got [%s, %s], want [a1, a2]",
			healthy[0].Config.Name, healthy[1].Config.Name)
	}
}

func endpointNames(endpoints []*Endpoint) []string {
	names := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep == nil {
			names = append(names, "")
			continue
		}
		names = append(names, ep.Config.Name)
	}
	return names
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
