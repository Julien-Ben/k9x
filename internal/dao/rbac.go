// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao

import (
	"context"
	"fmt"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	_ Accessor = (*Rbac)(nil)
	_ Nuker    = (*Rbac)(nil)
)

// Rbac represents a model for listing rbac resources.
type Rbac struct {
	Resource
}

// List lists out rbac resources.
func (r *Rbac) List(ctx context.Context, ns string) ([]runtime.Object, error) {
	gvr, ok := ctx.Value(internal.KeyGVR).(*client.GVR)
	if !ok {
		return nil, fmt.Errorf("expecting a context gvr")
	}
	path, ok := ctx.Value(internal.KeyPath).(string)
	if !ok || path == "" {
		return r.Resource.List(ctx, ns)
	}

	switch gvr.R() {
	case "clusterrolebindings":
		return r.loadClusterRoleBinding(ctx, path)
	case "rolebindings":
		return r.loadRoleBinding(ctx, path)
	case "clusterroles":
		return r.loadClusterRole(ctx, path)
	case "roles":
		return r.loadRole(ctx, path)
	default:
		return nil, fmt.Errorf("expecting clusterrole/role but found %s", gvr.R())
	}
}

func (r *Rbac) loadClusterRoleBinding(ctx context.Context, path string) ([]runtime.Object, error) {
	crbo, err := getRes(r.getFactory(), ctx, client.CrbGVR, path)
	if err != nil {
		return nil, err
	}
	var crb rbacv1.ClusterRoleBinding
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(crbo.(*unstructured.Unstructured).Object, &crb)
	if err != nil {
		return nil, err
	}

	cro, err := getRes(r.getFactory(), ctx, client.CrGVR, client.FQN("-", crb.RoleRef.Name))
	if err != nil {
		return nil, err
	}
	var cr rbacv1.ClusterRole
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(cro.(*unstructured.Unstructured).Object, &cr)
	if err != nil {
		return nil, err
	}

	return asRuntimeObjects(parseRules(client.ClusterScope, "-", cr.Rules)), nil
}

func (r *Rbac) loadRoleBinding(ctx context.Context, path string) ([]runtime.Object, error) {
	rbo, err := getRes(r.getFactory(), ctx, client.RobGVR, path)
	if err != nil {
		return nil, err
	}
	var rb rbacv1.RoleBinding
	if e := runtime.DefaultUnstructuredConverter.FromUnstructured(rbo.(*unstructured.Unstructured).Object, &rb); e != nil {
		return nil, e
	}

	if rb.RoleRef.Kind == "ClusterRole" {
		cro, e := getRes(r.getFactory(), ctx, client.CrGVR, client.FQN("-", rb.RoleRef.Name))
		if e != nil {
			return nil, e
		}
		var cr rbacv1.ClusterRole
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(cro.(*unstructured.Unstructured).Object, &cr)
		if err != nil {
			return nil, err
		}
		return asRuntimeObjects(parseRules(client.ClusterScope, "-", cr.Rules)), nil
	}

	ro, err := getRes(r.getFactory(), ctx, client.RoGVR, client.FQN(rb.Namespace, rb.RoleRef.Name))
	if err != nil {
		return nil, err
	}
	var role rbacv1.Role
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(ro.(*unstructured.Unstructured).Object, &role)
	if err != nil {
		return nil, err
	}

	return asRuntimeObjects(parseRules(client.ClusterScope, "-", role.Rules)), nil
}

func (r *Rbac) loadClusterRole(ctx context.Context, fqn string) ([]runtime.Object, error) {
	o, err := getRes(r.getFactory(), ctx, client.CrGVR, fqn)
	if err != nil {
		return nil, err
	}
	var cr rbacv1.ClusterRole
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &cr)
	if err != nil {
		return nil, err
	}

	return asRuntimeObjects(parseRules(client.ClusterScope, "-", cr.Rules)), nil
}

func (r *Rbac) loadRole(ctx context.Context, path string) ([]runtime.Object, error) {
	o, err := getRes(r.getFactory(), ctx, client.RoGVR, path)
	if err != nil {
		return nil, err
	}
	var ro rbacv1.Role
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.(*unstructured.Unstructured).Object, &ro)
	if err != nil {
		return nil, err
	}

	return asRuntimeObjects(parseRules(client.ClusterScope, "-", ro.Rules)), nil
}

func asRuntimeObjects(rr render.Policies) []runtime.Object {
	oo := make([]runtime.Object, len(rr))
	for i, r := range rr {
		oo[i] = r
	}

	return oo
}

func parseRules(ns, binding string, rules []rbacv1.PolicyRule) render.Policies {
	pp := make(render.Policies, 0, len(rules))
	for _, rule := range rules {
		for _, grp := range rule.APIGroups {
			if grp == "" {
				grp = "core"
			}
			for _, res := range rule.Resources {
				for _, na := range rule.ResourceNames {
					pp = pp.Upsert(render.NewPolicyRes(ns, binding, FQN(res, na), grp, rule.Verbs))
				}
				pp = pp.Upsert(render.NewPolicyRes(ns, binding, FQN(grp, res), grp, rule.Verbs))
			}
		}
		for _, nres := range rule.NonResourceURLs {
			if nres[0] != '/' {
				nres = "/" + nres
			}
			pp = pp.Upsert(render.NewPolicyRes(ns, binding, nres, client.NA, rule.Verbs))
		}
	}

	return pp
}
