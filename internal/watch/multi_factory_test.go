// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"testing"

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
// Only List is exercised; other methods are unreachable no-ops.
type fakeChild struct {
	listResult []runtime.Object
}

func (f *fakeChild) Client() client.Connection { return nil }
func (f *fakeChild) Get(*client.GVR, string, bool, labels.Selector) (runtime.Object, error) {
	return nil, nil
}
func (f *fakeChild) List(*client.GVR, string, bool, labels.Selector) ([]runtime.Object, error) {
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
