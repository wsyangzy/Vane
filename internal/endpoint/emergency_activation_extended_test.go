package endpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-relay-forwarder/config"
)

// 冷却机制已移除，TestEmergencyActivationCooldownScenarios 测试已删除

// TestEmergencyActivationEdgeCases 测试应急激活的边缘情况
func TestEmergencyActivationEdgeCases(t *testing.T) {
	cfg := &config.Config{
		Group: config.GroupConfig{
			Cooldown:                time.Minute,
			AutoSwitchBetweenGroups: false,
		},
	}

	gm := NewGroupManager(cfg)

	t.Run("不存在的组应该返回错误", func(t *testing.T) {
		err := gm.ManualActivateGroupWithForce("nonexistent-group", true)
		assert.Error(t, err, "不存在的组应该返回错误")
		assert.Contains(t, err.Error(), "组不存在", "错误消息应该提到组不存在")
	})

	t.Run("空组名应该在Web API层面被拦截", func(t *testing.T) {
		// 这个测试验证空组名的处理逻辑
		// 实际情况下这会在Web API层面被拦截，这里测试底层行为
		err := gm.ManualActivateGroupWithForce("", true)
		assert.Error(t, err, "空组名应该返回错误")
	})

	t.Run("没有端点的组应该能正常处理", func(t *testing.T) {
		// 创建一个没有端点的组
		emptyEndpoints := []*Endpoint{}
		gm.UpdateGroups(emptyEndpoints)

		// 尝试激活不存在端点的组
		err := gm.ManualActivateGroupWithForce("empty-group", true)
		assert.Error(t, err, "空组应该返回错误")
	})
}

// TestEmergencyActivationComprehensiveAPI 测试应急激活的完整API响应
// 一端点一组架构
func TestEmergencyActivationComprehensiveAPI(t *testing.T) {
	cfg := &config.Config{
		Group: config.GroupConfig{
			Cooldown:                time.Minute,
			AutoSwitchBetweenGroups: false,
		},
		Failover: config.FailoverConfig{
			Enabled:         false,
			DefaultCooldown: time.Minute,
		},
	}

	gm := NewGroupManager(cfg)

	// 创建端点用于测试，每个端点自动成为一个独立的组
	endpoints := []*Endpoint{
		{
			Config: config.EndpointConfig{
				Name:     "healthy-endpoint",
				URL:      "https://healthy.example.com",
				Priority: 1,
			},
			Status: EndpointStatus{
				Healthy: true,
			},
		},
		{
			Config: config.EndpointConfig{
				Name:     "unhealthy-endpoint",
				URL:      "https://unhealthy.example.com",
				Priority: 2,
			},
			Status: EndpointStatus{
				Healthy: false,
			},
		},
	}

	gm.UpdateGroups(endpoints)

	t.Run("综合测试所有组的can_force_activate状态", func(t *testing.T) {
		// 确保没有组是活跃的
		for _, group := range gm.groups {
			group.IsActive = false
			group.ManuallyPaused = false
			// CooldownUntil 已移除
		}

		// 组名就是端点名
		gm.ManualPauseGroup("healthy-endpoint", 0)
		gm.ManualPauseGroup("unhealthy-endpoint", 0)

		details := gm.GetGroupDetails()
		groupsData, ok := details["groups"].([]map[string]interface{})
		require.True(t, ok, "应该返回groups数组")

		for _, groupData := range groupsData {
			groupName := groupData["name"].(string)
			canForceActivate := groupData["can_force_activate"].(bool)
			healthyEndpoints := groupData["healthy_endpoints"].(int)
			isActive := groupData["is_active"].(bool)

			if groupName == "healthy-endpoint" {
				assert.False(t, canForceActivate, "有健康端点的组不应该能强制激活")
				assert.Greater(t, healthyEndpoints, 0, "healthy-endpoint应该有健康端点")
			} else if groupName == "unhealthy-endpoint" {
				assert.True(t, canForceActivate, "无健康端点的非活跃组应该能强制激活")
				assert.Equal(t, 0, healthyEndpoints, "unhealthy-endpoint应该没有健康端点")
			}

			assert.False(t, isActive, "所有组都应该是非活跃状态")

			t.Logf("组 %s: can_force_activate=%v, healthy_endpoints=%d, is_active=%v",
				groupName, canForceActivate, healthyEndpoints, isActive)
		}
	})

	t.Run("测试强制激活后状态变化", func(t *testing.T) {
		// 强制激活unhealthy-endpoint（组名=端点名）
		err := gm.ManualActivateGroupWithForce("unhealthy-endpoint", true)
		require.NoError(t, err, "应该成功强制激活")

		details := gm.GetGroupDetails()
		groupsData, ok := details["groups"].([]map[string]interface{})
		require.True(t, ok, "应该返回groups数组")

		var unhealthyGroupData, healthyGroupData map[string]interface{}
		for _, groupData := range groupsData {
			if groupData["name"] == "unhealthy-endpoint" {
				unhealthyGroupData = groupData
			} else if groupData["name"] == "healthy-endpoint" {
				healthyGroupData = groupData
			}
		}

		require.NotNil(t, unhealthyGroupData, "应该找到unhealthy-endpoint")
		require.NotNil(t, healthyGroupData, "应该找到healthy-endpoint")

		// 验证强制激活的组
		assert.True(t, unhealthyGroupData["is_active"].(bool), "unhealthy-endpoint应该已激活")
		assert.True(t, unhealthyGroupData["forced_activation"].(bool), "应该标记为强制激活")
		assert.Equal(t, "forced", unhealthyGroupData["activation_type"], "激活类型应该是forced")
		assert.False(t, unhealthyGroupData["can_force_activate"].(bool), "已激活组不能再强制激活")
		assert.NotEmpty(t, unhealthyGroupData["forced_activation_time"], "应该有强制激活时间")

		// 验证其他组
		assert.False(t, healthyGroupData["is_active"].(bool), "healthy-endpoint应该非活跃")
		assert.False(t, healthyGroupData["forced_activation"].(bool), "healthy-endpoint不应该标记为强制激活")
		assert.Equal(t, "normal", healthyGroupData["activation_type"], "激活类型应该是normal")

		t.Logf("✅ 强制激活后状态验证成功")
	})
}