package handlers

import (
	"fmt"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
)

func nextFailoverLoopState(loopCount int, failedChannel, newChannel string, triedChannels, triedEndpoints map[string]struct{}) (int, bool, bool) {
	_, alreadyTried := triedChannels[newChannel]
	isSameChannel := failedChannel != "" && failedChannel == newChannel
	if !alreadyTried && !isSameChannel {
		return loopCount, alreadyTried, isSameChannel
	}

	clearStringSet(triedChannels)
	clearStringSet(triedEndpoints)
	return loopCount + 1, alreadyTried, isSameChannel
}

func clearStringSet(values map[string]struct{}) {
	for key := range values {
		delete(values, key)
	}
}

func stringSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func endpointKeys(endpoints []*endpoint.Endpoint) []string {
	keys := make([]string, 0, len(endpoints))
	seen := make(map[string]struct{}, len(endpoints))
	for _, ep := range endpoints {
		if ep == nil {
			continue
		}
		key := endpoint.EndpointKeyForConfig(ep.Config)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func endpointRouteName(ep *endpoint.Endpoint) string {
	if ep == nil {
		return ""
	}
	if ep.Config.Channel != "" {
		return ep.Config.Channel
	}
	return ep.Config.Name
}

func endpointDisplayGroupName(ep *endpoint.Endpoint) string {
	if ep == nil {
		return ""
	}
	if ep.Config.CredentialGroupName != "" {
		return ep.Config.CredentialGroupName
	}
	if ep.Config.Group != "" {
		return ep.Config.Group
	}
	if ep.Config.CredentialGroupID > 0 {
		return fmt.Sprintf("g%d", ep.Config.CredentialGroupID)
	}
	return endpointRouteName(ep)
}

func failoverTermination(err error) (terminate bool, loopLimit bool, errText string) {
	if err == nil {
		return false, false, ""
	}
	errText = err.Error()
	terminate = strings.Contains(errText, "没有可用的故障转移渠道") ||
		strings.Contains(errText, "所有渠道均已尝试")
	loopLimit = strings.Contains(errText, "共循环") ||
		strings.Contains(errText, "最大循环轮数")
	return terminate, loopLimit, errText
}
