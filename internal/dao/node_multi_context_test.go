// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao

import (
	"testing"

	"github.com/derailed/k9s/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestCountPodsFiltersBySourceContext(t *testing.T) {
	pods := []runtime.Object{
		podOnNode("pod-a", "node-shared", "ctx-a"),
		podOnNode("pod-b", "node-shared", "ctx-b"),
		podOnNode("pod-c", "other-node", "ctx-a"),
	}

	count, err := countPods(pods, "node-shared", "ctx-b")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func podOnNode(name, node, source string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name": name,
			"annotations": map[string]any{
				render.SourceContextAnnotation: source,
			},
		},
		"spec": map[string]any{"nodeName": node},
	}}
	return pod
}
