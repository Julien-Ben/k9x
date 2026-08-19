// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"testing"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/model1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestDecorateNamespaceRowsUsesNameColumnInMultiContext(t *testing.T) {
	header := model1.Header{
		model1.HeaderColumn{Name: "CONTEXT"},
		model1.HeaderColumn{Name: "NAME"},
		model1.HeaderColumn{Name: "STATUS"},
		model1.HeaderColumn{Name: "LABELS"},
		model1.HeaderColumn{Name: "VALID"},
		model1.HeaderColumn{Name: "AGE"},
	}
	data := model1.NewTableDataWithRows(client.NsGVR, header, model1.NewRowEventsWithEvts(
		model1.RowEvent{Row: model1.Row{
			ID:     "team-a",
			Source: "ctx-b",
			Fields: model1.Fields{"ctx-b", "team-a", "Active", "", "", "1m"},
		}},
	))

	decorateNamespaceRows(data, sets.New("team-a"), "team-a")

	team, ok := data.FindRowByStoreKey("ctx-b@team-a")
	require.True(t, ok)
	assert.Equal(t, "ctx-b", team.Row.Fields[0])
	assert.Equal(t, "team-a+(*)", team.Row.Fields[1])
	all, ok := data.FindRow(client.NamespaceAll)
	require.True(t, ok)
	assert.Len(t, all.Row.Fields, len(header))
	assert.Empty(t, all.Row.Fields[0])
	assert.Equal(t, client.NamespaceAll, all.Row.Fields[1])
}
