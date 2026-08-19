// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
)

// Rbac presents an RBAC policy viewer.
type Rbac struct {
	ResourceViewer
}

// NewRbac returns a new viewer.
func NewRbac(gvr *client.GVR) ResourceViewer {
	r := Rbac{
		ResourceViewer: NewBrowser(gvr),
	}
	r.AddBindKeysFn(r.bindKeys)
	r.GetTable().SetSortCol("API-GROUP", true)
	r.GetTable().SetEnterFn(blankEnterFn)

	return &r
}

func (*Rbac) bindKeys(aa *ui.KeyActions) {
	aa.Delete(ui.KeyShiftA, tcell.KeyCtrlSpace, ui.KeySpace)
}

func showRules(app *App, _ ui.Tabular, gvr *client.GVR, path string, sel RowIdent) {
	v := NewRbac(client.RbacGVR)
	v.SetContextFn(rbacCtx(gvr, path, sel.Source))

	if err := app.inject(v, false); err != nil {
		app.Flash().Err(err)
	}
}

func rbacCtx(gvr *client.GVR, path, scope string) ContextFunc {
	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, internal.KeyPath, path)
		ctx = context.WithValue(ctx, internal.KeyGVR, gvr)
		if scope != "" {
			ctx = context.WithValue(ctx, internal.KeyScopeContext, scope)
		}
		return ctx
	}
}

func blankEnterFn(_ *App, _ ui.Tabular, _ *client.GVR, _ string, _ RowIdent) {}
