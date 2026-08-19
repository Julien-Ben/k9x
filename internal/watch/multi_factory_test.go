// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// TestMultiFactory_List_PerContextTimeout asserts a slow child does not stall
// the whole fan-out: the fast child's rows still merge through, and the slow
// child's failure is captured in LastTickErrors as a timeout rather than
// poisoning the result with a fatal error.
func TestMultiFactory_List_PerContextTimeout(t *testing.T) {
	slow := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-slow")},
		listDelay:  200 * time.Millisecond,
	}
	fast := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-fast")},
	}
	mf, err := newMultiFactoryForTesting("ctxFast", map[string]childFactory{
		"ctxFast": fast,
		"ctxSlow": slow,
	})
	require.NoError(t, err)
	mf.SetChildListTimeout(20 * time.Millisecond)

	start := time.Now()
	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	elapsed := time.Since(start)

	require.NoError(t, err, "partial success: at least one child returned")
	require.Len(t, merged, 1)
	assert.Equal(t, "p-fast", merged[0].(*unstructured.Unstructured).GetName())
	assert.Less(t, elapsed, slow.listDelay, "must not wait for the slow child")

	tickErrs := mf.LastTickErrors()
	require.Len(t, tickErrs, 1)
	assert.Equal(t, "ctxSlow", tickErrs[0].Context)
	assert.Contains(t, tickErrs[0].Err.Error(), "timed out")
}

// TestMultiFactory_List_PartialSuccess asserts an erroring child does not
// cause the whole call to fail when other children return rows. The error
// is recorded per-child in LastTickErrors for the view layer to surface.
func TestMultiFactory_List_PartialSuccess(t *testing.T) {
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}
	bad := &fakeChild{listErr: errors.New("connection refused")}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1)
	assert.Equal(t, "ctxGood", merged[0].(*unstructured.Unstructured).GetAnnotations()[render.SourceContextAnnotation])

	tickErrs := mf.LastTickErrors()
	require.Len(t, tickErrs, 1)
	assert.Equal(t, "ctxBad", tickErrs[0].Context)
	assert.ErrorContains(t, tickErrs[0].Err, "connection refused")
}

// TestMultiFactory_List_AllFail asserts that when every child fails the call
// surfaces the joined error rather than silently returning empty rows.
func TestMultiFactory_List_AllFail(t *testing.T) {
	a := &fakeChild{listErr: errors.New("boom A")}
	b := &fakeChild{listErr: errors.New("boom B")}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": a,
		"ctxB": b,
	})
	require.NoError(t, err)

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.Error(t, err)
	assert.Nil(t, merged)
	assert.ErrorContains(t, err, "boom A")
	assert.ErrorContains(t, err, "boom B")

	tickErrs := mf.LastTickErrors()
	assert.Len(t, tickErrs, 2)
}

// TestMultiFactory_List_SilentNotFound asserts that an IsNotFound error from
// a child (the GVR doesn't exist on that cluster) is routed to the divergence
// channel rather than the fatal-error channel. The merge succeeds with the
// other cluster's rows.
func TestMultiFactory_List_SilentNotFound(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}
	missing := &fakeChild{listErr: notFound}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood":    good,
		"ctxMissing": missing,
	})
	require.NoError(t, err)

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1)

	assert.Empty(t, mf.LastTickErrors(), "NotFound must not surface as a real error")
	divs := mf.LastTickDivergences()
	require.Len(t, divs, 1)
	assert.Equal(t, "ctxMissing", divs[0].Context)
}

// TestMultiFactory_NewDivergencesForFlash_BannerOnce asserts the one-shot
// dedup: the first call returns the diverging context, subsequent calls for
// the same (gvr, ctx) return nothing. A *different* GVR's divergence on the
// same ctx still flashes (independent acknowledgement state).
func TestMultiFactory_NewDivergencesForFlash_BannerOnce(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p")}}
	missing := &fakeChild{listErr: notFound}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": good,
		"ctxB": missing,
	})
	require.NoError(t, err)

	_, err = mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)

	first := mf.NewDivergencesForFlash(client.PodGVR)
	require.Equal(t, []string{"ctxB"}, first)
	second := mf.NewDivergencesForFlash(client.PodGVR)
	assert.Empty(t, second, "second call must be silenced")

	// Same divergence under a different GVR re-fires (independent dedup key).
	another := mf.NewDivergencesForFlash(client.DpGVR)
	assert.Equal(t, []string{"ctxB"}, another)
}

// TestMultiFactory_LastTickErrors_ResetsOnSuccess asserts a clean tick clears
// the previously stashed errors — otherwise stale banners would persist after
// a cluster recovered.
func TestMultiFactory_LastTickErrors_ResetsOnSuccess(t *testing.T) {
	bad := &fakeChild{listErr: errors.New("boom")}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}},
		"ctxB": bad,
	})
	require.NoError(t, err)

	_, err = mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, mf.LastTickErrors(), 1)

	bad.listErr = nil
	bad.listResult = []runtime.Object{newUnstructuredPod("d", "p")}
	_, err = mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	assert.Empty(t, mf.LastTickErrors(), "successful tick must reset")
}

// TestMultiFactory_Health_QuarantineAfterKFailures asserts a child flips to
// Quarantined after quarantineThreshold consecutive failures and emits a
// single transition for the flash dispatch.
func TestMultiFactory_Health_QuarantineAfterKFailures(t *testing.T) {
	bad := &fakeChild{listErr: errors.New("boom")}
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)

	// First (K-1) failures keep the child healthy — no transition.
	for i := 0; i < quarantineThreshold-1; i++ {
		_, err := mf.List(client.PodGVR, "default", false, labels.Everything())
		require.NoError(t, err)
	}
	assert.Empty(t, mf.NewHealthTransitions(), "no transition until threshold crossed")
	assert.Equal(t, HealthHealthy, mf.HealthSnapshot()["ctxBad"])

	// The threshold-crossing tick should produce one Healthy → Quarantined edge.
	_, err = mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	tr := mf.NewHealthTransitions()
	require.Len(t, tr, 1)
	assert.Equal(t, "ctxBad", tr[0].Context)
	assert.Equal(t, HealthHealthy, tr[0].From)
	assert.Equal(t, HealthQuarantined, tr[0].To)
	assert.NotNil(t, tr[0].Err)

	// Further ticks while still failing must NOT keep flashing.
	_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	assert.Empty(t, mf.NewHealthTransitions())
}

// TestMultiFactory_Health_RecoverOnSingleSuccess asserts a quarantined child
// returns to Healthy on the first successful probe tick, emitting one
// transition. Probe interval is compressed so the test doesn't wait 30s.
func TestMultiFactory_Health_RecoverOnSingleSuccess(t *testing.T) {
	bad := &fakeChild{listErr: errors.New("boom")}
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)
	mf.SetQuarantineProbeInterval(1 * time.Millisecond)

	// Drive ctxBad into quarantine.
	for i := 0; i < quarantineThreshold; i++ {
		_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	}
	_ = mf.NewHealthTransitions() // drain
	require.Equal(t, HealthQuarantined, mf.HealthSnapshot()["ctxBad"])

	// Heal the child and wait past the probe window.
	bad.listErr = nil
	bad.listResult = []runtime.Object{newUnstructuredPod("d", "p2")}
	time.Sleep(5 * time.Millisecond)
	_, err = mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	tr := mf.NewHealthTransitions()
	require.Len(t, tr, 1)
	assert.Equal(t, HealthQuarantined, tr[0].From)
	assert.Equal(t, HealthHealthy, tr[0].To)
}

// TestMultiFactory_Health_QuarantineSkipsFanOut asserts that once a child is
// Quarantined the next ticks skip it entirely (no per-tick timeout cost) and
// the merge still ships rows from healthy siblings.
func TestMultiFactory_Health_QuarantineSkipsFanOut(t *testing.T) {
	var badCalls int32
	bad := &fakeChild{listErr: errors.New("boom"), onList: func() { atomic.AddInt32(&badCalls, 1) }}
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)

	// Drive ctxBad into quarantine.
	for i := 0; i < quarantineThreshold; i++ {
		_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	}
	require.Equal(t, HealthQuarantined, mf.HealthSnapshot()["ctxBad"])
	pre := atomic.LoadInt32(&badCalls)

	// Subsequent tick should skip ctxBad (lastProbeAt < probeInterval).
	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1, "healthy sibling still ships rows")
	assert.Equal(t, pre, atomic.LoadInt32(&badCalls), "quarantined child must NOT be re-listed within probe interval")
}

// TestMultiFactory_Health_ScopeTargetedBypassesQuarantine asserts that an
// explicit scoped call (drill-down) attempts the requested context even if
// it's currently quarantined — the user explicitly asked for THIS cluster.
func TestMultiFactory_Health_ScopeTargetedBypassesQuarantine(t *testing.T) {
	var badCalls int32
	bad := &fakeChild{listErr: errors.New("boom"), onList: func() { atomic.AddInt32(&badCalls, 1) }}
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)
	for i := 0; i < quarantineThreshold; i++ {
		_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	}
	require.Equal(t, HealthQuarantined, mf.HealthSnapshot()["ctxBad"])
	pre := atomic.LoadInt32(&badCalls)

	scope := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxBad")
	_, _ = mf.ListWithContext(scope, client.PodGVR, "default", false, labels.Everything())
	assert.Greater(t, atomic.LoadInt32(&badCalls), pre, "scoped call must reach quarantined child")
}

// TestMultiFactory_Health_NotFoundDoesNotCount asserts that a NotFound (which
// already routes to divergences) does NOT count as a failure for the health
// state machine — the cluster IS up, it just doesn't have this GVR.
func TestMultiFactory_Health_NotFoundDoesNotCount(t *testing.T) {
	nf := apierrors.NewNotFound(schema.GroupResource{Resource: "crd"}, "")
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	missing := &fakeChild{listErr: nf}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood":    good,
		"ctxMissing": missing,
	})
	require.NoError(t, err)

	for i := 0; i < quarantineThreshold+2; i++ {
		_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	}
	assert.Equal(t, HealthHealthy, mf.HealthSnapshot()["ctxMissing"])
	assert.Empty(t, mf.NewHealthTransitions())
}

func TestMultiFactory_ToggleContext_SkipsListImmediately(t *testing.T) {
	var hitA, hitB int32
	childA := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-a")},
		onList:     func() { atomic.AddInt32(&hitA, 1) },
	}
	childB := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-b")},
		onList:     func() { atomic.AddInt32(&hitB, 1) },
	}

	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)

	enabled, err := mf.ToggleContext("ctxB")
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.False(t, mf.ContextEnabled("ctxB"))
	assert.Equal(t, 1, mf.EnabledCount())

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1)
	assert.Equal(t, "p-a", merged[0].(*unstructured.Unstructured).GetName())
	assert.Equal(t, int32(1), atomic.LoadInt32(&hitA))
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitB), "disabled context must be skipped before reconcile fires")
}

func TestMultiFactory_ToggleContext_LastEnabledGuard(t *testing.T) {
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{},
		"ctxB": &fakeChild{},
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)

	enabled, err := mf.ToggleContext("ctxB")
	require.NoError(t, err)
	assert.False(t, enabled)

	enabled, err = mf.ToggleContext("ctxA")
	require.ErrorIs(t, err, ErrLastEnabledContext)
	assert.True(t, enabled, "guarded context remains enabled")
	assert.True(t, mf.ContextEnabled("ctxA"))
	assert.False(t, mf.ContextEnabled("ctxB"))
	assert.Equal(t, 1, mf.EnabledCount())
}

func TestMultiFactory_ToggleContext_CoalescesReconcile(t *testing.T) {
	childB := &fakeChild{}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{},
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(10 * time.Millisecond)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&childB.terminateCalls) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&childB.startCalls), "off/on/off should converge with one terminate and no start")

	baseTerminate := atomic.LoadInt32(&childB.terminateCalls)
	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, baseTerminate, atomic.LoadInt32(&childB.terminateCalls), "off/on landing back on actual state should do no work")
}

func TestMultiFactory_ToggleContext_ReenableRestartsWithActiveNamespace(t *testing.T) {
	childB := &fakeChild{}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{},
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(10 * time.Millisecond)
	mf.Start("team-a")
	baseStarts := atomic.LoadInt32(&childB.startCalls)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&childB.terminateCalls) == 1
	}, time.Second, 5*time.Millisecond)

	enabled, err := mf.ToggleContext("ctxB")
	require.NoError(t, err)
	assert.True(t, enabled)
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&childB.startCalls) == baseStarts+1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, "team-a", childB.lastStartNamespace())
}

func TestMultiFactory_ToggleContext_DisabledIncludedInHealthSummaryTotal(t *testing.T) {
	bad := &fakeChild{listErr: errors.New("boom")}
	good := &fakeChild{listResult: []runtime.Object{newUnstructuredPod("d", "p")}}
	mf, err := newMultiFactoryForTesting("ctxGood", map[string]childFactory{
		"ctxGood": good,
		"ctxBad":  bad,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(10 * time.Millisecond)

	for i := 0; i < quarantineThreshold; i++ {
		_, _ = mf.List(client.PodGVR, "default", false, labels.Everything())
	}
	require.Equal(t, HealthQuarantined, mf.HealthSnapshot()["ctxBad"])

	_, err = mf.ToggleContext("ctxBad")
	require.NoError(t, err)

	total, healthy, quarantined := mf.HealthSummary()
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, healthy)
	assert.Empty(t, quarantined)
	assert.Equal(t, HealthDisabled, mf.HealthSnapshot()["ctxBad"])

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&bad.terminateCalls) == 1
	}, time.Second, 5*time.Millisecond)
	enabled, err := mf.ToggleContext("ctxBad")
	require.NoError(t, err)
	assert.True(t, enabled)
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&bad.startCalls) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, HealthHealthy, mf.HealthSnapshot()["ctxBad"], "disable should reset stale quarantine state")
}

func TestMultiFactory_HasMetrics_SkipsDisabledChildren(t *testing.T) {
	childA := &fakeChild{conn: &stubConn{metricsStub: true, hasMetrics: true}}
	childB := &fakeChild{conn: &stubConn{metricsStub: true, hasMetrics: false}}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)
	assert.False(t, mf.HasMetrics())

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	assert.True(t, mf.HasMetrics(), "disabled metrics-less child must not veto enabled set")
}

func TestMultiFactory_RuntimeIteratorsSkipDisabledChildren(t *testing.T) {
	childA := &fakeChild{}
	childB := &fakeChild{}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": childA,
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)

	mf.WaitForCacheSync()
	_, err = mf.HasSynced(client.PodGVR, "default")
	require.NoError(t, err)
	err = mf.SetActiveNS("team-a")
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&childA.waitSyncCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&childB.waitSyncCalls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&childA.hasSyncedCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&childB.hasSyncedCalls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&childA.setNSCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&childB.setNSCalls))
}

func TestMultiFactory_ToggleContext_DisablingPrimarySkipsFanoutWithoutTerminatingPrimary(t *testing.T) {
	var hitPrimary, hitOther int32
	primary := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-primary")},
		onList:     func() { atomic.AddInt32(&hitPrimary, 1) },
	}
	other := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-other")},
		onList:     func() { atomic.AddInt32(&hitOther, 1) },
	}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": primary,
		"ctxB": other,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(10 * time.Millisecond)

	enabled, err := mf.ToggleContext("ctxA")
	require.NoError(t, err)
	assert.False(t, enabled)

	require.Eventually(t, func() bool {
		return mf.HealthSnapshot()["ctxA"] == HealthDisabled
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&primary.terminateCalls), "primary infrastructure must remain alive")

	merged, err := mf.List(client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	require.Len(t, merged, 1)
	assert.Equal(t, "p-other", merged[0].(*unstructured.Unstructured).GetName())
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitPrimary))
	assert.Equal(t, int32(1), atomic.LoadInt32(&hitOther))
}

func TestMultiFactory_ListWithContext_DisabledScopeDoesNotReachChild(t *testing.T) {
	var hitB int32
	childB := &fakeChild{
		listResult: []runtime.Object{newUnstructuredPod("default", "p-b")},
		onList:     func() { atomic.AddInt32(&hitB, 1) },
	}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{listResult: []runtime.Object{newUnstructuredPod("default", "p-a")}},
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)

	scope := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxB")
	merged, err := mf.ListWithContext(scope, client.PodGVR, "default", false, labels.Everything())
	require.NoError(t, err)
	assert.Empty(t, merged)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitB), "disabled scoped list must not resurrect informers")
}

func TestMultiFactory_ToggleContext_ReenableAfterFullRestartStartsDisabledChild(t *testing.T) {
	childB := &fakeChild{}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{},
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)
	mf.Start("team-a")
	baseStarts := atomic.LoadInt32(&childB.startCalls)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)
	mf.Terminate()
	mf.Start("team-b")
	assert.Equal(t, baseStarts, atomic.LoadInt32(&childB.startCalls), "disabled child must stay stopped during full restart")

	mf.SetContextToggleDelay(10 * time.Millisecond)
	enabled, err := mf.ToggleContext("ctxB")
	require.NoError(t, err)
	assert.True(t, enabled)
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&childB.startCalls) == baseStarts+1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, "team-b", childB.lastStartNamespace())
}

func TestMultiFactory_GetWithContext_DisabledScopeDoesNotReachChild(t *testing.T) {
	var hitB int32
	childB := &fakeChild{
		getResult: newUnstructuredPod("default", "p-b"),
		onGet:     func() { atomic.AddInt32(&hitB, 1) },
	}
	mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
		"ctxA": &fakeChild{getResult: newUnstructuredPod("default", "p-a")},
		"ctxB": childB,
	})
	require.NoError(t, err)
	mf.SetContextToggleDelay(time.Hour)

	_, err = mf.ToggleContext("ctxB")
	require.NoError(t, err)

	scope := context.WithValue(t.Context(), internal.KeyScopeContext, "ctxB")
	_, err = mf.GetWithContext(scope, client.PodGVR, "default/p-b", true, labels.Everything())
	require.ErrorIs(t, err, ErrContextDisabled)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitB), "disabled scoped get must not resurrect informers")
}

// TestMultiFactory_HasMetrics_AllOrNone asserts the conservative AND across
// children: if any cluster lacks metrics, HasMetrics returns false so the UI
// hides CPU/MEM columns rather than rendering N/A for metric-less rows.
func TestMultiFactory_HasMetrics_AllOrNone(t *testing.T) {
	tests := []struct {
		name string
		a, b bool
		want bool
	}{
		{"both have metrics", true, true, true},
		{"a lacks metrics", false, true, false},
		{"b lacks metrics", true, false, false},
		{"neither has metrics", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			childA := &fakeChild{conn: &stubConn{metricsStub: true, hasMetrics: tc.a}}
			childB := &fakeChild{conn: &stubConn{metricsStub: true, hasMetrics: tc.b}}
			mf, err := newMultiFactoryForTesting("ctxA", map[string]childFactory{
				"ctxA": childA,
				"ctxB": childB,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, mf.HasMetrics())
		})
	}
}

// TestMultiFactory_Contexts asserts the sorted child-name accessor used by the
// startup flash and cluster-info panel.
func TestMultiFactory_Contexts(t *testing.T) {
	mf, err := newMultiFactoryForTesting("ctxB", map[string]childFactory{
		"ctxC": &fakeChild{},
		"ctxA": &fakeChild{},
		"ctxB": &fakeChild{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"ctxA", "ctxB", "ctxC"}, mf.Contexts())
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
// are skipped so existing call sites need no changes. listErr / listDelay are
// opt-in knobs for the resilience tests (per-child timeout, partial success):
// listErr lets a child return an error without affecting siblings; listDelay
// pushes a child past the per-child List timeout.
type fakeChild struct {
	listResult []runtime.Object
	listErr    error
	listDelay  time.Duration
	getResult  runtime.Object
	onList     func()
	onGet      func()
	conn       client.Connection
	mx         sync.Mutex
	lastStart  string

	startCalls     int32
	terminateCalls int32
	setNSCalls     int32
	waitSyncCalls  int32
	hasSyncedCalls int32
}

func (f *fakeChild) Client() client.Connection { return f.conn }

// stubConn is a Connection implementation used only by routing tests for
// pointer identity. Methods are unused; embedded interface is nil so any
// accidental call panics loudly. hasMetrics is opt-in for tests that need
// HasMetrics-shaped behavior (otherwise the embedded nil panics, surfacing
// any unintended call).
type stubConn struct {
	client.Connection
	name        string //nolint:unused // identifier preserved for debug printouts
	hasMetrics  bool
	metricsStub bool
}

func (s *stubConn) HasMetrics() bool {
	if s.metricsStub {
		return s.hasMetrics
	}
	// Fall through to embedded nil → panic, preserving original semantics.
	return s.Connection.HasMetrics()
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
	if f.listDelay > 0 {
		time.Sleep(f.listDelay)
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}
func (f *fakeChild) ForResource(string, *client.GVR) (informers.GenericInformer, error) {
	return nil, nil
}
func (f *fakeChild) CanForResource(string, *client.GVR, []string) (informers.GenericInformer, error) {
	return nil, nil
}
func (f *fakeChild) WaitForCacheSync() {
	atomic.AddInt32(&f.waitSyncCalls, 1)
}
func (f *fakeChild) HasSynced(*client.GVR, string) (bool, error) {
	atomic.AddInt32(&f.hasSyncedCalls, 1)
	return true, nil
}
func (f *fakeChild) Start(ns string) {
	atomic.AddInt32(&f.startCalls, 1)
	f.mx.Lock()
	defer f.mx.Unlock()
	f.lastStart = ns
}
func (f *fakeChild) Terminate() {
	atomic.AddInt32(&f.terminateCalls, 1)
}
func (f *fakeChild) SetActiveNS(string) error {
	atomic.AddInt32(&f.setNSCalls, 1)
	return nil
}
func (f *fakeChild) lastStartNamespace() string {
	f.mx.Lock()
	defer f.mx.Unlock()
	return f.lastStart
}
func (f *fakeChild) AddForwarder(Forwarder)                {}
func (f *fakeChild) ForwarderFor(string) (Forwarder, bool) { return nil, false }
func (f *fakeChild) DeleteForwarder(string)                {}
func (f *fakeChild) Forwarders() Forwarders                { return nil }
func (f *fakeChild) ValidatePortForwards()                 {}
