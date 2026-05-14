// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package client_test

import (
	"path"
	"sort"
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsNestedGVR(t *testing.T) {
	uu := map[string]struct {
		gvr  *client.GVR
		want bool
	}{
		"containers":   {gvr: client.CoGVR, want: true},
		"references":   {gvr: client.RefGVR, want: true},
		"portforwards": {gvr: client.PfGVR, want: true},
		"benchmarks":   {gvr: client.BeGVR, want: true},
		"helm-history": {gvr: client.HmhGVR, want: true},
		"pods":         {gvr: client.PodGVR, want: false},
		"deployments":  {gvr: client.DpGVR, want: false},
		"workloads":    {gvr: client.WkGVR, want: false},
		"contexts":     {gvr: client.CtGVR, want: false},
	}
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.want, client.IsNestedGVR(u.gvr))
		})
	}
}

func TestGVRSort(t *testing.T) {
	gg := client.GVRs{
		client.PodGVR,
		client.SvcGVR,
		client.DpGVR,
	}
	sort.Sort(gg)
	assert.Equal(t, client.GVRs{
		client.PodGVR,
		client.SvcGVR,
		client.DpGVR,
	}, gg)
}

func TestGVRCan(t *testing.T) {
	uu := map[string]struct {
		vv []string
		v  string
		e  bool
	}{
		"describe":  {[]string{"get"}, "describe", true},
		"view":      {[]string{"get", "list", "watch"}, "view", true},
		"delete":    {[]string{"delete", "list", "watch"}, "delete", true},
		"no_delete": {[]string{"get", "list", "watch"}, "delete", false},
		"edit":      {[]string{"path", "update", "watch"}, "edit", true},
		"no_edit":   {[]string{"get", "list", "watch"}, "edit", false},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.Can(u.vv, u.v))
		})
	}
}

func TestGVR(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   schema.GroupVersionResource
	}{
		"full": {client.DpGVR.String(), schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
		"core": {client.PodGVR.String(), schema.GroupVersionResource{Version: "v1", Resource: "pods"}},
		"bork": {client.UsrGVR.String(), schema.GroupVersionResource{Resource: "users"}},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).GVR())
		})
	}
}

func TestAsGV(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   schema.GroupVersion
	}{
		"full": {client.DpGVR.String(), schema.GroupVersion{Group: "apps", Version: "v1"}},
		"core": {client.PodGVR.String(), schema.GroupVersion{Version: "v1"}},
		"bork": {client.UsrGVR.String(), schema.GroupVersion{}},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).GV())
		})
	}
}

func TestNewGVR(t *testing.T) {
	uu := map[string]struct {
		g, v, r string
		e       string
	}{
		"full": {"apps", "v1", "deployments", client.DpGVR.String()},
		"core": {"", "v1", "pods", client.PodGVR.String()},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(path.Join(u.g, u.v, u.r)).String())
		})
	}
}

func TestGVRAsResourceName(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   string
	}{
		"full":  {client.DpGVR.String(), "deployments.v1.apps"},
		"core":  {client.PodGVR.String(), "pods"},
		"k9s":   {client.UsrGVR.String(), "users"},
		"empty": {"", ""},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).AsResourceName())
		})
	}
}

func TestToR(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   string
	}{
		"full":  {client.DpGVR.String(), "deployments"},
		"core":  {client.PodGVR.String(), "pods"},
		"k9s":   {client.UsrGVR.String(), "users"},
		"empty": {"", ""},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).R())
		})
	}
}

func TestToG(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   string
	}{
		"full":  {client.DpGVR.String(), "apps"},
		"core":  {client.PodGVR.String(), ""},
		"k9s":   {client.UsrGVR.String(), ""},
		"empty": {"", ""},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).G())
		})
	}
}

func TestToV(t *testing.T) {
	uu := map[string]struct {
		gvr string
		e   string
	}{
		"full":  {client.DpGVR.String(), "v1"},
		"core":  {"v1beta1/pods", "v1beta1"},
		"k9s":   {client.UsrGVR.String(), ""},
		"empty": {"", ""},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.e, client.NewGVR(u.gvr).V())
		})
	}
}

func TestToString(t *testing.T) {
	uu := map[string]struct {
		gvr string
	}{
		"full":  {client.DpGVR.String()},
		"core":  {"v1beta1/pods"},
		"k9s":   {client.UsrGVR.String()},
		"empty": {""},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.gvr, client.NewGVR(u.gvr).String())
		})
	}
}
