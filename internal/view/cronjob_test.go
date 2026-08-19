// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"
	"testing"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGetScopedResourceRoutesCronJobLookup(t *testing.T) {
	factory := &scopedGetFactory{
		obj: &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
		}},
	}
	ctx := scopedCtx(context.Background(), model1.RowIdent{ID: "default/nightly", Source: "ctx-b"})

	got, err := getScopedResource(factory, ctx, client.CjGVR, "default/nightly")

	require.NoError(t, err)
	assert.Same(t, factory.obj, got)
	assert.Equal(t, []string{"ctx-b"}, factory.scopes)
	assert.Zero(t, factory.plainGetCalls)
}

func TestFetchPodRoutesThroughScopeContext(t *testing.T) {
	factory := &scopedGetFactory{
		obj: &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "pod-a",
				"namespace": "default",
			},
			"spec": map[string]any{
				"containers": []any{map[string]any{"name": "main", "image": "busybox"}},
			},
		}},
	}
	ctx := scopedCtx(context.Background(), model1.RowIdent{ID: "default/pod-a", Source: "ctx-b"})

	pod, err := fetchPod(ctx, factory, "default/pod-a")

	require.NoError(t, err)
	assert.Equal(t, "pod-a", pod.Name)
	assert.Equal(t, []string{"ctx-b"}, factory.scopes)
	assert.Zero(t, factory.plainGetCalls)
}

type scopedGetFactory struct {
	testFactory
	obj           runtime.Object
	scopes        []string
	plainGetCalls int
}

var _ dao.ContextualFactory = (*scopedGetFactory)(nil)

func (f *scopedGetFactory) Get(*client.GVR, string, bool, labels.Selector) (runtime.Object, error) {
	f.plainGetCalls++
	return f.obj, nil
}

func (f *scopedGetFactory) GetWithContext(ctx context.Context, _ *client.GVR, _ string, _ bool, _ labels.Selector) (runtime.Object, error) {
	f.scopes = append(f.scopes, ctx.Value(internal.KeyScopeContext).(string))
	return f.obj, nil
}

func (*scopedGetFactory) ListWithContext(context.Context, *client.GVR, string, bool, labels.Selector) ([]runtime.Object, error) {
	return nil, nil
}

func (*scopedGetFactory) ClientFor(context.Context) (client.Connection, error) { return nil, nil }
