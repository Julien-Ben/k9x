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
	appsv1 "k8s.io/api/apps/v1"
)

// StatefulSet represents a statefulset viewer.
type StatefulSet struct {
	ResourceViewer
}

// NewStatefulSet returns a new viewer.
func NewStatefulSet(gvr *client.GVR) ResourceViewer {
	var s StatefulSet
	s.ResourceViewer = NewPortForwardExtender(
		NewVulnerabilityExtender(
			NewRestartExtender(
				NewScaleExtender(
					NewImageExtender(
						NewOwnerExtender(
							NewLogsExtender(NewBrowser(gvr), s.logOptions),
						),
					),
				),
			),
		),
	)
	s.GetTable().SetEnterFn(s.showPods)

	return &s
}

func (s *StatefulSet) logOptions(prev bool) (*dao.LogOptions, error) {
	path := s.GetTable().GetSelectedItem()
	if path == "" {
		return nil, errors.New("you must provide a selection")
	}
	sts, err := s.getInstance(path)
	if err != nil {
		return nil, err
	}

	return podLogOptions(s.App(), path, prev, &sts.ObjectMeta, &sts.Spec.Template.Spec), nil
}

func (s *StatefulSet) showPods(app *App, m ui.Tabular, _ *client.GVR, path string) {
	scope := extractRowScope(m, path)
	i, err := s.getInstanceForScope(path, scope)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showPodsFromSelector(app, path, i.Spec.Selector, scope)
}

func (s *StatefulSet) getInstance(path string) (*appsv1.StatefulSet, error) {
	return s.getInstanceForScope(path, "")
}

// getInstanceForScope fetches the statefulset from the cluster identified by
// scopeCtx ("" = primary). Used by drill-down so multi-context rows resolve
// to their source cluster.
func (s *StatefulSet) getInstanceForScope(path, scopeCtx string) (*appsv1.StatefulSet, error) {
	var sts dao.StatefulSet
	ctx := context.Background()
	if scopeCtx != "" {
		ctx = context.WithValue(ctx, internal.KeyScopeContext, scopeCtx)
	}
	return sts.GetInstanceWithContext(ctx, s.App().factory, path)
}
