package main

import (
	"context"
	"reflect"
	"testing"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/endpoint"
	"ai-relay-forwarder/internal/store"
)

type endpointLookupStub struct {
	endpoints []*endpoint.Endpoint
}

func (s endpointLookupStub) GetEndpoints() []*endpoint.Endpoint {
	return s.endpoints
}

type endpointRecordLookupStub struct {
	records []*store.EndpointRecord
}

func (s endpointRecordLookupStub) List(context.Context) ([]*store.EndpointRecord, error) {
	return s.records, nil
}

type credentialGroupLookupStub struct {
	byID     map[int64]*store.ClaudeCredentialGroupRecord
	defaults map[string]*store.ClaudeCredentialGroupRecord
}

func (s credentialGroupLookupStub) GetByID(_ context.Context, id int64) (*store.ClaudeCredentialGroupRecord, error) {
	return s.byID[id], nil
}

func (s credentialGroupLookupStub) GetDefaultByChannel(_ context.Context, channel string) (*store.ClaudeCredentialGroupRecord, error) {
	return s.defaults[channel], nil
}

func TestParseEndpointChain(t *testing.T) {
	tests := []struct {
		name  string
		chain string
		want  []chainNode
	}{
		{
			name:  "empty",
			chain: "",
			want:  nil,
		},
		{
			name:  "structured json fills missing hops",
			chain: `[{"channel":"alpha","endpoint":"ep-a"},{"hop":4,"channel":"beta","endpoint":"ep-b"}]`,
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 4, Channel: "beta", Endpoint: "ep-b"},
			},
		},
		{
			name:  "string array",
			chain: `["alpha::ep-a","beta::ep-b"]`,
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 2, Channel: "beta", Endpoint: "ep-b"},
			},
		},
		{
			name:  "legacy delimiter",
			chain: "alpha::ep-a -> ep-b -> beta::ep-c",
			want: []chainNode{
				{Hop: 1, Channel: "alpha", Endpoint: "ep-a"},
				{Hop: 2, Endpoint: "ep-b"},
				{Hop: 3, Channel: "beta", Endpoint: "ep-c"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEndpointChain(tt.chain)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseEndpointChain() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEnrichEndpointChainCredentialGroups(t *testing.T) {
	nodes := []chainNode{
		{Hop: 1, Channel: "公益站", Endpoint: "Ark API"},
		{Hop: 2, Channel: "foxcode", CredentialGroup: "已有分组", Endpoint: "aws"},
		{Hop: 3, Channel: "Right Code", Endpoint: "AWSQ逆向渠道"},
	}
	lookup := map[string]string{
		endpointChainLookupKey("公益站", "Ark API"):         "默认分组",
		endpointChainLookupKey("foxcode", "aws"):         "不应覆盖",
		endpointChainLookupKey("Right Code", "AWSQ逆向渠道"): "AWSQ逆向渠道",
	}

	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	want := []chainNode{
		{Hop: 1, Channel: "公益站", CredentialGroup: "默认分组", Endpoint: "Ark API"},
		{Hop: 2, Channel: "foxcode", CredentialGroup: "已有分组", Endpoint: "aws"},
		{Hop: 3, Channel: "Right Code", CredentialGroup: "AWSQ逆向渠道", Endpoint: "AWSQ逆向渠道"},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("enrichEndpointChainCredentialGroups() = %#v, want %#v", nodes, want)
	}
}

func TestEndpointChainCredentialGroupLookupIndexesEndpointNameAndURL(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{
			endpoints: []*endpoint.Endpoint{
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "Ark API",
						URL:                 "https://ark.example/v1",
						CredentialGroupName: "默认分组",
					},
				},
			},
		},
		nil,
		nil,
	)

	nodes := []chainNode{
		{Hop: 1, Channel: "公益站", Endpoint: "Ark API"},
		{Hop: 2, Channel: "公益站", Endpoint: "https://ark.example/v1"},
	}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	for _, node := range nodes {
		if node.CredentialGroup != "默认分组" {
			t.Fatalf("credential group for endpoint %q = %q, want 默认分组", node.Endpoint, node.CredentialGroup)
		}
	}
}

func TestEndpointChainCredentialGroupLookupUsesStoredDefaultGroup(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{},
		endpointRecordLookupStub{
			records: []*store.EndpointRecord{
				{Channel: "anyrouter", Name: "大陆网络优化", URL: "https://cn.example/v1"},
				{Channel: "anyrouter", Name: "主站直连", URL: "https://direct.example/v1"},
			},
		},
		credentialGroupLookupStub{
			defaults: map[string]*store.ClaudeCredentialGroupRecord{
				"anyrouter": {Channel: "anyrouter", Name: "默认分组", IsDefault: true},
			},
		},
	)

	nodes := []chainNode{
		{Hop: 1, Channel: "anyrouter", Endpoint: "大陆网络优化"},
		{Hop: 2, Channel: "anyrouter", Endpoint: "主站直连"},
		{Hop: 3, Channel: "anyrouter", Endpoint: "https://direct.example/v1"},
	}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	for _, node := range nodes {
		if node.CredentialGroup != "默认分组" {
			t.Fatalf("credential group for endpoint %q = %q, want 默认分组", node.Endpoint, node.CredentialGroup)
		}
	}
}

func TestEndpointChainCredentialGroupLookupSkipsAmbiguousEndpoint(t *testing.T) {
	lookup := endpointChainCredentialGroupLookup(
		context.Background(),
		endpointLookupStub{
			endpoints: []*endpoint.Endpoint{
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "shared",
						CredentialGroupName: "默认分组",
					},
				},
				{
					Config: config.EndpointConfig{
						Channel:             "公益站",
						Name:                "shared",
						CredentialGroupName: "备用分组",
					},
				},
			},
		},
		nil,
		nil,
	)

	nodes := []chainNode{{Hop: 1, Channel: "公益站", Endpoint: "shared"}}
	enrichEndpointChainCredentialGroups("claude", nodes, lookup)

	if nodes[0].CredentialGroup != "" {
		t.Fatalf("ambiguous endpoint group = %q, want empty", nodes[0].CredentialGroup)
	}
}

func TestEnrichEndpointChainCredentialGroupsSkipsCodex(t *testing.T) {
	nodes := []chainNode{{Hop: 1, Channel: "Right Code", Endpoint: "AWSQ逆向渠道"}}
	lookup := map[string]string{
		endpointChainLookupKey("Right Code", "AWSQ逆向渠道"): "AWSQ逆向渠道",
	}

	enrichEndpointChainCredentialGroups("codex", nodes, lookup)

	if nodes[0].CredentialGroup != "" {
		t.Fatalf("codex chain group = %q, want empty", nodes[0].CredentialGroup)
	}
}
