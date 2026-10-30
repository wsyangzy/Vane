package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/privacy"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

// The dispatch/protection histories own their final outcomes. Join by exact
// request identity instead of guessing from the current default profile or the
// most recent 100 diagnostic entries. This also recovers older usage records
// whose terminal snapshots were published after hot-pool archival.
func (a *App) enrichRequestDiagnostics(ctx context.Context, records []RequestRecord) {
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil || len(records) == 0 {
		return
	}
	ids := map[string][]string{}
	for _, record := range records {
		provider := record.Provider
		if provider == "" {
			provider = "claude"
		}
		ids[provider] = append(ids[provider], record.RequestID)
	}
	traces := map[string]map[string]routing.DispatchTrace{}
	activities := map[string]map[string]privacy.Activity{}
	for provider, requestIDs := range ids {
		// Optional diagnostic history must not make an otherwise valid request
		// page unavailable (e.g. an older database without these tables).
		traces[provider], _ = routingstore.NewProfileStore(db, buildinfo.BuildMode).TracesForRequests(ctx, provider, requestIDs)
		activities[provider], _ = privacy.NewStore(db).ActivitiesForRequests(ctx, provider, requestIDs)
	}
	resources := map[routing.Candidate]chainNode{}
	for i := range records {
		record := &records[i]
		provider := record.Provider
		if provider == "" {
			provider = "claude"
		}
		if trace, ok := traces[provider][record.RequestID]; ok && trace.ProfileID > 0 {
			var captured routing.DispatchTrace
			if json.Unmarshal([]byte(record.RouteSnapshot), &captured) == nil && captured.RequestID == record.RequestID && captured.Provider == provider && captured.ProfileID == trace.ProfileID {
				// While another attempt is running, the usage hot pool can be
				// newer than the last independently persisted settlement.
				if captured.Attempts > trace.Attempts || captured.Attempts == trace.Attempts && (captured.FinishedAt != nil && trace.FinishedAt == nil || len(captured.Candidates) > len(trace.Candidates)) {
					trace = captured
				}
			}
			if data, err := json.Marshal(trace); err == nil {
				record.RouteSnapshot = string(data)
			}
			if trace.Attempts > 0 && trace.Attempts-1 > record.RetryCount {
				record.RetryCount = trace.Attempts - 1
			}
			var last *routing.Candidate
			for _, event := range trace.Candidates {
				if event.Action == "attempt" {
					candidate := event.Candidate
					last = &candidate
				}
			}
			if last != nil && (record.Endpoint == "" || record.Group == "" || record.Channel == "") {
				missingEndpoint := record.Endpoint == ""
				resource, ok := resources[*last]
				if !ok {
					resource = a.requestDispatchResource(ctx, *last)
					resources[*last] = resource
				}
				if record.Endpoint == "" {
					record.Endpoint = resource.Endpoint
				}
				if record.Group == "" {
					record.Group = resource.CredentialGroup
				}
				// The initial channel used to be recorded even when the profile
				// actually sent elsewhere. Without a captured chain, use the
				// selected candidate from this request's trace.
				if missingEndpoint && len(record.EndpointChain) == 0 || record.Channel == "" {
					record.Channel = resource.Channel
				}
			}
		}
		if activity, ok := activities[provider][record.RequestID]; ok {
			var summary map[string]any
			_ = json.Unmarshal([]byte(record.PrivacyRedactionSummary), &summary)
			if summary == nil {
				summary = map[string]any{}
			}
			// Add metadata to the existing opaque summary. The Wails contract
			// stays stable and older consumers keep their original hit summary.
			summary["activity"] = activity
			if data, err := json.Marshal(summary); err == nil {
				record.PrivacyRedactionSummary = string(data)
			}
		}
	}
}

// Only names are resolved from current configuration; historical decisions
// always come from the trace. Deleted resources retain their recorded IDs.
func (a *App) requestDispatchResource(ctx context.Context, candidate routing.Candidate) chainNode {
	resource := chainNode{Channel: "渠道 #" + candidate.Channel, Endpoint: "端点 #" + candidate.Endpoint}
	channelID, _ := strconv.ParseInt(candidate.Channel, 10, 64)
	a.mu.RLock()
	cm, cs, es, gs := a.codexEndpointMgr, a.codexChannelStore, a.codexEndpointStore, a.codexCredentialGroupStore
	claudeManager := a.endpointManager
	claudeGroups := a.claudeCredentialGroupStore
	a.mu.RUnlock()
	if candidate.Provider == "codex" {
		var groupID int64
		if credential := strings.Split(candidate.Credential, "/"); len(credential) == 2 {
			groupID, _ = strconv.ParseInt(credential[1], 10, 64)
		}
		if cm != nil {
			if channel := cm.ChannelByID(channelID); channel != nil {
				resource.Channel = channel.Name
			}
			for _, ep := range cm.PoolEndpoints(channelID, 0) {
				if fmt.Sprint(ep.ID) == candidate.Endpoint {
					resource.Endpoint = ep.Name
					if groupID == 0 || groupID == ep.CredentialGroupID {
						resource.CredentialGroup = ep.CredentialGroupName
						groupID = ep.CredentialGroupID
					}
					break
				}
			}
		}
		if cs != nil {
			if channel, err := cs.GetByID(ctx, channelID); err == nil && channel != nil {
				resource.Channel = channel.Name
			}
		}
		if es != nil {
			endpointID, _ := strconv.ParseInt(candidate.Endpoint, 10, 64)
			if ep, err := es.GetByID(ctx, endpointID); err == nil && ep != nil && ep.ChannelID == channelID {
				resource.Endpoint = ep.Name
				if groupID == 0 {
					groupID = ep.CredentialGroupID
				}
			}
		}
		if groupID > 0 && gs != nil {
			if group, err := gs.GetByID(ctx, groupID); err == nil && group != nil && group.ChannelID == channelID {
				resource.CredentialGroup = group.Name
			}
		}
	} else {
		parts := strings.Split(candidate.Endpoint, "::")
		if len(parts) >= 2 {
			resource.Channel, resource.Endpoint = parts[0], parts[len(parts)-1]
		}
		if claudeManager != nil {
			for _, ep := range claudeManager.GetAllEndpoints() {
				if ep.Config.RuntimeKey == candidate.Endpoint || ep.Config.Channel+"::"+ep.Config.Name == candidate.Endpoint {
					resource.Channel, resource.Endpoint, resource.CredentialGroup = ep.Config.Channel, ep.Config.Name, ep.Config.CredentialGroupName
					break
				}
			}
		}
		var groupID int64
		if len(parts) >= 3 && strings.HasPrefix(parts[1], "g") {
			groupID, _ = strconv.ParseInt(strings.TrimPrefix(parts[1], "g"), 10, 64)
		} else if credential := strings.Split(candidate.Credential, "/"); len(credential) == 2 {
			groupID, _ = strconv.ParseInt(credential[1], 10, 64)
		}
		if resource.CredentialGroup == "" && claudeGroups != nil && groupID > 0 {
			if group, err := claudeGroups.GetByID(ctx, groupID); err == nil && group != nil && group.Channel == resource.Channel {
				resource.CredentialGroup = group.Name
			}
		}
	}
	if resource.CredentialGroup == "" {
		// Static credential identities use channel/group IDs. Managed account
		// hashes deliberately remain internal and never become a UI label.
		parts := strings.Split(candidate.Credential, "/")
		if len(parts) == 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil && id > 0 {
				resource.CredentialGroup = fmt.Sprintf("分组 #%d", id)
			}
		}
	}
	return resource
}
