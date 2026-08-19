// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"
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

func TestContextConfigFlagsDoNotCopyPrimaryCredentials(t *testing.T) {
	flags := genericclioptions.NewConfigFlags(false)
	kubeconfig, namespace, timeout := "config", "team-a", "5s"
	token, impersonate, uid, insecure := "primary-token", "primary-user", "primary-uid", true
	groups := []string{"primary-group"}
	flags.KubeConfig = &kubeconfig
	flags.Namespace = &namespace
	flags.Timeout = &timeout
	flags.BearerToken = &token
	flags.Impersonate = &impersonate
	flags.ImpersonateUID = &uid
	flags.ImpersonateGroup = &groups
	flags.Insecure = &insecure
	base := client.NewConfig(flags)

	child := contextConfigFlags(base, "ctx-b", &clientcmdapi.Context{Cluster: "cluster-b"})

	require.NotNil(t, child.Context)
	assert.Equal(t, "ctx-b", *child.Context)
	require.NotNil(t, child.ClusterName)
	assert.Equal(t, "cluster-b", *child.ClusterName)
	assert.Same(t, flags.KubeConfig, child.KubeConfig)
	assert.Same(t, flags.Namespace, child.Namespace)
	assert.Same(t, flags.Timeout, child.Timeout)
	assert.True(t, child.BearerToken == nil || *child.BearerToken == "")
	assert.True(t, child.Impersonate == nil || *child.Impersonate == "")
	assert.True(t, child.ImpersonateUID == nil || *child.ImpersonateUID == "")
	assert.True(t, child.ImpersonateGroup == nil || len(*child.ImpersonateGroup) == 0)
	assert.False(t, child.Insecure != nil && *child.Insecure)
}
