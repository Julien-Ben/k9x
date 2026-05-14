// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render

import (
	"context"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/model1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SourceContextAnnotation is the annotation key MultiFactory uses to tag each
// listed object with the kubeconfig context name it came from. Kept in this
// package so the wrapper renderer and the multi-factory share a single source
// of truth.
const SourceContextAnnotation = "k9s.io/source-context"

// MultiContextRenderer wraps any model1.Renderer and adds a leading CONTEXT
// column populated from the SourceContextAnnotation set by MultiFactory on
// each object before rendering.
//
// The CONTEXT column is prepended at the START of the header (and of
// row.Fields). Known side effects of this choice:
//   - In multi mode, namespace-view and context-view colorers that hardcode
//     `Fields[0]` to mean "the name column" (internal/render/ns.go:45,
//     internal/render/context.go:28) compare against the ctxName instead of
//     the name. Cosmetic only — those colorers add an active-row highlight;
//     in multi mode the highlight just doesn't trigger. Data integrity is
//     unaffected because column order in the UI is driven by Header, not by
//     hardcoded positions.
type MultiContextRenderer struct {
	inner model1.Renderer
}

// NewMultiContextRenderer returns a renderer that delegates to inner and adds
// a CONTEXT column.
func NewMultiContextRenderer(inner model1.Renderer) *MultiContextRenderer {
	return &MultiContextRenderer{inner: inner}
}

// IsGeneric delegates to the inner renderer.
func (m *MultiContextRenderer) IsGeneric() bool {
	return m.inner.IsGeneric()
}

// ColorerFunc delegates to the inner renderer.
func (m *MultiContextRenderer) ColorerFunc() model1.ColorerFunc {
	return m.inner.ColorerFunc()
}

// SetViewSetting delegates to the inner renderer.
func (m *MultiContextRenderer) SetViewSetting(vs *config.ViewSetting) {
	m.inner.SetViewSetting(vs)
}

// Healthy delegates to the inner renderer.
func (m *MultiContextRenderer) Healthy(ctx context.Context, o any) error {
	return m.inner.Healthy(ctx, o)
}

// Header returns the inner header prefixed with a leading CONTEXT column.
func (m *MultiContextRenderer) Header(ns string) model1.Header {
	inner := m.inner.Header(ns)
	out := make(model1.Header, 0, len(inner)+1)
	out = append(out, model1.HeaderColumn{Name: "CONTEXT"})
	out = append(out, inner...)
	return out
}

// SetTable delegates to the inner renderer when it implements model1.Generic.
// Without this, GenericHydrate (used by CRDs and other table-driven views)
// fails the `re.(Generic)` type assertion against the wrapper in multi mode
// with "expecting generic renderer but got *MultiContextRenderer".
func (m *MultiContextRenderer) SetTable(ns string, table *metav1.Table) {
	if g, ok := m.inner.(model1.Generic); ok {
		g.SetTable(ns, table)
	}
}

// Render delegates to the inner renderer then prepends the source-context
// annotation as the row's leading CONTEXT field. If the annotation is missing
// (e.g. single-context mode using the wrapper by mistake), the CONTEXT cell
// is left blank rather than failing the render.
func (m *MultiContextRenderer) Render(o any, ns string, row *model1.Row) error {
	if err := m.inner.Render(o, ns, row); err != nil {
		return err
	}
	ctxName := extractSourceContext(o)
	row.Source = ctxName
	row.Fields = append(model1.Fields{ctxName}, row.Fields...)
	return nil
}

// extractSourceContext reaches into the underlying unstructured object to
// read the source-context annotation. DAOs may wrap unstructured in
// renderer-specific types (PodWithMetrics, NodeWithMetrics) before passing to
// Render, so we unwrap before asserting.
func extractSourceContext(o any) string {
	u := underlyingUnstructured(o)
	if u == nil {
		return ""
	}
	return u.GetAnnotations()[SourceContextAnnotation]
}

func underlyingUnstructured(o any) *unstructured.Unstructured {
	switch v := o.(type) {
	case *unstructured.Unstructured:
		return v
	case *PodWithMetrics:
		return v.Raw
	case *NodeWithMetrics:
		return v.Raw
	default:
		return nil
	}
}
