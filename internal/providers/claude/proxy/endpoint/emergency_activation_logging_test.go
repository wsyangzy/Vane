package endpoint

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/config"
)

// 显式激活以 INFO 记录健康诊断，旧 force 参数不会产生安全告警或拒绝操作。
func TestEmergencyActivationLogging(t *testing.T) {
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
			gm.UpdateGroups([]*Endpoint{{
				Config: config.EndpointConfig{Name: "test-endpoint"},
				Status: EndpointStatus{Healthy: tt.healthy},
			}})

			var buffer bytes.Buffer
			originalLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buffer, nil)))
			t.Cleanup(func() { slog.SetDefault(originalLogger) })

			require.NoError(t, gm.ManualActivateGroupWithForce("test-endpoint", tt.force))
			type logEntry struct {
				Time    string `json:"time"`
				Level   string `json:"level"`
				Message string `json:"msg"`
			}
			var entries []logEntry
			for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
				var entry logEntry
				require.NoError(t, json.Unmarshal([]byte(line), &entry))
				entries = append(entries, entry)
			}
			require.Len(t, entries, 1, "每次显式激活只需一条健康诊断日志")
			entry := entries[0]
			assert.Equal(t, "INFO", entry.Level)
			_, err := time.Parse(time.RFC3339Nano, entry.Time)
			require.NoError(t, err, "日志必须带有效时间戳")
			assert.Contains(t, entry.Message, "手动激活")
			assert.Contains(t, entry.Message, "test-endpoint")
			if tt.healthy {
				assert.Contains(t, entry.Message, "健康端点: 1/1")
			} else {
				assert.Contains(t, entry.Message, "无健康端点组")
				assert.Contains(t, entry.Message, "健康端点: 0/1")
			}
		})
	}
}
