package endpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
)

// 用实际 Manager API 验证用户手动重试。旧的测试自建 HTTP handler
// 并未调用产品 API，也无法覆盖渠道激活和请求失败状态的恢复。
func TestEmergencyActivationScenarios(t *testing.T) {
	m := NewManager(&config.Config{
		Strategy: config.StrategyConfig{Type: "priority"},
		Endpoints: []config.EndpointConfig{
			{Name: "a1", Channel: "A", Priority: 1},
			{Name: "a2", Channel: "A", Priority: 2},
			{Name: "b1", Channel: "B", Priority: 1},
		},
	})
	defer m.Stop()
	endpoints := m.GetAllEndpoints()
	require.Len(t, endpoints, 3)

	require.NoError(t, m.ManualActivateGroup("B"))
	previousActivation := m.GetGroupManager().GetActiveGroups()[0].ManualActivationTime
	// 模拟渠道 A 的真实请求已经失败，等待用户点击重试。
	for _, ep := range endpoints[:2] {
		ep.Status.Healthy = false
		ep.Status.NeverChecked = false
		ep.Status.RequestFailed = true
	}
	beforeRetry := time.Now()
	require.NoError(t, m.ManualActivateGroup("A"), "无健康端点的渠道也必须允许显式重试")
	assert.True(t, m.GetGroupManager().WasGroupManuallyActivatedSince("A", beforeRetry))
	for _, group := range m.GetGroupManager().GetAllGroups() {
		if group.Name == "B" {
			assert.Equal(t, previousActivation, group.ManualActivationTime)
		}
	}

	active := m.GetGroupManager().GetActiveGroups()
	require.Len(t, active, 1)
	assert.Equal(t, "A", active[0].Name, "切换渠道必须停用旧渠道")
	for _, ep := range endpoints[:2] {
		assert.False(t, ep.Status.RequestFailed)
		assert.True(t, ep.Status.NeverChecked, "重试资格仍需真实请求验证")
	}
	available := m.GetHealthyEndpoints()
	require.Len(t, available, 2, "重试后的渠道端点必须重新参与路由")
	assert.Equal(t, "a1", available[0].Config.Name)
	assert.Equal(t, "a2", available[1].Config.Name)

	// 无效激活不能清空当前渠道，也不能递增用于故障转移 CAS 的版本。
	name, revision := m.GetGroupManager().ActiveGroupSnapshot()
	require.ErrorContains(t, m.ManualActivateGroup("missing"), "组不存在")
	currentName, currentRevision := m.GetGroupManager().ActiveGroupSnapshot()
	assert.Equal(t, name, currentName)
	assert.Equal(t, revision, currentRevision)
}
