// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"
	"errors"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const scaleDialogKey = "scale"

// Deploy represents a deployment view.
type Deploy struct {
	ResourceViewer
}

// NewDeploy returns a new deployment view.
func NewDeploy(gvr *client.GVR) ResourceViewer {
	var d Deploy
	d.ResourceViewer = NewPortForwardExtender(
		NewVulnerabilityExtender(
			NewRestartExtender(
				NewScaleExtender(
					NewImageExtender(
						NewOwnerExtender(
							NewLogsExtender(NewBrowser(gvr), d.logOptions),
						),
					),
				),
			),
		),
	)
	d.AddBindKeysFn(d.bindKeys)
	d.GetTable().SetEnterFn(d.showPods)

	return &d
}

func (d *Deploy) bindKeys(aa *ui.KeyActions) {
	aa.Bulk(ui.KeyMap{
		ui.KeyZ: ui.NewKeyAction("ReplicaSets", d.replicaSetsCmd, true),
	})
}

func (d *Deploy) logOptions(prev bool) (*dao.LogOptions, error) {
	path := d.GetTable().GetSelectedItem()
	if path == "" {
		return nil, errors.New("you must provide a selection")
	}
	dp, err := d.getInstance(path)
	if err != nil {
		return nil, err
	}

	return podLogOptions(d.App(), path, prev, &dp.ObjectMeta, &dp.Spec.Template.Spec), nil
}

func (d *Deploy) replicaSetsCmd(evt *tcell.EventKey) *tcell.EventKey {
	dName := d.GetTable().GetSelectedItem()
	if dName == "" {
		return evt
	}
	scope := d.GetTable().selectedContext()
	dp, err := d.getInstanceForScope(dName, scope)
	if err != nil {
		d.App().Flash().Err(err)
		return nil
	}
	showReplicasetsFromSelector(d.App(), dName, dp.Spec.Selector, scope)
	return nil
}

func (d *Deploy) showPods(app *App, _ ui.Tabular, _ *client.GVR, fqn string, sel RowIdent) {
	scope := sel.Source
	dp, err := d.getInstanceForScope(fqn, scope)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showPodsFromSelector(app, fqn, dp.Spec.Selector, scope)
}

func (d *Deploy) getInstance(fqn string) (*appsv1.Deployment, error) {
	return d.getInstanceForScope(fqn, "")
}

// getInstanceForScope fetches the deployment from the cluster identified by
// scopeCtx ("" = primary / single-context). Used by drill-down + restart so
// multi-context rows resolve to their source cluster.
func (d *Deploy) getInstanceForScope(fqn, scopeCtx string) (*appsv1.Deployment, error) {
	var dp dao.Deployment
	dp.Init(d.App().factory, d.GVR())

	ctx := context.Background()
	if scopeCtx != "" {
		ctx = context.WithValue(ctx, internal.KeyScopeContext, scopeCtx)
	}
	return dp.GetInstanceWithContext(ctx, fqn)
}

// ----------------------------------------------------------------------------
// Helpers...

func showPodsFromSelector(app *App, path string, sel *metav1.LabelSelector, scopeCtx string) {
	l, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showPods(app, path, l, "", scopeCtx)
}

func showReplicasetsFromSelector(app *App, path string, sel *metav1.LabelSelector, scopeCtx string) {
	l, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showReplicasets(app, path, l, "", scopeCtx)
}
