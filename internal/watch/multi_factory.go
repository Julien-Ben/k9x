// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/slogs"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
)

// childFactory captures the subset of *Factory that MultiFactory consumes
// from each per-context child. Defining it as an interface (instead of using
// *Factory directly) keeps MultiFactory testable with lightweight fakes,
// without introducing an import cycle on dao.LifecycleFactory.
type childFactory interface {
	Client() client.Connection
	Get(gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error)
	List(gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error)
	ForResource(ns string, gvr *client.GVR) (informers.GenericInformer, error)
	CanForResource(ns string, gvr *client.GVR, verbs []string) (informers.GenericInformer, error)
	WaitForCacheSync()
	HasSynced(gvr *client.GVR, ns string) (bool, error)
	Start(ns string)
	Terminate()
	SetActiveNS(ns string) error
	AddForwarder(pf Forwarder)
	ForwarderFor(path string) (Forwarder, bool)
	DeleteForwarder(path string)
	Forwarders() Forwarders
	ValidatePortForwards()
}

// MultiFactory aggregates Factories from multiple kubeconfig contexts behind
// the dao.LifecycleFactory interface. List fans out across all children and
// merges results, tagging each object with its source context via the
// render.SourceContextAnnotation annotation. Get and forwarder operations are
// routed to the primary only — describe/yaml/port-forward are blanket-disabled
// in multi-context mode for the demo, so non-primary routing is not required.
type MultiFactory struct {
	children map[string]childFactory
	primary  string
	mx       sync.RWMutex
}

// NewMultiFactory builds a MultiFactory from a set of already-constructed
// per-context factories. primary must be a key in children — it is the
// kubeconfig's current-context and is used for non-fan-out operations
// (Get, AddForwarder, Client, etc.).
func NewMultiFactory(primary string, children map[string]*Factory) (*MultiFactory, error) {
	if _, ok := children[primary]; !ok {
		return nil, fmt.Errorf("primary context %q not present in children", primary)
	}
	cc := make(map[string]childFactory, len(children))
	for k, v := range children {
		cc[k] = v
	}
	return &MultiFactory{children: cc, primary: primary}, nil
}

// newMultiFactoryForTesting is an internal constructor used by tests in this
// package to plug in fake childFactory implementations. Not exported.
func newMultiFactoryForTesting(primary string, children map[string]childFactory) (*MultiFactory, error) {
	if _, ok := children[primary]; !ok {
		return nil, fmt.Errorf("primary context %q not present in children", primary)
	}
	return &MultiFactory{children: children, primary: primary}, nil
}

// Client returns the primary child's connection. In multi-context mode the
// view layer uses this for discovery, version, namespace validation, etc. —
// all anchored to the primary context.
func (m *MultiFactory) Client() client.Connection {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Client()
}

// ClientFor returns the client.Connection for the child identified by
// internal.KeyScopeContext on ctx, falling back to the primary's connection
// when the key is absent / empty / unknown. DAOs use this to dispatch
// mutations (delete, restart, scale, …) to the row's source cluster instead
// of always hitting the primary.
func (m *MultiFactory) ClientFor(ctx context.Context) client.Connection {
	m.mx.RLock()
	defer m.mx.RUnlock()
	if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
		if child, exists := m.children[scope]; exists {
			return child.Client()
		}
	}
	return m.children[m.primary].Client()
}

// List fans out across all children in parallel, deep-copies each returned
// object, and tags it with the source-context annotation. The merged slice is
// returned. The first error encountered aborts the whole call (demo behavior;
// MVP will isolate per-context errors).
func (m *MultiFactory) List(gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	return m.listImpl(nil, gvr, ns, wait, sel)
}

// ListWithContext is the context-aware variant. When internal.KeyScopeContext
// is present as a non-empty string in ctx, fan-out is constrained to that one
// child context (used by drill-down navigation so child views surface
// resources only from the parent row's source cluster). Otherwise behaves
// like List.
func (m *MultiFactory) ListWithContext(ctx context.Context, gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	return m.listImpl(ctx, gvr, ns, wait, sel)
}

func (m *MultiFactory) listImpl(ctx context.Context, gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	type childResult struct {
		ctxName string
		objs    []runtime.Object
		err     error
	}

	// Iterate children in a deterministic order so the merged row sequence is
	// stable across refresh ticks (avoids visible row reshuffling).
	ctxNames := slices.Sorted(maps.Keys(m.children))
	// Honor KeyScopeContext: constrain fan-out to a single child if set.
	if ctx != nil {
		if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
			if _, exists := m.children[scope]; exists {
				ctxNames = []string{scope}
			} else {
				return nil, fmt.Errorf("scope context %q not in children", scope)
			}
		}
	}
	results := make([]childResult, 0, len(ctxNames))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, ctxName := range ctxNames {
		child := m.children[ctxName]
		wg.Add(1)
		go func(name string, c childFactory) {
			defer wg.Done()
			oo, err := c.List(gvr, ns, wait, sel)
			mu.Lock()
			results = append(results, childResult{ctxName: name, objs: oo, err: err})
			mu.Unlock()
		}(ctxName, child)
	}
	wg.Wait()

	// Sort results back into deterministic order — goroutines complete in
	// arbitrary order so the slice must be re-ordered to match ctxNames.
	slices.SortFunc(results, func(a, b childResult) int {
		switch {
		case a.ctxName < b.ctxName:
			return -1
		case a.ctxName > b.ctxName:
			return 1
		default:
			return 0
		}
	})

	var (
		merged []runtime.Object
		errs   []error
	)
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("context %q: %w", r.ctxName, r.err))
			continue
		}
		for _, o := range r.objs {
			tagged, err := tagWithSourceContext(o, r.ctxName)
			if err != nil {
				errs = append(errs, fmt.Errorf("context %q: %w", r.ctxName, err))
				continue
			}
			merged = append(merged, tagged)
		}
	}
	if len(errs) > 0 {
		return merged, errors.Join(errs...)
	}
	return merged, nil
}

// tagWithSourceContext deep-copies the object (to avoid mutating the informer
// cache) and stamps it with the source-context annotation.
func tagWithSourceContext(o runtime.Object, ctxName string) (runtime.Object, error) {
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected *unstructured.Unstructured, got %T", o)
	}
	copy := u.DeepCopy()
	ann := copy.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 1)
	}
	ann[render.SourceContextAnnotation] = ctxName
	copy.SetAnnotations(ann)
	return copy, nil
}

// Get delegates to the primary child. Used by code paths that don't carry a
// context.Context; callers that have one should prefer GetWithContext to honor
// internal.KeyScopeContext for per-row routing.
func (m *MultiFactory) Get(gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Get(gvr, path, wait, sel)
}

// GetWithContext routes to the child identified by internal.KeyScopeContext.
// Falls back to the primary when the key is absent or empty. Returned objects
// are tagged with the source-context annotation so renderers can populate
// row.Source consistently with the List path.
func (m *MultiFactory) GetWithContext(ctx context.Context, gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	target := m.primary
	if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
		if _, exists := m.children[scope]; !exists {
			return nil, fmt.Errorf("scope context %q not in children", scope)
		}
		target = scope
	}
	o, err := m.children[target].Get(gvr, path, wait, sel)
	if err != nil {
		return nil, err
	}
	return tagWithSourceContext(o, target)
}

// ForResource delegates to the primary. Used by `List` internals on the
// primary path; the fan-out List in this type calls each child's List
// directly, so non-primary informers are still backed by their own children's
// `ForResource`.
func (m *MultiFactory) ForResource(ns string, gvr *client.GVR) (informers.GenericInformer, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].ForResource(ns, gvr)
}

// CanForResource delegates to the primary (RBAC check anchored to primary).
func (m *MultiFactory) CanForResource(ns string, gvr *client.GVR, verbs []string) (informers.GenericInformer, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].CanForResource(ns, gvr, verbs)
}

// WaitForCacheSync waits on every child's informers.
func (m *MultiFactory) WaitForCacheSync() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for _, c := range m.children {
		c.WaitForCacheSync()
	}
}

// HasSynced returns true only when every child reports synced for the given
// resource/namespace. Any child error short-circuits and propagates.
func (m *MultiFactory) HasSynced(gvr *client.GVR, ns string) (bool, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		ok, err := c.HasSynced(gvr, ns)
		if err != nil {
			return false, fmt.Errorf("context %q: %w", ctxName, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Start starts informers in every child for the given namespace.
func (m *MultiFactory) Start(ns string) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		slog.Debug("MultiFactory: starting informers", slogs.Context, ctxName, slogs.Namespace, ns)
		c.Start(ns)
	}
}

// Terminate stops informers in every child.
func (m *MultiFactory) Terminate() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		slog.Debug("MultiFactory: terminating informers", slogs.Context, ctxName)
		c.Terminate()
	}
}

// SetActiveNS sets the active namespace on every child. All children get the
// same namespace (uniform-namespace assumption — Option A in the design doc).
func (m *MultiFactory) SetActiveNS(ns string) error {
	m.mx.RLock()
	defer m.mx.RUnlock()
	var errs []error
	for ctxName, c := range m.children {
		if err := c.SetActiveNS(ns); err != nil {
			errs = append(errs, fmt.Errorf("context %q: %w", ctxName, err))
		}
	}
	return errors.Join(errs...)
}

// AddForwarder registers a port forwarder with the primary. Port-forwards
// aren't supported on non-primary rows in the demo.
func (m *MultiFactory) AddForwarder(pf Forwarder) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].AddForwarder(pf)
}

// ForwarderFor delegates to the primary.
func (m *MultiFactory) ForwarderFor(path string) (Forwarder, bool) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].ForwarderFor(path)
}

// DeleteForwarder deletes a port forwarder from the primary.
func (m *MultiFactory) DeleteForwarder(path string) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].DeleteForwarder(path)
}

// Forwarders returns the primary's port forwarders.
func (m *MultiFactory) Forwarders() Forwarders {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Forwarders()
}

// ValidatePortForwards validates port forwarders on the primary.
func (m *MultiFactory) ValidatePortForwards() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].ValidatePortForwards()
}
