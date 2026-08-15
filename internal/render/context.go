// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/tcell/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	ContextHealthHealthy     = "Healthy"
	ContextHealthQuarantined = "Quarantined"
	ContextHealthDisabled    = "Disabled"

	contextWatchMarker = "*"
)

// Context renders a K8s ConfigMap to screen.
type Context struct {
	Base
}

// ContextManager renders contexts as a runtime multi-context manager.
type ContextManager struct {
	Context
}

// ColorerFunc colors a resource row.
func (Context) ColorerFunc() model1.ColorerFunc {
	return func(ns string, h model1.Header, r *model1.RowEvent) tcell.Color {
		c := model1.DefaultColorer(ns, h, r)
		if idx, ok := h.IndexOf("NAME", true); ok {
			if strings.Contains(strings.TrimSpace(r.Row.Fields[idx]), "*") {
				return model1.HighlightColor
			}
		}

		return c
	}
}

// Header returns a header row.
func (Context) Header(string) model1.Header {
	return model1.Header{
		model1.HeaderColumn{Name: "NAME"},
		model1.HeaderColumn{Name: "CLUSTER"},
		model1.HeaderColumn{Name: "AUTHINFO"},
		model1.HeaderColumn{Name: "NAMESPACE"},
	}
}

// ColorerFunc colors a multi-context manager row.
func (ContextManager) ColorerFunc() model1.ColorerFunc {
	return func(ns string, h model1.Header, r *model1.RowEvent) tcell.Color {
		if idx, ok := h.IndexOf("HEALTH", true); ok && strings.TrimSpace(r.Row.Fields[idx]) == ContextHealthDisabled {
			return tcell.ColorGray
		}
		return Context{}.ColorerFunc()(ns, h, r)
	}
}

// Header returns a multi-context manager header row.
func (ContextManager) Header(string) model1.Header {
	return model1.Header{
		model1.HeaderColumn{Name: "WATCH"},
		model1.HeaderColumn{Name: "NAME"},
		model1.HeaderColumn{Name: "CLUSTER"},
		model1.HeaderColumn{Name: "AUTHINFO"},
		model1.HeaderColumn{Name: "NAMESPACE"},
		model1.HeaderColumn{Name: "HEALTH"},
	}
}

// Render renders a K8s resource to screen.
func (Context) Render(o any, _ string, r *model1.Row) error {
	ctx, ok := o.(*NamedContext)
	if !ok {
		return fmt.Errorf("expected *NamedContext, but got %T", o)
	}

	name := ctx.Name
	if ctx.IsCurrentContext(ctx.Name) {
		name += "(*)"
	}

	r.ID = ctx.Name
	r.Fields = model1.Fields{
		name,
		ctx.Context.Cluster,
		ctx.Context.AuthInfo,
		ctx.Context.Namespace,
	}

	return nil
}

// Render renders a context manager row.
func (ContextManager) Render(o any, _ string, r *model1.Row) error {
	ctx, ok := o.(*NamedContext)
	if !ok {
		return fmt.Errorf("expected *NamedContext, but got %T", o)
	}

	name := ctx.Name
	if ctx.IsCurrentContext(ctx.Name) {
		name += "(*)"
	}
	watch := ""
	if ctx.Enabled {
		watch = contextWatchMarker
	}
	health := ctx.Health
	if health == "" {
		health = ContextHealthHealthy
	}

	r.ID = ctx.Name
	r.Fields = model1.Fields{
		watch,
		name,
		ctx.Context.Cluster,
		ctx.Context.AuthInfo,
		ctx.Context.Namespace,
		health,
	}

	return nil
}

// Helpers...

// NamedContext represents a named cluster context.
type NamedContext struct {
	Name    string
	Context *api.Context
	Config  ContextNamer
	Managed bool
	Enabled bool
	Health  string
}

// ContextNamer represents a named context.
type ContextNamer interface {
	CurrentContextName() (string, error)
}

// NewNamedContext returns a new named context.
func NewNamedContext(c ContextNamer, n string, ctx *api.Context) *NamedContext {
	return &NamedContext{Name: n, Context: ctx, Config: c, Enabled: true}
}

// IsCurrentContext return the active context name.
func (c *NamedContext) IsCurrentContext(n string) bool {
	cl, err := c.Config.CurrentContextName()
	if err != nil {
		slog.Error("Fail to retrieve current context. Exiting!")
		os.Exit(1)
	}
	return cl == n
}

// GetObjectKind returns a schema object.
func (*NamedContext) GetObjectKind() schema.ObjectKind {
	return nil
}

// DeepCopyObject returns a container copy.
func (c *NamedContext) DeepCopyObject() runtime.Object {
	return c
}
