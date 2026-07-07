package proxy

import (
	"encoding/json"
	"testing"
)

func TestRequestStateManagerAppendEndpointChainIncludesCredentialGroup(t *testing.T) {
	manager := NewRequestStateManager("req-chain-group")

	chain := manager.AppendEndpointChain("公益站", "aws", "Ark API")
	chain = manager.AppendEndpointChain("公益站", "aws", "Ark API")
	chain = manager.AppendEndpointChain("Right Code", "AWSQ逆向渠道", "主线路")

	var nodes []ChainNode
	if err := json.Unmarshal([]byte(chain), &nodes); err != nil {
		t.Fatalf("endpoint chain should be valid JSON: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected duplicate route nodes to be collapsed, got %d nodes: %#v", len(nodes), nodes)
	}

	first := nodes[0]
	if first.Hop != 1 || first.Channel != "公益站" || first.CredentialGroup != "aws" || first.Endpoint != "Ark API" {
		t.Fatalf("unexpected first node: %#v", first)
	}

	second := nodes[1]
	if second.Hop != 2 || second.Channel != "Right Code" || second.CredentialGroup != "AWSQ逆向渠道" || second.Endpoint != "主线路" {
		t.Fatalf("unexpected second node: %#v", second)
	}
}
