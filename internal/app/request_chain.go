package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudestore "github.com/wsyangzy/Vane/internal/providers/claude/store"
)

func parseEndpointChain(chain string) []chainNode {
	if chain == "" {
		return nil
	}
	var nodes []chainNode
	// JSON 格式
	if strings.HasPrefix(chain, "[") {
		// 尝试结构化对象数组
		if json.Unmarshal([]byte(chain), &nodes) == nil && len(nodes) > 0 {
			fillHopNumbers(nodes)
			return nodes
		}
		// 尝试字符串数组（中间格式）
		var items []string
		if json.Unmarshal([]byte(chain), &items) == nil {
			return keyStringsToNodes(items)
		}
	}
	// 旧格式 " -> " 分隔
	return keyStringsToNodes(strings.Split(chain, " -> "))
}

func fillHopNumbers(nodes []chainNode) {
	for i := range nodes {
		if nodes[i].Hop == 0 {
			nodes[i].Hop = i + 1
		}
	}
}

func keyStringsToNodes(items []string) []chainNode {
	nodes := make([]chainNode, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		hop := len(nodes) + 1
		parts := strings.Split(trimmed, "::")
		if len(parts) >= 3 && strings.TrimSpace(parts[0]) != "" {
			nodes = append(nodes, chainNode{
				Hop:             hop,
				Channel:         strings.TrimSpace(parts[0]),
				CredentialGroup: strings.TrimSpace(parts[1]),
				Endpoint:        strings.TrimSpace(strings.Join(parts[2:], "::")),
			})
		} else if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			nodes = append(nodes, chainNode{
				Hop:      hop,
				Channel:  strings.TrimSpace(parts[0]),
				Endpoint: strings.TrimSpace(parts[1]),
			})
		} else {
			nodes = append(nodes, chainNode{Hop: hop, Endpoint: trimmed})
		}
	}
	return nodes
}

func endpointChainCredentialGroupLookup(
	ctx context.Context,
	endpointManager interface {
		GetEndpoints() []*endpoint.Endpoint
	},
	endpointStore interface {
		List(context.Context) ([]*claudestore.EndpointRecord, error)
	},
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
) map[string]string {
	lookup := make(map[string]string)
	addEndpointManagerCredentialGroupLookup(lookup, endpointManager)
	addEndpointStoreCredentialGroupLookup(ctx, lookup, endpointStore, groupStore)
	return lookup
}

func addEndpointManagerCredentialGroupLookup(lookup map[string]string, endpointManager interface {
	GetEndpoints() []*endpoint.Endpoint
}) {
	if endpointManager == nil {
		return
	}
	for _, ep := range endpointManager.GetEndpoints() {
		if ep == nil {
			continue
		}
		channel := strings.TrimSpace(ep.Config.Channel)
		endpointName := strings.TrimSpace(ep.Config.Name)
		if channel == "" || endpointName == "" {
			continue
		}
		groupName := strings.TrimSpace(ep.Config.CredentialGroupName)
		if groupName == "" {
			groupName = strings.TrimSpace(ep.Config.Group)
		}
		if groupName == "" && ep.Config.CredentialGroupID > 0 {
			groupName = fmt.Sprintf("g%d", ep.Config.CredentialGroupID)
		}
		if groupName == "" {
			continue
		}

		addEndpointChainLookup(lookup, channel, endpointName, groupName)
		addEndpointChainLookup(lookup, channel, ep.Config.URL, groupName)
	}
}

func addEndpointStoreCredentialGroupLookup(
	ctx context.Context,
	lookup map[string]string,
	endpointStore interface {
		List(context.Context) ([]*claudestore.EndpointRecord, error)
	},
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
) {
	if endpointStore == nil {
		return
	}
	records, err := endpointStore.List(ctx)
	if err != nil {
		return
	}
	groupByID := make(map[int64]string)
	defaultGroupByChannel := make(map[string]string)
	for _, record := range records {
		if record == nil {
			continue
		}
		channel := strings.TrimSpace(record.Channel)
		groupName := endpointRecordCredentialGroupName(ctx, record, groupStore, groupByID, defaultGroupByChannel)
		if channel == "" || groupName == "" {
			continue
		}
		addEndpointChainLookup(lookup, channel, record.Name, groupName)
		addEndpointChainLookup(lookup, channel, record.URL, groupName)
	}
}

func endpointRecordCredentialGroupName(
	ctx context.Context,
	record *claudestore.EndpointRecord,
	groupStore interface {
		GetByID(context.Context, int64) (*claudestore.ClaudeCredentialGroupRecord, error)
		GetDefaultByChannel(context.Context, string) (*claudestore.ClaudeCredentialGroupRecord, error)
	},
	groupByID map[int64]string,
	defaultGroupByChannel map[string]string,
) string {
	if record == nil {
		return ""
	}
	if groupName := strings.TrimSpace(record.CredentialGroupName); groupName != "" {
		return groupName
	}
	if groupStore != nil && record.CredentialGroupID > 0 {
		groupName, ok := groupByID[record.CredentialGroupID]
		if !ok {
			if group, err := groupStore.GetByID(ctx, record.CredentialGroupID); err == nil && group != nil {
				groupName = strings.TrimSpace(group.Name)
			}
			groupByID[record.CredentialGroupID] = groupName
		}
		if groupName != "" {
			return groupName
		}
	}
	channel := strings.TrimSpace(record.Channel)
	if groupStore != nil && channel != "" {
		groupName, ok := defaultGroupByChannel[channel]
		if !ok {
			if group, err := groupStore.GetDefaultByChannel(ctx, channel); err == nil && group != nil {
				groupName = strings.TrimSpace(group.Name)
			}
			defaultGroupByChannel[channel] = groupName
		}
		if groupName != "" {
			return groupName
		}
	}
	if record.CredentialGroupID > 0 {
		return fmt.Sprintf("g%d", record.CredentialGroupID)
	}
	return ""
}

func addEndpointChainLookup(lookup map[string]string, channel, endpointName, groupName string) {
	key := endpointChainLookupKey(channel, endpointName)
	if key == "" {
		return
	}
	if existing, ok := lookup[key]; ok && existing != groupName {
		lookup[key] = ""
		return
	}
	lookup[key] = groupName
}

func enrichEndpointChainCredentialGroups(provider string, nodes []chainNode, lookup map[string]string) {
	if len(nodes) == 0 || len(lookup) == 0 || provider == "codex" {
		return
	}
	for i := range nodes {
		if strings.TrimSpace(nodes[i].CredentialGroup) != "" {
			continue
		}
		groupName := lookup[endpointChainLookupKey(nodes[i].Channel, nodes[i].Endpoint)]
		if groupName == "" {
			continue
		}
		nodes[i].CredentialGroup = groupName
	}
}

func endpointChainLookupKey(channel, endpointName string) string {
	channel = strings.TrimSpace(channel)
	endpointName = strings.TrimSpace(endpointName)
	if channel == "" || endpointName == "" {
		return ""
	}
	return channel + "\x00" + endpointName
}
