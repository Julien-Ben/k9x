// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"
	"k8s.io/apimachinery/pkg/util/sets"
)

// SelectTable represents a table with selections.
type SelectTable struct {
	*tview.Table

	model      Tabular
	selectedFn func(string) string
	marks      sets.Set[model1.RowIdent]
	selFgColor tcell.Color
	selBgColor tcell.Color
}

// SetModel sets the table model.
func (s *SelectTable) SetModel(m Tabular) {
	s.model = m
}

// GetModel returns the current model.
func (s *SelectTable) GetModel() Tabular {
	return s.model
}

// ClearSelection reset selected row.
func (s *SelectTable) ClearSelection() {
	s.Select(0, 0)
	s.ScrollToBeginning()
}

// SelectFirstRow select first data row if any.
func (s *SelectTable) SelectFirstRow() {
	if s.GetRowCount() > 0 {
		s.Select(1, 0)
	}
}

// GetSelectedRefs return currently marked or selected row identities.
func (s *SelectTable) GetSelectedRefs() []model1.RowIdent {
	if s.marks.Len() == 0 {
		if row := s.GetSelectedRowRef(); row != nil {
			return []model1.RowIdent{row.Ident()}
		}
		if item := s.GetSelectedItem(); item != "" {
			return []model1.RowIdent{{ID: item}}
		}
		return nil
	}

	return s.marks.UnsortedList()
}

// cellRowID extracts the row's path ID from a cell-0 reference. Supports the
// canonical case (reference is a model1.Row) and the legacy case (reference
// is a bare string) so callers added before the multi-context work keep
// functioning.
func cellRowID(ref any) (string, bool) {
	ident, ok := cellRowIdent(ref)
	return ident.ID, ok
}

func cellRowIdent(ref any) (model1.RowIdent, bool) {
	switch v := ref.(type) {
	case model1.Row:
		return v.Ident(), true
	case string:
		return model1.RowIdent{ID: v}, true
	default:
		return model1.RowIdent{}, false
	}
}

// GetRowID returns the row id at given location.
func (s *SelectTable) GetRowID(index int) (string, bool) {
	cell := s.GetCell(index, 0)
	if cell == nil {
		return "", false
	}
	return cellRowID(cell.GetReference())
}

func (s *SelectTable) getRowRef(index int) (model1.RowIdent, bool) {
	cell := s.GetCell(index, 0)
	if cell == nil {
		return model1.RowIdent{}, false
	}
	return cellRowIdent(cell.GetReference())
}

// GetSelectedRowRef returns the currently selected row by reading the cell-0
// reference, or nil if no row is selected or the reference doesn't carry a
// Row. Used by action handlers in multi-context mode to recover the source
// context name from row.Source. Distinct from Table.GetSelectedRow(path),
// which does a path-based model lookup.
func (s *SelectTable) GetSelectedRowRef() *model1.Row {
	if s.GetSelectedRowIndex() == 0 || s.model.Empty() {
		return nil
	}
	cell := s.GetCell(s.GetSelectedRowIndex(), 0)
	if cell == nil {
		return nil
	}
	row, ok := cell.GetReference().(model1.Row)
	if !ok {
		return nil
	}
	return &row
}

// GetSelectedItem returns the currently selected item name.
func (s *SelectTable) GetSelectedItem() string {
	if s.GetSelectedRowIndex() == 0 || s.model.Empty() {
		return ""
	}
	sel, ok := cellRowID(s.GetCell(s.GetSelectedRowIndex(), 0).GetReference())
	if !ok {
		return ""
	}
	if s.selectedFn != nil {
		return s.selectedFn(sel)
	}
	return sel
}

// GetSelectedCell returns the content of a cell for the currently selected row.
func (s *SelectTable) GetSelectedCell(col int) string {
	r, _ := s.GetSelection()
	return TrimCell(s, r, col)
}

// SetSelectedFn defines a function that cleanse the current selection.
func (s *SelectTable) SetSelectedFn(f func(string) string) {
	s.selectedFn = f
}

// GetSelectedRowIndex fetch the currently selected row index.
func (s *SelectTable) GetSelectedRowIndex() int {
	r, _ := s.GetSelection()
	return r
}

// SelectRow select a given row by index.
func (s *SelectTable) SelectRow(r, c int, broadcast bool) {
	if !broadcast {
		s.SetSelectionChangedFunc(nil)
	}
	if count := s.model.RowCount(); count > 0 && r-1 > count {
		r = count + 1
	}
	defer s.SetSelectionChangedFunc(s.selectionChanged)
	s.Select(r, c)
}

// UpdateSelection refresh selected row.
func (s *SelectTable) updateSelection(broadcast bool) {
	r, c := s.GetSelection()
	s.SelectRow(r, c, broadcast)
}

func (s *SelectTable) selectionChanged(r, c int) {
	if r < 0 {
		return
	}
	if cell := s.GetCell(r, c); cell != nil {
		s.SetSelectedStyle(
			tcell.StyleDefault.Foreground(s.selFgColor).
				Background(cell.Color).Attributes(tcell.AttrBold))
	}
}

// ClearMarks delete all marked items.
func (s *SelectTable) ClearMarks() {
	s.marks.Clear()
}

// DeleteMark delete a marked item.
func (s *SelectTable) DeleteMark(k model1.RowIdent) {
	s.marks.Delete(k)
}

// ToggleMark toggles marked row.
func (s *SelectTable) ToggleMark() {
	sel, ok := s.getRowRef(s.GetSelectedRowIndex())
	if !ok || sel.ID == "" {
		return
	}
	if s.marks.Has(sel) {
		s.marks.Delete(sel)
	} else {
		s.marks.Insert(sel)
	}

	if cell := s.GetCell(s.GetSelectedRowIndex(), 0); cell != nil {
		s.SetSelectedStyle(tcell.StyleDefault.Foreground(cell.BackgroundColor).Background(cell.Color).Attributes(tcell.AttrBold))
	}
}

// SpanMark toggles marked row.
func (s *SelectTable) SpanMark() {
	selIndex, prev := s.GetSelectedRowIndex(), -1
	if selIndex <= 0 {
		return
	}
	// Look back to find previous mark
	for i := selIndex - 1; i > 0; i-- {
		ref, ok := s.getRowRef(i)
		if !ok {
			break
		}
		if s.marks.Has(ref) {
			prev = i
			break
		}
	}
	if prev != -1 {
		s.markRange(prev, selIndex)
		return
	}

	// Look forward to see if we have a mark
	for i := selIndex; i < s.GetRowCount(); i++ {
		ref, ok := s.getRowRef(i)
		if !ok {
			break
		}
		if s.marks.Has(ref) {
			prev = i
			break
		}
	}
	s.markRange(prev, selIndex)
}

func (s *SelectTable) markRange(prev, curr int) {
	if prev < 0 {
		return
	}
	if prev > curr {
		prev, curr = curr, prev
	}
	for i := prev + 1; i <= curr; i++ {
		ref, ok := s.getRowRef(i)
		if !ok {
			break
		}
		s.marks.Insert(ref)
		cell := s.GetCell(s.GetSelectedRowIndex(), 0)
		if cell == nil {
			break
		}
		s.SetSelectedStyle(tcell.StyleDefault.Foreground(cell.BackgroundColor).Background(cell.Color).Attributes(tcell.AttrBold))
	}
}

// IsMarked returns true if this item was marked.
func (s *SelectTable) IsMarked(item model1.RowIdent) bool {
	return s.marks.Has(item)
}
