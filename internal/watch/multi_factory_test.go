// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
)

func TestMultiFactoryList_MergesAndAnnotates(t *testing.T) {
	originalA := newUnstructuredPod("default", "pod-a1")
	originalB := newUnstructuredPod("default", "pod-b1")
	childA := &fakeChild{listResult: []runtime.Object{originalA}}
	childB := &fakeChild{listResult: []runtime.Object{originalB}}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 2)

	gotByName := make(map[string]string, len(merged))
	for _, o := range merged {
		u, ok := o.(*unstructured.Unstructured)
		require.True(t, ok, "merged result not unstructured")
		gotByName[u.GetName()] = u.GetAnnotations()[render.SourceContextAnnotation]
	}
	assert.Equal(t, "ctxA", gotByName["pod-a1"], "pod-a1 must carry ctxA annotation")
	assert.Equal(t, "ctxB", gotByName["pod-b1"], "pod-b1 must carry ctxB annotation")

	// DeepCopy guarantee: original objects in the children's caches must not
	// have been mutated. (If we forgot to DeepCopy in tagWithSourceContext we'd
	// corrupt the informer cache for the next reconcile tick.)
	assert.Empty(t, originalA.GetAnnotations(),
		"original child object must not be mutated — DeepCopy missing in MultiFactory.List")
	assert.Empty(t, originalB.GetAnnotations(),
		"original child object must not be mutated — DeepCopy missing in MultiFactory.List")
}

// TestMultiFactoryList_DeterministicOrder asserts that merged rows arrive in a
// stable order across successive refresh ticks even though child List calls
// fan out across goroutines. Without sorting, Go map iteration randomness
// would cause merged-row order to shuffle, which manifests as visible row
// reshuffling in the UI on every refresh.
func TestMultiFactoryList_DeterministicOrder(t *testing.T) {
	childA := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}
	childB := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}
	childC := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxC": childC,
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)

	var firstOrder []string
	for i := 0; i < 5; i++ {
		merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
		require.NoError(t, err)
		require.Len(t, merged, 3)

		order := make([]string, 0, len(merged))
		for _, o := range merged {
			u := o.(*unstructured.Unstructured)
			order = append(order, u.GetAnnotations()[render.SourceContextAnnotation])
		}
		if firstOrder == nil {
			firstOrder = order
			assert.Equal(t, []string{"ctxA", "ctxB", "ctxC"}, order,
				"merged rows must be ordered by sorted context name")
		} else {
			assert.Equal(t, firstOrder, order, "row order must be stable across List calls")
		}
	}
}

// TestMultiFactoryListWithContext_ScopeFiltersFanOut asserts that when
// internal.KeyScopeContext is set on the ctx, ListWithContext routes only to
// the matching child — the underlying mechanism for drill-down navigation
// that constrains a child view's resources to the parent row's cluster.
func TestMultiFactoryListWithContext_ScopeFiltersFanOut(t *testing.T) {
	var hitA, hitB int32
	childA := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-from-a")},
		onList:     func() { atomic.AddInt32(&hitA, 1) },
	}
	childB := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-from-b")},
		onList:     func() { atomic.AddInt32(&hitB, 1) },
	}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxB")
	merged, err := mf.ListWithContext(ctx, client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1)

	u := merged[0].(*unstructured.Unstructured)
	assert.Equal(t, "p-from-b", u.GetName())
	assert.Equal(t, "ctxB", u.GetAnnotations()[render.SourceContextAnnotation])
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitA), "ctxA must not be hit when scope=ctxB")
	assert.Equal(t, int32(1), atomic.LoadInt32(&hitB), "ctxB must be hit exactly once")
}

// TestMultiFactoryGetWithContext_RoutesToScope asserts GetWithContext fetches
// from the scoped child rather than the primary.
func TestMultiFactoryGetWithContext_RoutesToScope(t *testing.T) {
	var hitA, hitB int32
	childA := &fakeChild{
		getResult: newUnstructuredPod("default", "p-from-a"),
		onGet:     func() { atomic.AddInt32(&hitA, 1) },
	}
	childB := &fakeChild{
		getResult: newUnstructuredPod("default", "p-from-b"),
		onGet:     func() { atomic.AddInt32(&hitB, 1) },
	}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxB")
	o, err := mf.GetWithContext(ctx, client.PodGVR, "default/p", true, labels.Everything())
	require.NoError(t, err)
	u := o.(*unstructured.Unstructured)
	assert.Equal(t, "p-from-b", u.GetName())
	assert.Equal(t, "ctxB", u.GetAnnotations()[render.SourceContextAnnotation])
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitA))
	assert.Equal(t, int32(1), atomic.LoadInt32(&hitB))
}

// TestMultiFactoryClientFor_RoutesByScope asserts ClientFor returns the
// matching child's connection when KeyScopeContext is set, and falls back to
// the primary otherwise. This is the routing hop DAO mutations dispatch
// through to land writes on the row's source cluster.
func TestMultiFactoryClientFor_RoutesByScope(t *testing.T) {
	connA, connB := &stubConn{name: "ctxA"}, &stubConn{name: "ctxB"}
	childA := &fakeChild{conn: connA}
	childB := &fakeChild{conn: connB}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)

	t.Run("no_scope_uses_primary", func(t *testing.T) {
		c := mf.ClientFor(t.Context())
		assert.Same(t, connA, c)
	})

	t.Run("scope_ctxB_routes_to_b", func(t *testing.T) {
		ctx := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxB")
		c := mf.ClientFor(ctx)
		assert.Same(t, connB, c)
	})

	t.Run("unknown_scope_falls_back_to_primary", func(t *testing.T) {
		ctx := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxZ")
		c := mf.ClientFor(ctx)
		assert.Same(t, connA, c)
	})
}

// ----------------------------------------------------------------------------
// Helpers.

func newUnstructuredPod(ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("Pod")
	u.SetNamespace(ns)
	u.SetName(name)
	return u
}

// fakeChild is a minimal childFactory used only by tests in this package.
// onList/onGet are call-counter hooks used by scope-routing tests; nil hooks
// are skipped so existing call sites need no changes.
type fakeChild struct {
	listResult []runtime.Object
	getResult  runtime.Object
	onList     func()
	onGet      func()
	conn       client.Connection
}

func (f *fakeChild) Client() client.Connection { return f.conn }

// stubConn is a Connection implementation used only by routing tests for
// pointer identity. Methods are unused; embedded interface is nil so any
// accidental call panics loudly.
type stubConn struct {
	client.Connection
	name string //nolint:unused // identifier preserved for debug printouts
}
func (f *fakeChild) Get(*client.GVR, string, bool, labels.Selector) (runtime.Object, error) {
	if f.onGet != nil {
		f.onGet()
	}
	return f.getResult, nil
}
func (f *fakeChild) List(*client.GVR, string, bool, labels.Selector) ([]runtime.Object, error) {
	if f.onList != nil {
		f.onList()
	}
	return f.listResult, nil
}
func (f *fakeChild) ForResource(string, *client.GVR) (informers.GenericInformer, error) {
	return nil, nil
}
func (f *fakeChild) CanForResource(string, *client.GVR, []string) (informers.GenericInformer, error) {
	return nil, nil
}
func (f *fakeChild) WaitForCacheSync()                                {}
func (f *fakeChild) HasSynced(*client.GVR, string) (bool, error)      { return true, nil }
func (f *fakeChild) Start(string)                                     {}
func (f *fakeChild) Terminate()                                       {}
func (f *fakeChild) SetActiveNS(string) error                         { return nil }
func (f *fakeChild) AddForwarder(Forwarder)                           {}
func (f *fakeChild) ForwarderFor(string) (Forwarder, bool)            { return nil, false }
func (f *fakeChild) DeleteForwarder(string)                           {}
func (f *fakeChild) Forwarders() Forwarders                           { return nil }
func (f *fakeChild) ValidatePortForwards()                            {}
