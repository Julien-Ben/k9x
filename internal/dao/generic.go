// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao

import (
	"context"
	"fmt"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type Grace int64

const (
	// DefaultGrace uses delete default termination policy.
	DefaultGrace Grace = -1

	// ForceGrace sets delete grace-period to 0.
	ForceGrace Grace = 0

	// NowGrace set delete grace-period to 1,
	NowGrace Grace = 1
)

var _ Describer = (*Generic)(nil)

// Generic represents a generic resource.
type Generic struct {
	NonResource
}

// List returns a collection of resources.
// BOZO!! no auth check??
func (g *Generic) List(ctx context.Context, ns string) ([]runtime.Object, error) {
	labelSel, ok := ctx.Value(internal.KeyLabels).(labels.Selector)
	if !ok {
		labelSel = labels.Everything()
	}
	if client.IsAllNamespace(ns) {
		ns = client.BlankNamespace
	}

	dial, err := g.dynClientFor(ctx)
	if err != nil {
		return nil, err
	}

	opts := metav1.ListOptions{LabelSelector: labelSel.String()}
	var ll *unstructured.UnstructuredList
	if client.IsClusterScoped(ns) {
		ll, err = dial.List(ctx, opts)
	} else {
		ll, err = dial.Namespace(ns).List(ctx, opts)
	}
	if err != nil {
		return nil, err
	}

	oo := make([]runtime.Object, len(ll.Items))
	for i := range ll.Items {
		oo[i] = &ll.Items[i]
	}

	return oo, nil
}

// Get returns a given resource.
func (g *Generic) Get(ctx context.Context, path string) (runtime.Object, error) {
	ns, n := client.Namespaced(path)
	dial, err := g.dynClientFor(ctx)
	if err != nil {
		return nil, err
	}

	var opts metav1.GetOptions
	if client.IsClusterScoped(ns) {
		return dial.Get(ctx, n, opts)
	}

	return dial.Namespace(ns).Get(ctx, n, opts)
}

// Describe describes a resource.
func (g *Generic) Describe(path string) (string, error) {
	return Describe(g.Client(), g.gvr, path)
}

// DescribeWithContext is the context-aware Describe used by views that need
// to route per-row in multi-context mode (the ctx carries
// internal.KeyScopeContext).
func (g *Generic) DescribeWithContext(ctx context.Context, path string) (string, error) {
	conn, err := g.clientFor(ctx)
	if err != nil {
		return "", err
	}
	return Describe(conn, g.gvr, path)
}

// ToYAML returns a resource yaml.
func (g *Generic) ToYAML(path string, showManaged bool) (string, error) {
	return g.ToYAMLWithContext(context.Background(), path, showManaged)
}

// ToYAMLWithContext is the context-aware ToYAML used by views that need to
// route per-row in multi-context mode (the ctx carries internal.KeyScopeContext).
func (g *Generic) ToYAMLWithContext(ctx context.Context, path string, showManaged bool) (string, error) {
	o, err := g.Get(ctx, path)
	if err != nil {
		return "", err
	}

	raw, err := ToYAML(o, showManaged)
	if err != nil {
		return "", fmt.Errorf("unable to marshal resource %w", err)
	}
	return raw, nil
}

// Delete deletes a resource.
func (g *Generic) Delete(ctx context.Context, path string, propagation *metav1.DeletionPropagation, grace Grace) error {
	ns, n := client.Namespaced(path)
	conn, err := g.clientFor(ctx)
	if err != nil {
		return err
	}
	auth, err := conn.CanI(ns, g.gvr, n, []string{client.DeleteVerb})
	if err != nil {
		return err
	}
	if !auth {
		return fmt.Errorf("user is not authorized to delete %s", path)
	}

	var gracePeriod *int64
	if grace != DefaultGrace {
		gracePeriod = (*int64)(&grace)
	}
	opts := metav1.DeleteOptions{
		PropagationPolicy:  propagation,
		GracePeriodSeconds: gracePeriod,
	}

	dial, err := g.dynClientFor(ctx)
	if err != nil {
		return err
	}
	if client.IsClusterScoped(ns) {
		return dial.Delete(ctx, n, opts)
	}
	ctx, cancel := context.WithTimeout(ctx, conn.Config().CallTimeout())
	defer cancel()

	return dial.Namespace(ns).Delete(ctx, n, opts)
}

func (g *Generic) dynClient() (dynamic.NamespaceableResourceInterface, error) {
	dial, err := g.Client().DynDial()
	if err != nil {
		return nil, err
	}

	return dial.Resource(g.gvr.GVR()), nil
}

// dynClientFor returns a dynamic client scoped to the per-row source cluster
// when ctx carries internal.KeyScopeContext (multi-context mode). Otherwise
// falls back to the primary's connection.
func (g *Generic) dynClientFor(ctx context.Context) (dynamic.NamespaceableResourceInterface, error) {
	conn, err := g.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	dial, err := conn.DynDial()
	if err != nil {
		return nil, err
	}
	return dial.Resource(g.gvr.GVR()), nil
}
