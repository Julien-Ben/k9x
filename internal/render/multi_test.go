// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render_test

import (
	"context"
	"testing"

	k9sconfig "github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMultiContextRenderer_HappyPath(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetAPIVersion("v1")
	pod.SetKind("Pod")
	pod.SetNamespace("default")
	pod.SetName("p1")
	pod.SetAnnotations(map[string]string{
		render.SourceContextAnnotation: "ctxA",
	})

	wrapped := render.NewMultiContextRenderer(&stubRenderer{})

	row := model1.NewRow(0)
	require.NoError(t, wrapped.Render(pod, "default", &row))

	assert.Equal(t, "ctxA", row.Source, "row.Source must reflect annotation")
	require.NotEmpty(t, row.Fields, "renderer must have produced fields")
	assert.Equal(t, "ctxA", row.Fields[0],
		"first field must carry the context name (matching the leading CONTEXT column)")

	h := wrapped.Header("default")
	require.NotEmpty(t, h)
	assert.Equal(t, "CONTEXT", h[0].Name,
		"CONTEXT must be the leading header column")
}

// TestMultiContextRenderer_UnwrapsPodWithMetrics covers the real-world Pod
// path: the DAO wraps the annotated unstructured in a *PodWithMetrics before
// handing it to the renderer. The wrapper must dig through to reach the
// annotation; otherwise the CONTEXT column comes back empty for pods.
func TestMultiContextRenderer_UnwrapsPodWithMetrics(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetAPIVersion("v1")
	pod.SetKind("Pod")
	pod.SetNamespace("default")
	pod.SetName("p1")
	pod.SetAnnotations(map[string]string{
		render.SourceContextAnnotation: "ctxB",
	})
	wrapped := render.NewMultiContextRenderer(&stubRenderer{})

	row := model1.NewRow(0)
	require.NoError(t, wrapped.Render(&render.PodWithMetrics{Raw: pod}, "default", &row))

	assert.Equal(t, "ctxB", row.Source)
	assert.Equal(t, "ctxB", row.Fields[0])
}

// stubRenderer is a minimal model1.Renderer that produces a single-field row.
// It's an external-test-package stub — not part of the package API.
type stubRenderer struct{}

func (*stubRenderer) IsGeneric() bool                       { return false }
func (*stubRenderer) ColorerFunc() model1.ColorerFunc       { return nil }
func (*stubRenderer) Healthy(context.Context, any) error    { return nil }
func (*stubRenderer) Header(string) model1.Header {
	return model1.Header{
		model1.HeaderColumn{Name: "NAME"},
	}
}
func (*stubRenderer) Render(_ any, _ string, row *model1.Row) error {
	row.ID = "default/p1"
	row.Fields = model1.Fields{"p1"}
	return nil
}
func (*stubRenderer) SetViewSetting(*k9sconfig.ViewSetting) {}
