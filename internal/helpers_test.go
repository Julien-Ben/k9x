// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package internal_test

import (
	"testing"

	"github.com/derailed/k9s/internal"
	"github.com/stretchr/testify/assert"
)

func TestIsContextSelector(t *testing.T) {
	uu := map[string]struct {
		s       string
		scope   string
		inverse bool
		ok      bool
	}{
		"empty":          {s: ""},
		"plain":          {s: "ctx=kind-a", scope: "kind-a", ok: true},
		"with-dots":      {s: "ctx=arn.aws.east-1", scope: "arn.aws.east-1", ok: true},
		"inverse":        {s: "!ctx=kind-b", scope: "kind-b", inverse: true, ok: true},
		"trailing-junk":  {s: "ctx=kind-a x"},
		"no-equal":       {s: "ctx"},
		"empty-name":     {s: "ctx="},
		"different-key":  {s: "context=kind-a"},
		"label-selector": {s: "-l app=ctx"},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			scope, inverse, ok := internal.IsContextSelector(u.s)
			assert.Equal(t, u.ok, ok)
			if u.ok {
				assert.Equal(t, u.scope, scope)
				assert.Equal(t, u.inverse, inverse)
			}
		})
	}
}

func TestIsLabelSelector(t *testing.T) {
	uu := map[string]struct {
		s  string
		ok bool
	}{
		"empty":       {s: ""},
		"cool":        {s: "-l app=fred,env=blee", ok: true},
		"no-flag":     {s: "app=fred,env=blee", ok: true},
		"no-space":    {s: "-lapp=fred,env=blee", ok: true},
		"wrong-flag":  {s: "-f app=fred,env=blee"},
		"missing-key": {s: "=fred"},
		"missing-val": {s: "fred="},
	}

	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, u.ok, internal.IsLabelSelector(u.s))
		})
	}
}
