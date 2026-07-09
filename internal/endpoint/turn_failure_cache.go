package endpoint

import (
	"context"
	"strings"
	"time"
)

const failedCredentialGroupCacheIdle = 30 * time.Minute

type turnFailureContextKey struct{}

type credentialGroupRef struct {
	ID      int64
	Name    string
	Channel string
}

func (g credentialGroupRef) valid() bool {
	return g.ID > 0 || g.Name != ""
}

// ContextWithTurnFailureKey attaches the current user-turn fingerprint to endpoint selection.
func ContextWithTurnFailureKey(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, turnFailureContextKey{}, key)
}

func TurnFailureKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(turnFailureContextKey{}).(string)
	return strings.TrimSpace(key)
}

func credentialGroupRefFromEndpoint(ep *Endpoint) credentialGroupRef {
	if ep == nil {
		return credentialGroupRef{}
	}
	return credentialGroupRef{
		ID:      ep.Config.CredentialGroupID,
		Name:    strings.TrimSpace(ep.Config.CredentialGroupName),
		Channel: strings.TrimSpace(ep.Config.Channel),
	}
}

func sameCredentialGroup(ep *Endpoint, group credentialGroupRef) bool {
	if ep == nil || !group.valid() {
		return false
	}
	if group.ID > 0 {
		return ep.Config.CredentialGroupID == group.ID
	}
	return strings.TrimSpace(ep.Config.Channel) == group.Channel &&
		strings.TrimSpace(ep.Config.CredentialGroupName) == group.Name
}

func credentialGroupSkipped(ep *Endpoint, groups map[credentialGroupRef]struct{}) bool {
	if ep == nil || len(groups) == 0 {
		return false
	}
	for group := range groups {
		if sameCredentialGroup(ep, group) {
			return true
		}
	}
	return false
}

func (m *Manager) failedCredentialGroupsForTurn(turnKey string) map[credentialGroupRef]struct{} {
	if m == nil {
		return nil
	}
	now := time.Now()

	m.turnFailureCacheMu.Lock()
	defer m.turnFailureCacheMu.Unlock()

	if !m.prepareTurnFailureCacheLocked(turnKey, now) || len(m.turnFailedCredentialRefs) == 0 {
		return nil
	}
	groups := make(map[credentialGroupRef]struct{}, len(m.turnFailedCredentialRefs))
	for group := range m.turnFailedCredentialRefs {
		groups[group] = struct{}{}
	}
	return groups
}

// RememberFailedCredentialGroupForTurn records a credential group failure for the current answer/user turn.
func (m *Manager) RememberFailedCredentialGroupForTurn(turnKey string, ep *Endpoint) bool {
	if m == nil {
		return false
	}
	group := credentialGroupRefFromEndpoint(ep)
	if !group.valid() {
		return false
	}
	now := time.Now()

	m.turnFailureCacheMu.Lock()
	defer m.turnFailureCacheMu.Unlock()

	if !m.prepareTurnFailureCacheLocked(turnKey, now) {
		return false
	}
	m.turnFailedCredentialRefs[group] = struct{}{}
	return true
}

func (m *Manager) prepareTurnFailureCacheLocked(turnKey string, now time.Time) bool {
	turnKey = strings.TrimSpace(turnKey)
	if m.turnFailedCredentialRefs == nil {
		m.turnFailedCredentialRefs = make(map[credentialGroupRef]struct{})
	}
	if turnKey != "" {
		if m.turnFailureCacheKey != turnKey {
			m.turnFailureCacheKey = turnKey
			m.turnFailedCredentialRefs = make(map[credentialGroupRef]struct{})
		}
		m.turnFailureCacheTouched = now
		return true
	}
	if m.turnFailureCacheKey == "" {
		return false
	}
	if !m.turnFailureCacheTouched.IsZero() && now.Sub(m.turnFailureCacheTouched) > failedCredentialGroupCacheIdle {
		m.turnFailureCacheKey = ""
		m.turnFailedCredentialRefs = make(map[credentialGroupRef]struct{})
		return false
	}
	m.turnFailureCacheTouched = now
	return true
}

// EndpointKeysInCredentialGroup returns runtime endpoint keys that share ep's credential group.
func (m *Manager) EndpointKeysInCredentialGroup(ep *Endpoint) []string {
	if m == nil || ep == nil {
		return nil
	}
	group := credentialGroupRefFromEndpoint(ep)
	if !group.valid() {
		return nil
	}
	endpoints := m.GetAllEndpoints()
	keys := make([]string, 0, len(endpoints))
	for _, candidate := range endpoints {
		if sameCredentialGroup(candidate, group) {
			if key := endpointKeyFromConfig(candidate.Config); key != "" {
				keys = append(keys, key)
			}
		}
	}
	return keys
}
