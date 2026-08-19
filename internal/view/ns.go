// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package view

import (
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/k9s/internal/ui"
	"github.com/derailed/tcell/v2"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	favNSIndicator     = "+"
	defaultNSIndicator = "(*)"
)

// Namespace represents a namespace viewer.
type Namespace struct {
	ResourceViewer
}

// NewNamespace returns a new viewer.
func NewNamespace(gvr *client.GVR) ResourceViewer {
	n := Namespace{
		ResourceViewer: NewBrowser(gvr),
	}
	n.GetTable().SetDecorateFn(n.decorate)
	n.GetTable().SetEnterFn(n.switchNs)
	n.AddBindKeysFn(n.bindKeys)

	return &n
}

func (n *Namespace) bindKeys(aa *ui.KeyActions) {
	aa.Bulk(ui.KeyMap{
		ui.KeyU: ui.NewKeyAction("Use", n.useNsCmd, true),
	})
}

func (n *Namespace) switchNs(app *App, _ ui.Tabular, _ *client.GVR, path string, _ RowIdent) {
	n.useNamespace(path)
	_, ns := client.Namespaced(path)
	app.gotoResource(client.PodGVR.String()+" "+ns, "", false, true)
}

func (n *Namespace) useNsCmd(*tcell.EventKey) *tcell.EventKey {
	path := n.GetTable().GetSelectedItem()
	if path == "" {
		return nil
	}
	n.useNamespace(path)

	return nil
}

func (n *Namespace) useNamespace(fqn string) {
	_, ns := client.Namespaced(fqn)
	if client.CleanseNamespace(n.App().Config.ActiveNamespace()) == ns {
		return
	}
	if err := n.App().switchNS(ns); err != nil {
		n.App().Flash().Err(err)
		return
	}
	if err := n.App().Config.SetActiveNamespace(ns); err != nil {
		n.App().Flash().Err(err)
		return
	}
}

func (n *Namespace) decorate(td *model1.TableData) {
	if n.App().Conn() == nil || td.RowCount() == 0 {
		return
	}
	decorateNamespaceRows(td, sets.New(n.App().Config.FavNamespaces()...), n.App().Config.ActiveNamespace())
}

func decorateNamespaceRows(td *model1.TableData, favs sets.Set[string], activeNS string) {
	nameCol, ok := td.Header().IndexOf("NAME", true)
	if !ok {
		return
	}
	// checks if all ns is in the list if not add it.
	if _, ok := td.FindRow(client.NamespaceAll); !ok {
		fields := make(model1.Fields, td.HeaderCount())
		fields[nameCol] = client.NamespaceAll
		if statusCol, found := td.Header().IndexOf("STATUS", true); found {
			fields[statusCol] = "Active"
		}
		td.AddRow(model1.RowEvent{
			Kind: model1.EventUnchanged,
			Row: model1.Row{
				ID:     client.NamespaceAll,
				Fields: fields,
			},
		},
		)
	}

	td.RowsRange(func(i int, re model1.RowEvent) bool {
		_, name := client.Namespaced(re.Row.ID)
		if favs.Has(name) {
			re.Row.Fields[nameCol] += favNSIndicator
		}
		if name == activeNS {
			re.Row.Fields[nameCol] += defaultNSIndicator
		}
		re.Kind = model1.EventUnchanged
		td.SetRow(i, re)
		return true
	})
}
