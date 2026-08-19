// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"
	"fmt"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/k9s/internal/ui/dialog"
	"github.com/derailed/tcell/v2"
)

// ReplicaSet presents a replicaset viewer.
type ReplicaSet struct {
	ResourceViewer
}

// NewReplicaSet returns a new viewer.
func NewReplicaSet(gvr *client.GVR) ResourceViewer {
	r := ReplicaSet{
		ResourceViewer: NewOwnerExtender(
			NewVulnerabilityExtender(
				NewBrowser(gvr),
			),
		),
	}
	r.AddBindKeysFn(r.bindKeys)
	r.GetTable().SetEnterFn(r.showPods)

	return &r
}

func (r *ReplicaSet) bindKeys(aa *ui.KeyActions) {
	aa.Bulk(ui.KeyMap{
		tcell.KeyCtrlL: ui.NewKeyAction("Rollback", r.rollbackCmd, true),
	})
}

func (*ReplicaSet) showPods(app *App, _ ui.Tabular, _ *client.GVR, path string, sel RowIdent) {
	var drs dao.ReplicaSet
	scope := sel.Source
	ctx := context.Background()
	if scope != "" {
		ctx = context.WithValue(ctx, internal.KeyScopeContext, scope)
	}
	rs, err := drs.LoadWithContext(ctx, app.factory, path)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showPodsFromSelector(app, path, rs.Spec.Selector, scope)
}

func (r *ReplicaSet) rollbackCmd(evt *tcell.EventKey) *tcell.EventKey {
	row := r.GetTable().GetSelectedRowRef()
	if row == nil || row.ID == "" {
		return evt
	}
	ref := row.Ident()
	if refuseUnscopedSelection(r.App(), []model1.RowIdent{ref}) {
		return nil
	}

	msg := fmt.Sprintf("Rollback %s %s?", r.GVR(), ref.ID)

	d := r.App().Styles.Dialog()
	dialog.ShowConfirm(&d, r.App().Content.Pages, "Rollback", msg, func() {
		r.App().Flash().Infof("Rolling back %s %s", r.GVR(), ref.ID)
		var drs dao.ReplicaSet
		drs.Init(r.App().factory, r.GVR())
		if err := drs.Rollback(scopedCtx(context.Background(), ref), ref.ID); err != nil {
			r.App().Flash().Err(err)
		} else {
			r.App().Flash().Infof("%s successfully rolled back", ref.ID)
		}
		r.Refresh()
	}, func() {})

	return nil
}
