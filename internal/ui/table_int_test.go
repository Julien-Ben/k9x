// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/dao"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/k9s/internal/model1"
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
		"single mode hides CONTEXT": {multi: false, excl: true,
			reason: "in single-context mode the column is irrelevant"},
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

// multiCtxStubModel implements ui.Tabular with only what shouldExcludeColumn
// needs to read.
type multiCtxStubModel struct{ multi bool }

func (*multiCtxStubModel) ClusterWide() bool                                        { return false }
func (*multiCtxStubModel) GetNamespace() string                                     { return "" }
func (*multiCtxStubModel) SetNamespace(string)                                      {}
func (*multiCtxStubModel) InNamespace(string) bool                                  { return false }
func (m *multiCtxStubModel) MultiContext() bool                                     { return m.multi }
func (*multiCtxStubModel) SetMultiContext(bool)                                     {}
func (*multiCtxStubModel) Get(context.Context, string) (runtime.Object, error)     { return nil, nil }
func (*multiCtxStubModel) SetInstance(string)                                      {}
func (*multiCtxStubModel) SetLabelSelector(labels.Selector)                         {}
func (*multiCtxStubModel) GetLabelSelector() labels.Selector                        { return nil }
func (*multiCtxStubModel) Empty() bool                                              { return true }
func (*multiCtxStubModel) RowCount() int                                            { return 0 }
func (*multiCtxStubModel) Peek() *model1.TableData                                  { return nil }
func (*multiCtxStubModel) Watch(context.Context) error                              { return nil }
func (*multiCtxStubModel) Refresh(context.Context) error                            { return nil }
func (*multiCtxStubModel) SetRefreshRate(time.Duration)                             {}
func (*multiCtxStubModel) AddListener(model.TableListener)                          {}
func (*multiCtxStubModel) RemoveListener(model.TableListener)                       {}
func (*multiCtxStubModel) Delete(context.Context, string, *metav1.DeletionPropagation, dao.Grace) error {
	return nil
}
func (*multiCtxStubModel) SetViewSetting(context.Context, *config.ViewSetting) {}
