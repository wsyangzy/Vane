package endpoint

import (
	"fmt"
	"github.com/wsyangzy/Vane/internal/routing"
)

func (m *Manager) SetPoolRuntime(runtime *routing.PoolRuntime) { m.poolRuntime.Store(runtime) }
func (m *Manager) PoolRuntime() *routing.PoolRuntime {
	if m == nil {
		return nil
	}
	return m.poolRuntime.Load()
}

// Pools own availability; health checks remain diagnostic. Old failover=false
// endpoints remain historical exclusions until explicitly restored.
func (m *Manager) PoolEndpoints(channel string, groupID int64) []*Endpoint {
	return m.resourceEndpoints(channel, groupID, true)
}

// ProfileEndpoints resolves resources authorized by a dispatch profile without
// applying the legacy failover switches. The profile owns their participation.
func (m *Manager) ProfileEndpoints(channel string, groupID int64) []*Endpoint {
	return m.resourceEndpoints(channel, groupID, false)
}

func (m *Manager) resourceEndpoints(channel string, groupID int64, legacy bool) []*Endpoint {
	var candidates []*Endpoint
	for _, ep := range m.GetAllEndpoints() {
		if ep == nil || ChannelKey(ep) != channel || ep.Config.CredentialGroupID != groupID {
			continue
		}
		if legacy && ep.Config.FailoverEnabled != nil && !*ep.Config.FailoverEnabled {
			continue
		}
		candidates = append(candidates, ep)
	}
	return m.sortHealthyEndpoints(candidates, false)
}

func (m *Manager) CheckPoolEndpoint(channel string, group int64, name string) error {
	for _, ep := range m.GetAllEndpoints() {
		if ChannelKey(ep) == channel && ep.Config.CredentialGroupID == group && ep.Config.Name == name {
			m.checkEndpointHealthForced(ep)
			return nil
		}
	}
	return fmt.Errorf("端点不存在")
}
