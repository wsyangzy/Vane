package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
)

// 显式激活不受健康检查限制；force 参数只为旧调用方保留。
func TestForceActivation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		healthy bool
		force   bool
	}{
		{name: "healthy", healthy: true},
		{name: "unhealthy"},
		{name: "healthy with legacy force", healthy: true, force: true},
		{name: "unhealthy with legacy force", force: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gm := NewGroupManager(&config.Config{})
			ep := &Endpoint{
				Config: config.EndpointConfig{Name: "test-endpoint"},
				Status: EndpointStatus{Healthy: tt.healthy},
			}
			gm.UpdateGroups([]*Endpoint{ep})
			require.Empty(t, gm.GetActiveGroups(), "构造管理器不应自动激活渠道")

			if tt.force {
				require.NoError(t, gm.ManualActivateGroupWithForce("test-endpoint", true))
			} else {
				require.NoError(t, gm.ManualActivateGroup("test-endpoint"))
			}

			active := gm.GetActiveGroups()
			require.Len(t, active, 1)
			assert.Equal(t, "test-endpoint", active[0].Name)
			assert.False(t, active[0].ManualActivationTime.IsZero())
			assert.False(t, active[0].ForcedActivation)
			assert.True(t, active[0].ForcedActivationTime.IsZero())
			assert.Equal(t, tt.healthy, ep.IsHealthy(), "激活不能伪造已完成的健康检查结果")
		})
	}
}
