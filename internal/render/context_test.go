// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package render_test

import (
	"testing"

	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd/api"
)

func TestContextHeader(t *testing.T) {
	var c render.Context

	assert.Len(t, c.Header(""), 4)
}

func TestContextManagerHeader(t *testing.T) {
	var c render.ContextManager

	assert.Equal(t, model1.Header{
		model1.HeaderColumn{Name: "WATCH"},
		model1.HeaderColumn{Name: "NAME"},
		model1.HeaderColumn{Name: "CLUSTER"},
		model1.HeaderColumn{Name: "AUTHINFO"},
		model1.HeaderColumn{Name: "NAMESPACE"},
		model1.HeaderColumn{Name: "HEALTH"},
	}, c.Header(""))
}

func TestContextRender(t *testing.T) {
	uu := map[string]struct {
		ctx *render.NamedContext
		e   model1.Row
	}{
		"active": {
			ctx: &render.NamedContext{
				Name: "c1",
				Context: &api.Context{
					LocationOfOrigin: "fred",
					Cluster:          "c1",
					AuthInfo:         "u1",
					Namespace:        "ns1",
				},
				Config: &config{},
			},
			e: model1.Row{
				ID:     "c1",
				Fields: model1.Fields{"c1", "c1", "u1", "ns1"},
			},
		},
	}

	var r render.Context
	for k := range uu {
		uc := uu[k]
		t.Run(k, func(t *testing.T) {
			row := model1.NewRow(4)
			err := r.Render(uc.ctx, "", &row)

			require.NoError(t, err)
			assert.Equal(t, uc.e, row)
		})
	}
}

func TestContextManagerRender(t *testing.T) {
	uu := map[string]struct {
		ctx *render.NamedContext
		e   model1.Row
	}{
		"enabled": {
			ctx: &render.NamedContext{
				Name: "c1",
				Context: &api.Context{
					LocationOfOrigin: "fred",
					Cluster:          "c1",
					AuthInfo:         "u1",
					Namespace:        "ns1",
				},
				Config:  &config{},
				Enabled: true,
				Health:  render.ContextHealthHealthy,
			},
			e: model1.Row{
				ID:     "c1",
				Fields: model1.Fields{"*", "c1", "c1", "u1", "ns1", "Healthy"},
			},
		},
		"disabled": {
			ctx: &render.NamedContext{
				Name: "c2",
				Context: &api.Context{
					LocationOfOrigin: "fred",
					Cluster:          "c2",
					AuthInfo:         "u2",
					Namespace:        "ns2",
				},
				Config:  &config{},
				Enabled: false,
				Health:  render.ContextHealthDisabled,
			},
			e: model1.Row{
				ID:     "c2",
				Fields: model1.Fields{"", "c2", "c2", "u2", "ns2", "Disabled"},
			},
		},
	}

	var r render.ContextManager
	for k := range uu {
		uc := uu[k]
		t.Run(k, func(t *testing.T) {
			row := model1.NewRow(6)
			err := r.Render(uc.ctx, "", &row)

			require.NoError(t, err)
			assert.Equal(t, uc.e, row)
		})
	}
}

// ----------------------------------------------------------------------------
// Helpers...

type config struct{}

func (config) CurrentContextName() (string, error) {
	return "fred", nil
}
