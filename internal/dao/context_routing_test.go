// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

var errScopedDial = errors.New("scoped dial")

func TestCronJobRunRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, cronJobObject(t))
	var cj dao.CronJob
	cj.Init(f, client.CjGVR)

	err := cj.Run(scopedRoutingCtx("ctx-b"), "default/cj")
	require.ErrorIs(t, err, errScopedDial)
	assert.Equal(t, []string{"ctx-b"}, f.clientForScopes)
	assert.Equal(t, []string{"ctx-b"}, f.getWithContextScopes)
	assert.Zero(t, f.plainGetCalls)
	assert.Equal(t, 1, f.conn.dialCalls)
}

func TestNodeToggleCordonRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, nodeObject(t, true))
	var n dao.Node
	n.Init(f, client.NodeGVR)

	err := n.ToggleCordon(scopedRoutingCtx("ctx-b"), "node-a", false)
	require.ErrorIs(t, err, errScopedDial)
	assert.Equal(t, []string{"ctx-b", "ctx-b"}, f.clientForScopes)
	assert.Equal(t, []string{"ctx-b"}, f.getWithContextScopes)
	assert.Zero(t, f.plainGetCalls)
	assert.Equal(t, 1, f.conn.dialCalls)
}

func TestNodeDrainRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, nodeObject(t, true))
	var n dao.Node
	n.Init(f, client.NodeGVR)

	err := n.Drain(scopedRoutingCtx("ctx-b"), "node-a", dao.DrainOptions{}, bytes.NewBuffer(nil))
	require.ErrorIs(t, err, errScopedDial)
	assert.Equal(t, []string{"ctx-b", "ctx-b"}, f.clientForScopes)
	assert.Equal(t, []string{"ctx-b"}, f.getWithContextScopes)
	assert.Zero(t, f.plainGetCalls)
	assert.Equal(t, 1, f.conn.dialCalls)
}

func TestPodImageReadsRouteViaScopeContext(t *testing.T) {
	obj := routedPodObject(t, true)
	f := newScopedRoutingFactory(t, obj)
	var pod dao.Pod
	pod.Init(f, client.PodGVR)
	ctx := scopedRoutingCtx("ctx-b")

	spec, err := pod.GetPodSpec(ctx, "default/pod-a")
	require.NoError(t, err)
	require.Len(t, spec.Containers, 1)
	assert.Equal(t, "ctx-b", f.getWithContextScopes[0])
	assert.Zero(t, f.plainGetCalls)

	err = pod.SetImages(ctx, "default/pod-a", nil)
	require.EqualError(t, err, "unable to set image. This pod is managed by ReplicaSet/owner-a. Please set the image on the controller")
	assert.Equal(t, []string{"ctx-b", "ctx-b"}, f.getWithContextScopes)
	assert.Equal(t, []string{"ctx-b"}, f.clientForScopes)
	assert.Zero(t, f.plainGetCalls)
}

func TestPodListImagesRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, routedPodObject(t, false))
	var pod dao.Pod
	pod.Init(f, client.PodGVR)

	images, err := pod.ListImages(scopedRoutingCtx("ctx-b"), "default/pod-a")
	require.NoError(t, err)
	assert.Equal(t, []string{"busybox"}, images)
	assert.Equal(t, []string{"ctx-b"}, f.getWithContextScopes)
	assert.Zero(t, f.plainGetCalls)
}

func TestJobTailLogsRoutesSelectorReadsViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, asUnstructured(t, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job-a", Namespace: "default"},
		Spec: batchv1.JobSpec{Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"job": "job-a"},
		}},
	}))
	var job dao.Job
	job.Init(f, client.JobGVR)
	ctx := context.WithValue(scopedRoutingCtx("ctx-b"), internal.KeyFactory, dao.Factory(f))

	_, err := job.TailLogs(ctx, &dao.LogOptions{Path: "default/job-a"})
	require.NoError(t, err)
	assert.NotEmpty(t, f.getWithContextScopes)
	assert.NotEmpty(t, f.listWithContextScopes)
	for _, scope := range append(f.getWithContextScopes, f.listWithContextScopes...) {
		assert.Equal(t, "ctx-b", scope)
	}
	assert.Zero(t, f.plainGetCalls)
	assert.Zero(t, f.plainListCalls)
}

func TestPodSanitizeRoutesListAndDeleteViaScopeContext(t *testing.T) {
	obj := routedPodObject(t, false)
	obj.(*unstructured.Unstructured).Object["status"] = map[string]any{
		"phase":  string(v1.PodSucceeded),
		"reason": render.PhaseCompleted,
	}
	f := newScopedRoutingFactory(t, obj)
	var pod dao.Pod
	pod.Init(f, client.PodGVR)

	total, err := pod.Sanitize(scopedRoutingCtx("ctx-b"), "default")
	require.ErrorIs(t, err, errScopedDial)
	assert.Zero(t, total)
	assert.Equal(t, []string{"ctx-b"}, f.listWithContextScopes)
	assert.Equal(t, []string{"ctx-b", "ctx-b"}, f.clientForScopes)
	assert.Zero(t, f.plainListCalls)
}

func TestReplicaSetRollbackRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, replicaSetObject(t))
	var rs dao.ReplicaSet
	rs.Init(f, client.RsGVR)

	err := rs.Rollback(scopedRoutingCtx("ctx-b"), "default/rs-a")
	require.ErrorIs(t, err, errScopedDial)
	assert.Equal(t, []string{"ctx-b"}, f.getWithContextScopes)
	assert.Equal(t, []string{"ctx-b"}, f.clientForScopes)
	assert.Zero(t, f.plainGetCalls)
}

func TestWorkloadDeleteRoutesViaScopeContext(t *testing.T) {
	f := newScopedRoutingFactory(t, nil)
	var workload dao.Workload
	workload.Init(f, client.WkGVR)
	ctx := context.WithValue(scopedRoutingCtx("ctx-b"), internal.KeyGVR, client.PodGVR)

	err := workload.Delete(ctx, "default/pod-a", nil, dao.DefaultGrace)
	require.ErrorIs(t, err, errScopedDial)
	assert.Equal(t, []string{"ctx-b"}, f.clientForScopes)
	assert.Equal(t, 1, f.conn.dialCalls)
}

type scopedRoutingConn struct {
	conn

	dialCalls int
}

func (c *scopedRoutingConn) Dial() (kubernetes.Interface, error) {
	c.dialCalls++
	return nil, errScopedDial
}

func (c *scopedRoutingConn) DynDial() (dynamic.Interface, error) {
	c.dialCalls++
	return nil, errScopedDial
}

func (*scopedRoutingConn) Config() *client.Config {
	return client.NewConfig(genericclioptions.NewConfigFlags(false))
}

type scopedRoutingFactory struct {
	obj                   runtime.Object
	conn                  *scopedRoutingConn
	plainGetCalls         int
	plainListCalls        int
	clientForScopes       []string
	getWithContextScopes  []string
	listWithContextScopes []string
}

var _ dao.ContextualFactory = (*scopedRoutingFactory)(nil)

func newScopedRoutingFactory(t *testing.T, obj runtime.Object) *scopedRoutingFactory {
	t.Helper()

	return &scopedRoutingFactory{
		obj:  obj,
		conn: new(scopedRoutingConn),
	}
}

func (f *scopedRoutingFactory) Client() client.Connection {
	return makeConn()
}

func (f *scopedRoutingFactory) ClientFor(ctx context.Context) (client.Connection, error) {
	f.clientForScopes = append(f.clientForScopes, routingScope(ctx))
	return f.conn, nil
}

func (f *scopedRoutingFactory) Get(*client.GVR, string, bool, labels.Selector) (runtime.Object, error) {
	f.plainGetCalls++
	return f.obj, nil
}

func (f *scopedRoutingFactory) GetWithContext(ctx context.Context, _ *client.GVR, _ string, _ bool, _ labels.Selector) (runtime.Object, error) {
	f.getWithContextScopes = append(f.getWithContextScopes, routingScope(ctx))
	return f.obj, nil
}

func (f *scopedRoutingFactory) List(*client.GVR, string, bool, labels.Selector) ([]runtime.Object, error) {
	f.plainListCalls++
	return []runtime.Object{f.obj}, nil
}

func (f *scopedRoutingFactory) ListWithContext(ctx context.Context, _ *client.GVR, _ string, _ bool, _ labels.Selector) ([]runtime.Object, error) {
	f.listWithContextScopes = append(f.listWithContextScopes, routingScope(ctx))
	return []runtime.Object{f.obj}, nil
}

func (*scopedRoutingFactory) ForResource(string, *client.GVR) (informers.GenericInformer, error) {
	return nil, nil
}

func (*scopedRoutingFactory) CanForResource(string, *client.GVR, []string) (informers.GenericInformer, error) {
	return nil, nil
}

func (*scopedRoutingFactory) WaitForCacheSync()            {}
func (*scopedRoutingFactory) Forwarders() watch.Forwarders { return nil }
func (*scopedRoutingFactory) DeleteForwarder(string)       {}

func scopedRoutingCtx(scope string) context.Context {
	return context.WithValue(context.Background(), internal.KeyScopeContext, scope)
}

func routingScope(ctx context.Context) string {
	scope, _ := ctx.Value(internal.KeyScopeContext).(string)
	return scope
}

func cronJobObject(t *testing.T) runtime.Object {
	t.Helper()

	return asUnstructured(t, &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cj",
			Namespace: "default",
			UID:       types.UID("uid-a"),
		},
		Spec: batchv1.CronJobSpec{
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							RestartPolicy: v1.RestartPolicyNever,
							Containers: []v1.Container{
								{Name: "main", Image: "busybox"},
							},
						},
					},
				},
			},
		},
	})
}

func nodeObject(t *testing.T, unschedulable bool) runtime.Object {
	t.Helper()

	return asUnstructured(t, &v1.Node{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Node",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-a",
		},
		Spec: v1.NodeSpec{
			Unschedulable: unschedulable,
		},
	})
}

func routedPodObject(t *testing.T, controlled bool) runtime.Object {
	t.Helper()
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "main", Image: "busybox"}},
		},
	}
	if controlled {
		controller := true
		pod.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "apps/v1",
			Kind:       "ReplicaSet",
			Name:       "owner-a",
			Controller: &controller,
		}}
	}
	return asUnstructured(t, pod)
}

func replicaSetObject(t *testing.T) runtime.Object {
	t.Helper()
	controller := true
	return asUnstructured(t, &appsv1.ReplicaSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-a",
			Namespace: "default",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "deploy-a",
				Controller: &controller,
			}},
		},
	})
}

func asUnstructured(t *testing.T, obj runtime.Object) runtime.Object {
	t.Helper()

	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: m}
}
