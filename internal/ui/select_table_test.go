// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package ui

import (
	"testing"

	"github.com/derailed/k9s/internal/model1"
	"github.com/derailed/tview"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestClearMark(t *testing.T) {
	uu := map[string]struct {
		marks []model1.RowIdent
	}{
		"empty": {},

		"single": {
			marks: []model1.RowIdent{{ID: "id1"}},
		},

		"multiple": {
			marks: []model1.RowIdent{{ID: "id1"}, {ID: "id2"}, {ID: "id3"}},
		},
	}

	for k, u := range uu {
		t.Run(k, func(t *testing.T) {
			s := SelectTable{
				Table: tview.NewTable(),
				marks: sets.New[model1.RowIdent](),
			}
			for _, mark := range u.marks {
				s.marks.Insert(mark)
			}
			s.ClearMarks()
			assert.Empty(t, s.marks)
		})
	}
}

func TestDeleteMark(t *testing.T) {
	uu := map[string]struct {
		marks   []model1.RowIdent
		deletes []model1.RowIdent
		e       []model1.RowIdent
	}{
		"empty": {
			deletes: []model1.RowIdent{{ID: "foo"}},
			e:       []model1.RowIdent{},
		},

		"delete-none": {
			marks:   []model1.RowIdent{{ID: "id1"}, {ID: "id2"}, {ID: "id3"}},
			deletes: []model1.RowIdent{{ID: "id5"}, {ID: "id6"}, {ID: "id4"}},
			e:       []model1.RowIdent{{ID: "id1"}, {ID: "id2"}, {ID: "id3"}},
		},

		"delete-multiple": {
			marks:   []model1.RowIdent{{ID: "id1"}, {ID: "id2"}, {ID: "id3"}},
			deletes: []model1.RowIdent{{ID: "id1"}, {ID: "id2"}},
			e:       []model1.RowIdent{{ID: "id3"}},
		},
	}

	for k, u := range uu {
		t.Run(k, func(t *testing.T) {
			s := SelectTable{
				Table: tview.NewTable(),
				marks: sets.New[model1.RowIdent](),
			}
			for _, mark := range u.marks {
				s.marks.Insert(mark)
			}
			for _, mark := range u.deletes {
				s.DeleteMark(mark)
			}
			assert.ElementsMatch(t, u.e, s.marks.UnsortedList())
		})
	}
}

func TestToggleMarkDistinguishesRowSource(t *testing.T) {
	s := newSelectTableWithRows(
		model1.Row{ID: "default/p1", Source: "ctx-a"},
		model1.Row{ID: "default/p1", Source: "ctx-b"},
	)

	s.Select(1, 0)
	s.ToggleMark()
	s.Select(2, 0)
	s.ToggleMark()

	assert.True(t, s.IsMarked(model1.RowIdent{ID: "default/p1", Source: "ctx-a"}))
	assert.True(t, s.IsMarked(model1.RowIdent{ID: "default/p1", Source: "ctx-b"}))
	assert.Len(t, s.marks, 2)

	s.Select(1, 0)
	s.ToggleMark()
	assert.False(t, s.IsMarked(model1.RowIdent{ID: "default/p1", Source: "ctx-a"}))
	assert.True(t, s.IsMarked(model1.RowIdent{ID: "default/p1", Source: "ctx-b"}))
}

func TestSpanMarkDistinguishesRowSource(t *testing.T) {
	s := newSelectTableWithRows(
		model1.Row{ID: "default/p1", Source: "ctx-a"},
		model1.Row{ID: "default/p1", Source: "ctx-b"},
		model1.Row{ID: "default/p1", Source: "ctx-c"},
	)

	s.Select(1, 0)
	s.ToggleMark()
	s.Select(3, 0)
	s.SpanMark()

	assert.ElementsMatch(t, []model1.RowIdent{
		{ID: "default/p1", Source: "ctx-a"},
		{ID: "default/p1", Source: "ctx-b"},
		{ID: "default/p1", Source: "ctx-c"},
	}, s.marks.UnsortedList())
}

func newSelectTableWithRows(rows ...model1.Row) *SelectTable {
	s := &SelectTable{
		Table: tview.NewTable(),
		marks: sets.New[model1.RowIdent](),
	}
	for i, row := range rows {
		s.SetCell(i+1, 0, tview.NewTableCell(row.ID).SetReference(row))
	}
	return s
}
