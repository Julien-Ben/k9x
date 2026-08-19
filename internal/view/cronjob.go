// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/slogs"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/k9s/internal/ui/dialog"
	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	suspendDialogKey     = "suspend"
	lastScheduledCol     = "LAST_SCHEDULE"
	defaultSuspendStatus = "true"
)

// CronJob represents a cronjob viewer.
type CronJob struct {
	ResourceViewer
}

// NewCronJob returns a new viewer.
func NewCronJob(gvr *client.GVR) ResourceViewer {
	c := CronJob{ResourceViewer: NewVulnerabilityExtender(
		NewOwnerExtender(NewBrowser(gvr)),
	)}
	c.AddBindKeysFn(c.bindKeys)
	c.GetTable().SetEnterFn(c.showJobs)

	return &c
}

func (*CronJob) showJobs(app *App, _ ui.Tabular, gvr *client.GVR, fqn string, sel RowIdent) {
	slog.Debug("Showing Jobs", slogs.GVR, gvr, slogs.FQN, fqn)
	o, err := getScopedResource(app.factory, scopedCtx(context.Background(), sel), gvr, fqn)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	var cj batchv1.CronJob
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &cj)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	ns, _ := client.Namespaced(fqn)
	if err := app.Config.SetActiveNamespace(ns); err != nil {
		slog.Error("Unable to set active namespace during show pods", slogs.Error, err)
	}
	v := NewJob(client.JobGVR)
	v.SetContextFn(jobCtx(fqn, string(cj.UID), sel.Source))
	if err := app.inject(v, false); err != nil {
		app.Flash().Err(err)
	}
}

func jobCtx(fqn, uid, scopeCtx string) ContextFunc {
	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, internal.KeyPath, fqn)
		if scopeCtx != "" {
			ctx = context.WithValue(ctx, internal.KeyScopeContext, scopeCtx)
		}
		return context.WithValue(ctx, internal.KeyUID, uid)
	}
}

func (c *CronJob) bindKeys(aa *ui.KeyActions) {
	aa.Bulk(ui.KeyMap{
		ui.KeyT: ui.NewKeyAction("Trigger", c.triggerCmd, true),
		ui.KeyS: ui.NewKeyAction("Suspend/Resume", c.toggleSuspendCmd, true),
	})
}

func (c *CronJob) triggerCmd(evt *tcell.EventKey) *tcell.EventKey {
	refs := c.GetTable().GetSelectedRefs()
	if len(refs) == 0 {
		return evt
	}
	if refuseUnscopedSelection(c.App(), refs) {
		return nil
	}
	msg := fmt.Sprintf("Trigger CronJob: %s?", refs[0].ID)
	if len(refs) > 1 {
		msg = fmt.Sprintf("Trigger %d CronJobs?", len(refs))
	}
	d := c.App().Styles.Dialog()
	dialog.ShowConfirm(&d, c.App().Content.Pages, "Confirm Job Trigger", msg, func() {
		res, err := dao.AccessorFor(c.App().factory, c.GVR())
		if err != nil {
			c.App().Flash().Err(fmt.Errorf("no accessor for %q", c.GVR()))
			return
		}
		runner, ok := res.(dao.Runnable)
		if !ok {
			c.App().Flash().Err(fmt.Errorf("expecting a job runner resource for %q", c.GVR()))
			return
		}

		for _, ref := range refs {
			if err := runner.Run(scopedCtx(context.Background(), ref), ref.ID); err != nil {
				c.App().Flash().Errf("CronJob trigger failed for %s: %v", ref.ID, err)
			} else {
				c.App().Flash().Infof("Triggered Job %s %s", c.GVR(), ref.ID)
			}
		}
	}, func() {})

	return nil
}

func (c *CronJob) toggleSuspendCmd(evt *tcell.EventKey) *tcell.EventKey {
	table := c.GetTable()
	row := table.GetSelectedRowRef()
	if row == nil || row.ID == "" {
		return evt
	}
	sel := row.Ident()
	if refuseUnscopedSelection(c.App(), []model1.RowIdent{sel}) {
		return nil
	}

	suspendCol, ok := table.HeaderIndex("SUSPEND")
	if !ok {
		c.App().Flash().Errf("Unable to locate suspend status")
		return nil
	}
	cell := table.GetCell(table.GetSelectedRowIndex(), suspendCol)
	if cell == nil {
		c.App().Flash().Errf("Unable to assert current status")
		return nil
	}

	c.Stop()
	defer c.Start()

	c.showSuspendDialog(cell, sel)

	return nil
}

func (c *CronJob) showSuspendDialog(cell *tview.TableCell, sel model1.RowIdent) {
	title := "Suspend"

	if strings.TrimSpace(cell.Text) == defaultSuspendStatus {
		title = "Resume"
	}

	d := c.App().Styles.Dialog()
	dialog.ShowConfirm(&d, c.App().Content.Pages, title, sel.ID, func() {
		ctx, cancel := context.WithTimeout(scopedCtx(context.Background(), sel), c.App().Conn().Config().CallTimeout())
		defer cancel()

		res, err := dao.AccessorFor(c.App().factory, c.GVR())
		if err != nil {
			c.App().Flash().Err(fmt.Errorf("no accessor for %q", c.GVR()))
			return
		}

		cronJob, ok := res.(*dao.CronJob)
		if !ok {
			c.App().Flash().Errf("expecting a cron job for %q", c.GVR())
			return
		}

		if err := cronJob.ToggleSuspend(ctx, sel.ID); err != nil {
			c.App().Flash().Errf("Cronjob %s failed for %v", strings.ToLower(title), err)
			return
		}
	}, func() {})
}
