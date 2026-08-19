// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model

import (
	"context"
	"testing"

	"github.com/derailed/k9s/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dispatchDescriber struct {
	describeCalls           int
	describeContextualCalls int
	yamlCalls               int
	yamlContextualCalls     int
}

func (d *dispatchDescriber) Describe(string) (string, error) {
	d.describeCalls++
	return "plain describe", nil
}

func (d *dispatchDescriber) DescribeWithContext(context.Context, string) (string, error) {
	d.describeContextualCalls++
	return "contextual describe", nil
}

func (d *dispatchDescriber) ToYAML(string, bool) (string, error) {
	d.yamlCalls++
	return "plain yaml", nil
}

func (d *dispatchDescriber) ToYAMLWithContext(context.Context, string, bool) (string, error) {
	d.yamlContextualCalls++
	return "contextual yaml", nil
}

func TestDescriberDispatchUsesSpecializedMethodsWithoutScope(t *testing.T) {
	d := &dispatchDescriber{}

	description, err := describeDAO(t.Context(), d, "default/secret")
	require.NoError(t, err)
	yaml, err := yamlDAO(t.Context(), d, "default/secret", false)
	require.NoError(t, err)

	assert.Equal(t, "plain describe", description)
	assert.Equal(t, "plain yaml", yaml)
	assert.Equal(t, 1, d.describeCalls)
	assert.Equal(t, 1, d.yamlCalls)
	assert.Zero(t, d.describeContextualCalls)
	assert.Zero(t, d.yamlContextualCalls)
}

func TestDescriberDispatchUsesContextualMethodsWithScope(t *testing.T) {
	d := &dispatchDescriber{}
	ctx := context.WithValue(t.Context(), internal.KeyScopeContext, "ctx-b")

	description, err := describeDAO(ctx, d, "default/secret")
	require.NoError(t, err)
	yaml, err := yamlDAO(ctx, d, "default/secret", false)
	require.NoError(t, err)

	assert.Equal(t, "contextual describe", description)
	assert.Equal(t, "contextual yaml", yaml)
	assert.Zero(t, d.describeCalls)
	assert.Zero(t, d.yamlCalls)
	assert.Equal(t, 1, d.describeContextualCalls)
	assert.Equal(t, 1, d.yamlContextualCalls)
}
