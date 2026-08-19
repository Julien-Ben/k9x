// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/tcell/v2"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestShouldExcludeColumn_Context(t *testing.T) {
	uu := map[string]struct {
		multi  bool
		excl   bool
		reason string
	}{
		"single mode preserves schema CONTEXT": {multi: false, excl: false,
			reason: "without the wrapper, CONTEXT belongs to the resource schema"},
		"multi mode shows CONTEXT": {multi: true, excl: false,
			reason: "in multi-context mode the column is mandatory"},
	}
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			tbl := &Table{
				SelectTable: &SelectTable{model: &multiCtxStubModel{multi: u.multi}},
			}
			got := tbl.shouldExcludeColumn(model1.HeaderColumn{Name: "CONTEXT"})
			assert.Equal(t, u.excl, got, u.reason)
		})
	}
}

func TestTableContextSortActionRequiresContextHeader(t *testing.T) {
	uu := map[string]struct {
		header model1.Header
		want   bool
	}{
		"multi resource with context header": {
			header: model1.Header{model1.HeaderColumn{Name: "CONTEXT"}, model1.HeaderColumn{Name: "NAME"}},
			want:   true,
		},
		"context manager without context header": {
			header: model1.Header{model1.HeaderColumn{Name: "WATCH"}, model1.HeaderColumn{Name: "NAME"}, model1.HeaderColumn{Name: "HEALTH"}},
			want:   false,
		},
	}
	for k := range uu {
		u := uu[k]
		t.Run(k, func(t *testing.T) {
			data := model1.NewTableDataWithRows(client.PodGVR, u.header, model1.NewRowEvents(0))
			tbl := &Table{
				SelectTable: &SelectTable{model: &multiCtxStubModel{multi: true, data: data}},
				actions:     NewKeyActions(),
				cmdBuff:     model.NewFishBuff('/', model.FilterBuffer),
			}

			tbl.doUpdate(data)

			_, got := tbl.actions.Get(KeyShiftX)
			assert.Equal(t, u.want, got)
		})
	}
}

func TestTableUpdatePreservesSingleContextShiftXBinding(t *testing.T) {
	header := model1.Header{model1.HeaderColumn{Name: "NAME"}}
	data := model1.NewTableDataWithRows(client.PodGVR, header, model1.NewRowEvents(0))
	tbl := &Table{
		SelectTable: &SelectTable{model: &multiCtxStubModel{data: data}},
		actions:     NewKeyActions(),
		cmdBuff:     model.NewFishBuff('/', model.FilterBuffer),
	}
	tbl.actions.Add(KeyShiftX, NewKeyAction("Custom", func(evt *tcell.EventKey) *tcell.EventKey { return evt }, true))

	tbl.doUpdate(data)

	action, ok := tbl.actions.Get(KeyShiftX)
	assert.True(t, ok)
	assert.Equal(t, "Custom", action.Description)
}

// multiCtxStubModel implements ui.Tabular with only what shouldExcludeColumn
// needs to read.
type multiCtxStubModel struct {
	multi bool
	data  *model1.TableData
}

func (*multiCtxStubModel) ClusterWide() bool                                   { return false }
func (*multiCtxStubModel) GetNamespace() string                                { return "" }
func (*multiCtxStubModel) SetNamespace(string)                                 {}
func (*multiCtxStubModel) InNamespace(string) bool                             { return false }
func (m *multiCtxStubModel) MultiContext() bool                                { return m.multi }
func (*multiCtxStubModel) SetMultiContext(bool)                                {}
func (*multiCtxStubModel) Get(context.Context, string) (runtime.Object, error) { return nil, nil }
func (*multiCtxStubModel) SetInstance(string)                                  {}
func (*multiCtxStubModel) SetLabelSelector(labels.Selector)                    {}
func (*multiCtxStubModel) GetLabelSelector() labels.Selector                   { return nil }
func (*multiCtxStubModel) Empty() bool                                         { return true }
func (*multiCtxStubModel) RowCount() int                                       { return 0 }
func (m *multiCtxStubModel) Peek() *model1.TableData {
	return m.data
}
func (*multiCtxStubModel) Watch(context.Context) error        { return nil }
func (*multiCtxStubModel) Refresh(context.Context) error      { return nil }
func (*multiCtxStubModel) SetRefreshRate(time.Duration)       {}
func (*multiCtxStubModel) AddListener(model.TableListener)    {}
func (*multiCtxStubModel) RemoveListener(model.TableListener) {}
func (*multiCtxStubModel) Delete(context.Context, string, *metav1.DeletionPropagation, dao.Grace) error {
	return nil
}
func (*multiCtxStubModel) SetViewSetting(context.Context, *config.ViewSetting) {}
