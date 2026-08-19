// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao

import (
	"context"
	"fmt"
	"sync"

	"github.com/derailed/k9s/internal/client"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// NonResource represents a non k8s resource.
type NonResource struct {
	Factory

	gvr        *client.GVR
	mx         sync.RWMutex
	includeObj bool
}

// Init initializes the resource.
func (n *NonResource) Init(f Factory, gvr *client.GVR) {
	n.mx.Lock()
	n.Factory, n.gvr = f, gvr
	n.mx.Unlock()
}

// SetIncludeObject sets if resource object should be included in the api server response.
func (n *NonResource) SetIncludeObject(f bool) {
	n.includeObj = f
}

func (n *NonResource) gvrStr() string {
	n.mx.RLock()
	defer n.mx.RUnlock()

	return n.gvr.String()
}

func (n *NonResource) getFactory() Factory {
	n.mx.RLock()
	defer n.mx.RUnlock()

	return n.Factory
}

// clientFor returns the per-row client.Connection when the underlying factory
// implements ContextualFactory (multi-context mode), or the primary's Client()
// otherwise. Disabled and unknown scopes are rejected by the contextual
// factory so mutations cannot silently fall through to the primary.
func (n *NonResource) clientFor(ctx context.Context) (client.Connection, error) {
	f := n.getFactory()
	if cf, ok := f.(ContextualFactory); ok {
		return cf.ClientFor(ctx)
	}
	return f.Client(), nil
}

// getRes fetches the runtime.Object for path, routing through the factory's
// ContextualFactory.GetWithContext (multi-context mode) when available so the
// read lands on the row's source cluster. Falls back to the plain Factory.Get
// in single-context mode. Used by every typed-DAO *GetInstanceWithContext*
// helper.
func getRes(f Factory, ctx context.Context, gvr *client.GVR, path string) (runtime.Object, error) {
	if cf, ok := f.(ContextualFactory); ok {
		return cf.GetWithContext(ctx, gvr, path, true, labels.Everything())
	}
	return f.Get(gvr, path, true, labels.Everything())
}

// listRes is the List equivalent of getRes: routes through
// ContextualFactory.ListWithContext when the factory supports it so the fan-out
// scopes to the ctx's KeyScopeContext (or, when KeyScopeContext is unset,
// behaves like the plain List). Used by the RefScanner Scan implementations
// so UsedBy results don't bleed across clusters.
func listRes(f Factory, ctx context.Context, gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	if cf, ok := f.(ContextualFactory); ok {
		return cf.ListWithContext(ctx, gvr, ns, wait, sel)
	}
	return f.List(gvr, ns, wait, sel)
}

// GVR returns a gvr.
func (n *NonResource) GVR() string {
	n.mx.RLock()
	defer n.mx.RUnlock()

	return n.gvrStr()
}

// Get returns the given resource.
func (*NonResource) Get(context.Context, string) (runtime.Object, error) {
	return nil, fmt.Errorf("nyi")
}
