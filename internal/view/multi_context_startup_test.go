// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestSelectMultiContexts(t *testing.T) {
	contexts := map[string]*clientcmdapi.Context{
		"ctx-a": {Cluster: "cluster-a"},
		"ctx-b": {Cluster: "cluster-b"},
		"ctx-c": {Cluster: "cluster-c"},
	}

	tests := []struct {
		name      string
		requested []string
		want      []string
		wantErr   string
	}{
		{name: "empty means all", want: []string{"ctx-a", "ctx-b", "ctx-c"}},
		{name: "all keyword", requested: []string{"all"}, want: []string{"ctx-a", "ctx-b", "ctx-c"}},
		{name: "subset includes primary", requested: []string{"ctx-c"}, want: []string{"ctx-a", "ctx-c"}},
		{name: "deduplicates names", requested: []string{"ctx-b", "ctx-b"}, want: []string{"ctx-a", "ctx-b"}},
		{name: "unknown is rejected", requested: []string{"missing"}, wantErr: `requested context "missing" not found in kubeconfig`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectMultiContexts(contexts, "ctx-a", tt.requested)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
