package main

import (
	"reflect"
	"testing"
)

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
