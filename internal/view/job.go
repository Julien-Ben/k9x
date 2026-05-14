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
	batchv1 "k8s.io/api/batch/v1"
)

// Job represents a job viewer.
type Job struct {
	ResourceViewer
}

// NewJob returns a new viewer.
func NewJob(gvr *client.GVR) ResourceViewer {
	var j Job

	j.ResourceViewer = NewVulnerabilityExtender(
		NewOwnerExtender(
			NewLogsExtender(NewBrowser(gvr), j.logOptions),
		),
	)
	j.GetTable().SetEnterFn(j.showPods)
	j.GetTable().SetSortCol("AGE", true)

	return &j
}

func (*Job) showPods(app *App, m ui.Tabular, _ *client.GVR, path string) {
	scope := extractRowScope(m, path)
	ctx := context.Background()
	if scope != "" {
		ctx = context.WithValue(ctx, internal.KeyScopeContext, scope)
	}
	var jdao dao.Job
	jdao.Init(app.factory, client.JobGVR)
	job, err := jdao.GetInstanceWithContext(ctx, path)
	if err != nil {
		app.Flash().Err(err)
		return
	}

	showPodsFromSelector(app, path, job.Spec.Selector, scope)
}

func (j *Job) logOptions(prev bool) (*dao.LogOptions, error) {
	path := j.GetTable().GetSelectedItem()
	if path == "" {
		return nil, errors.New("you must provide a selection")
	}
	job, err := j.getInstance(path)
	if err != nil {
		return nil, err
	}

	return podLogOptions(j.App(), path, prev, &job.ObjectMeta, &job.Spec.Template.Spec), nil
}

func (j *Job) getInstance(fqn string) (*batchv1.Job, error) {
	var job dao.Job
	job.Init(j.App().factory, client.JobGVR)

	return job.GetInstance(fqn)
}
