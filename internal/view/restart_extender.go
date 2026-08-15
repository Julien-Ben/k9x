// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/k9s/internal/ui/dialog"
	"github.com/derailed/tcell/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RestartExtender represents a restartable resource.
type RestartExtender struct {
	ResourceViewer
}

// NewRestartExtender returns a new extender.
func NewRestartExtender(v ResourceViewer) ResourceViewer {
	r := RestartExtender{ResourceViewer: v}
	v.AddBindKeysFn(r.bindKeys)

	return &r
}

// BindKeys creates additional menu actions.
func (r *RestartExtender) bindKeys(aa *ui.KeyActions) {
	if r.App().Config.IsReadOnly() {
		return
	}
	aa.Add(ui.KeyR, ui.NewKeyActionWithOpts("Restart", r.restartCmd,
		ui.ActionOpts{
			Visible:   true,
			Dangerous: true,
		},
	))
}

func (r *RestartExtender) restartCmd(*tcell.EventKey) *tcell.EventKey {
	refs := r.GetTable().GetSelectedRefs()
	if len(refs) == 0 || refs[0].ID == "" {
		return nil
	}
	if refuseUnscopedSelection(r.App(), refs) {
		return nil
	}

	r.Stop()
	defer r.Start()
	msg := fmt.Sprintf("Restart %s %s?", singularize(r.GVR().R()), refs[0].ID)
	if len(refs) > 1 {
		msg = fmt.Sprintf("Restart %d %s?", len(refs), r.GVR().R())
	}
	d := r.App().Styles.Dialog()

	opts := dialog.RestartDialogOpts{
		Title:        "Confirm Restart",
		Message:      msg,
		FieldManager: "kubectl-rollout",
		Ack: func(opts *metav1.PatchOptions) bool {
			rootCtx, cancel := context.WithTimeout(context.Background(), r.App().Conn().Config().CallTimeout())
			defer cancel()
			for _, ref := range refs {
				if err := r.restartRollout(scopedCtx(rootCtx, ref), ref.ID, opts); err != nil {
					r.App().Flash().Err(err)
				} else {
					r.App().Flash().Infof("Restart in progress for `%s...", ref.ID)
				}
			}
			return true
		},
		Cancel: func() {},
	}
	dialog.ShowRestart(&d, r.App().Content.Pages, &opts)

	return nil
}

func (r *RestartExtender) restartRollout(ctx context.Context, path string, opts *metav1.PatchOptions) error {
	res, err := dao.AccessorFor(r.App().factory, r.GVR())
	if err != nil {
		return err
	}
	s, ok := res.(dao.Restartable)
	if !ok {
		return errors.New("resource is not restartable")
	}

	return s.Restart(ctx, path, opts)
}

// Helpers...

func singularize(s string) string {
	if strings.LastIndex(s, "s") == len(s)-1 {
		return s[:len(s)-1]
	}

	return s
}
