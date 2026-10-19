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

func TestRequestStateManagerAnnotateEndpointChainError(t *testing.T) {
	manager := NewRequestStateManager("req-chain-error")

	manager.AppendEndpointChain("公益站", "aws", "Ark API")
	chain := manager.AnnotateEndpointChainError(403, `{"error":{"message":"quota exceeded"}}`, map[string]string{"cf-ray": "abc"})

	var nodes []ChainNode
	if err := json.Unmarshal([]byte(chain), &nodes); err != nil {
		t.Fatalf("endpoint chain should be valid JSON: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one chain node, got %d", len(nodes))
	}
	if nodes[0].StatusCode != 403 || nodes[0].Error != `{"error":{"message":"quota exceeded"}}` {
		t.Fatalf("unexpected chain error fields: %#v", nodes[0])
	}
	if nodes[0].Headers["cf-ray"] != "abc" || nodes[0].Local {
		t.Fatalf("upstream hop must keep diagnostic headers and not be marked local: %#v", nodes[0])
	}
}

// 上游只回状态码不带正文（anyrouter 的 520）时也要落到链路上：否则错误卡片只剩一个数字
func TestRequestStateManagerAnnotatesEmptyBodyAndLocalFailures(t *testing.T) {
	manager := NewRequestStateManager("req-chain-empty")
	manager.AppendEndpointChain("anyrouter", "github", "主站直连")
	chain := manager.AnnotateEndpointChainError(520, "", map[string]string{"server": "cloudflare"})
	var nodes []ChainNode
	if err := json.Unmarshal([]byte(chain), &nodes); err != nil {
		t.Fatalf("endpoint chain should be valid JSON: %v", err)
	}
	if nodes[0].StatusCode != 520 || nodes[0].Headers["server"] != "cloudflare" || nodes[0].Local {
		t.Fatalf("empty-body upstream failure must keep status and headers: %#v", nodes[0])
	}

	manager.AppendEndpointChain("anyrouter", "linuxdo", "主站直连")
	chain = manager.AnnotateEndpointChainError(0, "http2: timeout awaiting response headers", nil)
	nodes = nil
	if err := json.Unmarshal([]byte(chain), &nodes); err != nil {
		t.Fatalf("endpoint chain should be valid JSON: %v", err)
	}
	if !nodes[1].Local || nodes[1].StatusCode != 0 {
		t.Fatalf("a failure without an upstream response must be marked local: %#v", nodes[1])
	}
}
